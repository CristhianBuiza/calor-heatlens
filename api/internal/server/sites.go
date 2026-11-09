package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"calor/api/internal/store"
)

var (
	siteIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,63}$`)
	domainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
)

// normalizeDomain turns "https://www.Example.com:8080/x" into "example.com".
func normalizeDomain(raw string) (string, bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	// "www." is dropped so the apex and every subdomain are accepted.
	h := strings.TrimPrefix(strings.TrimSuffix(u.Hostname(), "."), "www.")
	if len(h) > 253 || !domainRe.MatchString(h) {
		return "", false
	}
	return h, true
}

type siteInput struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

type siteView struct {
	store.Site
	Pageviews   int64 `json:"pageviews"`
	LastEventAt int64 `json:"lastEventAt"`
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return badRequest("invalid JSON body")
	}
	return nil
}

func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := s.store.ListSites(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	stats, err := s.store.SiteStats(r.Context())
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]siteView, 0, len(sites))
	for _, site := range sites {
		st := stats[site.ID]
		out = append(out, siteView{Site: site, Pageviews: st.Pageviews, LastEventAt: st.LastEventAt})
	}
	writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var in siteInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	in.Name = clip(in.Name, 100)
	domain, ok := normalizeDomain(in.Domain)
	if in.Name == "" || !ok {
		writeError(w, r, http.StatusBadRequest, "name and a valid domain are required")
		return
	}
	if in.ID != "" && !siteIDRe.MatchString(in.ID) {
		writeError(w, r, http.StatusBadRequest, "id must match ^[a-z0-9][a-z0-9_-]{2,63}$")
		return
	}
	site, err := s.store.CreateSite(r.Context(), store.Site{ID: in.ID, Name: in.Name, Domain: domain})
	if errors.Is(err, store.ErrExists) {
		writeError(w, r, http.StatusConflict, "site id already exists")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	s.sites.invalidate(r.Context(), site.ID)
	writeJSON(w, r, http.StatusCreated, site)
}

func (s *Server) handleGetSite(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, siteFrom(r))
}

func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	site := siteFrom(r)
	var in siteInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if in.Name != "" {
		site.Name = clip(in.Name, 100)
	}
	if in.Domain != "" {
		d, ok := normalizeDomain(in.Domain)
		if !ok {
			writeError(w, r, http.StatusBadRequest, "invalid domain")
			return
		}
		site.Domain = d
	}
	updated, err := s.store.UpdateSite(r.Context(), site)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	s.sites.invalidate(r.Context(), site.ID)
	writeJSON(w, r, http.StatusOK, updated)
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id := siteFrom(r).ID
	if err := s.store.DeleteSite(r.Context(), id); err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	s.sites.invalidate(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

// ---- screenshots ----

const maxScreenshot = 8 << 20

func pageParam(r *http.Request) (string, bool) {
	return store.NormalizeURL(r.URL.Query().Get("url"))
}

func (s *Server) handlePutScreenshot(w http.ResponseWriter, r *http.Request) {
	page, ok := pageParam(r)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "url is required")
		return
	}
	var data []byte
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(maxScreenshot); err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid multipart form")
			return
		}
		f, _, ferr := r.FormFile("file")
		if ferr != nil {
			writeError(w, r, http.StatusBadRequest, "missing file field")
			return
		}
		defer f.Close()
		data, err = io.ReadAll(io.LimitReader(f, maxScreenshot+1))
	} else {
		data, err = io.ReadAll(io.LimitReader(r.Body, maxScreenshot+1))
	}
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "read error")
		return
	}
	if len(data) > maxScreenshot {
		writeError(w, r, http.StatusRequestEntityTooLarge, "screenshot larger than 8 MiB")
		return
	}
	ct := http.DetectContentType(data)
	switch ct {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		writeError(w, r, http.StatusUnsupportedMediaType, "expected png, jpeg, gif or webp")
		return
	}
	var width, height int
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		width, height = cfg.Width, cfg.Height
	}
	site := siteFrom(r)
	if err := s.store.PutScreenshot(r.Context(), site.ID, page, store.Screenshot{ContentType: ct, Data: data, Width: width, Height: height}); err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	s.sites.invalidate(r.Context(), site.ID)
	writeJSON(w, r, http.StatusOK, map[string]any{"url": page, "width": width, "height": height, "bytes": len(data)})
}

func (s *Server) handleGetScreenshot(w http.ResponseWriter, r *http.Request) {
	page, ok := pageParam(r)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "url is required")
		return
	}
	shot, err := s.store.GetScreenshot(r.Context(), siteFrom(r).ID, page)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "no screenshot")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	etag := `"` + strconv.FormatInt(shot.UpdatedAt, 36) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=60")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", shot.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(shot.Data)))
	_, _ = w.Write(shot.Data)
}

func (s *Server) handleScreenshotMeta(w http.ResponseWriter, r *http.Request) {
	page, ok := pageParam(r)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "url is required")
		return
	}
	m, err := s.store.ScreenshotMeta(r.Context(), siteFrom(r).ID, page)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"url": page, "screenshot": m})
}

func (s *Server) handleDeleteScreenshot(w http.ResponseWriter, r *http.Request) {
	page, ok := pageParam(r)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "url is required")
		return
	}
	site := siteFrom(r)
	if err := s.store.DeleteScreenshot(r.Context(), site.ID, page); err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	s.sites.invalidate(r.Context(), site.ID)
	w.WriteHeader(http.StatusNoContent)
}
