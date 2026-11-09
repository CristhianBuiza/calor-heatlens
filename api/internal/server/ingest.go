package server

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"calor/api/internal/store"
)

// Wire format (sent by tracker/src/calor.js). Batch-level u/p/vw/vh are
// defaults that individual events only repeat when they differ.
type wireBatch struct {
	V      int         `json:"v"`
	Site   string      `json:"site"`
	SID    string      `json:"sid"`
	TS     int64       `json:"ts"` // client clock at send time (unix ms)
	URL    string      `json:"u"`
	PV     string      `json:"p"`
	VW     int         `json:"vw"`
	VH     int         `json:"vh"`
	Events []wireEvent `json:"e"`
}

type wireEvent struct {
	T    string   `json:"t"` // c | n | p | s (or the long names)
	D    int64    `json:"d"` // ms relative to batch ts (<= 0)
	URL  string   `json:"u"`
	PV   string   `json:"p"`
	X    *float64 `json:"x"`
	Y    *float64 `json:"y"`
	VW   int      `json:"vw"`
	VH   int      `json:"vh"`
	DW   int      `json:"dw"`
	DH   int      `json:"dh"`
	El   *wireEl  `json:"el"`
	Rage int      `json:"rage"`
	Dead int      `json:"dead"`
	To   string   `json:"to"`
	K    string   `json:"k"`
	Ref  string   `json:"ref"`
	RC   int      `json:"rc"`
	SD   *float64 `json:"sd"`
	MS   int64    `json:"ms"`
}

type wireEl struct {
	Sel  string `json:"sel"`
	Tag  string `json:"tag"`
	ID   string `json:"id"`
	Cls  string `json:"cls"`
	Txt  string `json:"txt"`
	Aria string `json:"aria"`
}

var (
	idRe       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	typeCodes  = map[string]string{"c": store.TypeClick, "n": store.TypeNavigation, "p": store.TypePageview, "s": store.TypeScroll, store.TypeClick: store.TypeClick, store.TypeNavigation: store.TypeNavigation, store.TypePageview: store.TypePageview, store.TypeScroll: store.TypeScroll}
	validKinds = map[string]bool{store.NavLoad: true, store.NavLink: true, store.NavPushState: true, store.NavReplaceState: true, store.NavPopState: true, store.NavUnload: true}
)

const (
	maxPast   = 35 * 24 * time.Hour
	maxFuture = 5 * time.Minute
)

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

func finite(p *float64, lo, hi float64) *float64 {
	if p == nil || math.IsNaN(*p) || math.IsInf(*p, 0) {
		return nil
	}
	v := math.Max(lo, math.Min(hi, *p))
	return &v
}

func clampDim(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100000 {
		return 100000
	}
	return v
}

type ingestResult struct {
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		writeError(w, r, http.StatusBadRequest, "read error")
		return
	}
	// sendBeacon posts text/plain; fetch fallback may use application/json. Both carry JSON.
	var b wireBatch
	if err := json.Unmarshal(body, &b); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(b.Events) == 0 {
		writeError(w, r, http.StatusBadRequest, "no events")
		return
	}
	if len(b.Events) > s.cfg.MaxBatch {
		writeError(w, r, http.StatusRequestEntityTooLarge, "too many events in batch")
		return
	}
	site, ok := s.sites.get(r.Context(), b.Site)
	if !ok {
		writeError(w, r, http.StatusForbidden, "unknown site")
		return
	}
	// Backfill (seeding/imports) trusts client timestamps and skips origin
	// checks, but only with the admin key.
	backfill := r.Header.Get("X-Calor-Backfill") == "1" && s.cfg.AdminKey != "" && s.isAdmin(r)
	if s.cfg.StrictOrigin && !backfill {
		if o := r.Header.Get("Origin"); o != "" && o != "null" && !store.DomainMatches(store.HostOf(o), site.Domain) {
			writeError(w, r, http.StatusForbidden, "origin not allowed for site")
			return
		}
	}
	if !idRe.MatchString(b.SID) {
		writeError(w, r, http.StatusBadRequest, "invalid sid")
		return
	}

	now := time.Now()
	base := now.UnixMilli()
	if backfill && b.TS > 0 {
		base = b.TS
	}
	// Event times are relative to the batch send time and re-anchored to the
	// server clock, which corrects client clock skew.
	minTS, maxTS := now.Add(-maxPast).UnixMilli(), now.Add(maxFuture).UnixMilli()

	res := ingestResult{}
	events := make([]store.Event, 0, len(b.Events))
	for _, we := range b.Events {
		ev, ok := s.convert(&b, &we, site, base, backfill)
		if !ok || ev.TS < minTS || ev.TS > maxTS {
			res.Rejected++
			continue
		}
		events = append(events, ev)
	}
	if len(events) > 0 {
		if err := s.queue.Enqueue(events); err != nil {
			w.Header().Set("Retry-After", "5")
			writeError(w, r, http.StatusServiceUnavailable, "ingest busy")
			return
		}
	}
	res.Accepted = len(events)
	writeJSON(w, r, http.StatusAccepted, res)
}

func (s *Server) convert(b *wireBatch, we *wireEvent, site store.Site, base int64, backfill bool) (store.Event, bool) {
	typ, ok := typeCodes[we.T]
	if !ok {
		return store.Event{}, false
	}
	rawURL := we.URL
	if rawURL == "" {
		rawURL = b.URL
	}
	page, ok := store.NormalizeURL(rawURL)
	if !ok {
		return store.Event{}, false
	}
	if s.cfg.StrictOrigin && !backfill && !store.DomainMatches(store.HostOf(page), site.Domain) {
		return store.Event{}, false
	}
	pv := we.PV
	if pv == "" {
		pv = b.PV
	}
	if pv != "" && !idRe.MatchString(pv) {
		return store.Event{}, false
	}
	d := we.D
	if d > 0 {
		d = 0
	}
	ev := store.Event{
		SiteID:      site.ID,
		Type:        typ,
		PageURL:     page,
		SessionID:   b.SID,
		PageviewID:  pv,
		TS:          base + d,
		X:           finite(we.X, 0, 1e6),
		Y:           finite(we.Y, 0, 1e6),
		ViewportW:   clampDim(firstNonZero(we.VW, b.VW)),
		ViewportH:   clampDim(firstNonZero(we.VH, b.VH)),
		DocW:        clampDim(we.DW),
		DocH:        clampDim(we.DH),
		Rage:        we.Rage == 1,
		Dead:        we.Dead == 1,
		ScrollDepth: finite(we.SD, 0, 100),
	}
	if we.El != nil {
		ev.Selector = clip(we.El.Sel, 300)
		ev.ElTag = strings.ToLower(clip(we.El.Tag, 32))
		ev.ElID = clip(we.El.ID, 100)
		ev.ElClasses = clip(we.El.Cls, 200)
		ev.ElText = clip(we.El.Txt, 120)
		ev.ElLabel = clip(we.El.Aria, 120)
	}
	if we.MS > 0 && we.MS < int64(24*time.Hour/time.Millisecond) {
		ev.DurationMs = we.MS
	}
	if (typ == store.TypePageview || typ == store.TypeScroll) && pv == "" {
		return store.Event{}, false
	}
	switch typ {
	case store.TypeNavigation, store.TypePageview:
		if !validKinds[we.K] {
			return store.Event{}, false
		}
		ev.NavKind = we.K
		if we.To != "" {
			if t, ok := store.NormalizeURL(we.To); ok {
				ev.TargetURL = t
			} else {
				ev.TargetURL = clip(we.To, 512)
			}
		}
		if typ == store.TypeNavigation && ev.NavKind != store.NavUnload && ev.TargetURL == "" {
			return store.Event{}, false
		}
		if we.Ref != "" {
			if ref, ok := store.NormalizeURL(we.Ref); ok {
				ev.Referrer = ref
			}
		}
		if we.RC > 0 && we.RC < 100 {
			ev.RedirectCount = we.RC
		}
	case store.TypeClick:
		if ev.X == nil || ev.Y == nil {
			return store.Event{}, false
		}
	}
	return ev, true
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
