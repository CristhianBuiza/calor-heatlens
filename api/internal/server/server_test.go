package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"calor/api/internal/store"
)

const key = "test-key"

type harness struct {
	t   *testing.T
	srv *Server
	st  *store.Store
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, Config{AdminKey: key, StrictOrigin: true, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { srv.Close(); st.Close() })
	h := &harness{t: t, srv: srv, st: st}
	h.createSite("acme", "Acme", "acme.test")
	h.createSite("nimbus", "Nimbus", "https://www.nimbus.test/")
	return h
}

func (h *harness) do(method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)
	return rec
}

func (h *harness) admin(method, path string, body io.Reader) *httptest.ResponseRecorder {
	return h.do(method, path, body, map[string]string{"Authorization": "Bearer " + key})
}

func (h *harness) createSite(id, name, domain string) {
	h.t.Helper()
	b, _ := json.Marshal(map[string]string{"id": id, "name": name, "domain": domain})
	if rec := h.admin("POST", "/api/sites", bytes.NewReader(b)); rec.Code != http.StatusCreated {
		h.t.Fatalf("create site %s: %d %s", id, rec.Code, rec.Body)
	}
}

func (h *harness) ingest(body string, hdr map[string]string) *httptest.ResponseRecorder {
	if hdr == nil {
		hdr = map[string]string{}
	}
	if _, ok := hdr["Content-Type"]; !ok {
		hdr["Content-Type"] = "text/plain;charset=UTF-8"
	}
	rec := h.do("POST", "/api/events", strings.NewReader(body), hdr)
	h.srv.Flush()
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func batch(site, page string, events string) string {
	return fmt.Sprintf(`{"v":1,"site":%q,"sid":"sess1","ts":%d,"u":%q,"p":"pv1","vw":1280,"vh":800,"e":[%s]}`,
		site, time.Now().UnixMilli(), page, events)
}

const acmeEvents = `{"t":"p","d":-900,"k":"load","ref":"https://google.com/","sd":45},
	{"t":"c","d":-500,"x":100,"y":50,"dw":1280,"dh":2000,"el":{"sel":"a.cta","tag":"A","txt":"Shop beans"}},
	{"t":"c","d":-400,"x":110,"y":60,"dw":1280,"dh":2000,"el":{"sel":"a.cta","tag":"A","txt":"Shop beans"},"rage":1},
	{"t":"n","d":-300,"x":110,"y":60,"dw":1280,"dh":2000,"k":"link","to":"https://acme.test/shop?ref=hero"},
	{"t":"s","d":-100,"sd":80,"ms":4200}`

func TestIngestBeaconTextPlainAndCORS(t *testing.T) {
	h := newHarness(t)
	rec := h.ingest(batch("acme", "https://acme.test/?utm=x#top", acmeEvents), map[string]string{"Origin": "https://acme.test"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("CORS header = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "" {
		t.Errorf("preflight-only header on a simple POST: %q", got)
	}
	res := decode[ingestResult](t, rec)
	if res.Accepted != 5 || res.Rejected != 0 {
		t.Fatalf("result %+v", res)
	}
	k, err := h.st.KPIs(ctxBG(), store.Filter{SiteID: "acme", PageURL: "https://acme.test/"})
	if err != nil {
		t.Fatal(err)
	}
	if k.Clicks != 2 || k.Pageviews != 1 || k.RageClicks != 1 || k.AvgScrollDepth != 80 || k.AvgTimeOnPageMs != 4200 {
		t.Errorf("stored kpis (query/hash should be normalised away): %+v", k)
	}
	el, _ := h.st.Elements(ctxBG(), store.Filter{SiteID: "acme"}, 5, 0)
	if len(el.Rows) != 1 || el.Rows[0].Label != "Shop beans" || el.Rows[0].Tag != "a" {
		t.Errorf("elements: %+v", el.Rows)
	}

	pre := h.do("OPTIONS", "/api/events", nil, map[string]string{"Origin": "https://acme.test", "Access-Control-Request-Method": "POST"})
	if pre.Code != http.StatusNoContent || !strings.Contains(pre.Header().Get("Access-Control-Allow-Headers"), "Content-Type") {
		t.Errorf("preflight: %d %v", pre.Code, pre.Header())
	}
}

func TestIngestJSONContentTypeAndLongTypeNames(t *testing.T) {
	h := newHarness(t)
	body := batch("nimbus", "https://app.nimbus.test/pricing", `{"t":"pageview","k":"load"},{"t":"click","x":5,"y":5,"dw":100,"dh":100}`)
	rec := h.ingest(body, map[string]string{"Content-Type": "application/json", "Origin": "https://app.nimbus.test"})
	if rec.Code != http.StatusAccepted || decode[ingestResult](t, rec).Accepted != 2 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestIngestRejectsUnknownSiteAndForeignOrigin(t *testing.T) {
	h := newHarness(t)
	if rec := h.ingest(batch("nope", "https://acme.test/", acmeEvents), nil); rec.Code != http.StatusForbidden {
		t.Errorf("unknown site: %d", rec.Code)
	}
	if rec := h.ingest(batch("acme", "https://acme.test/", acmeEvents), map[string]string{"Origin": "https://evil.test"}); rec.Code != http.StatusForbidden {
		t.Errorf("foreign origin: %d", rec.Code)
	}
	// Events whose page belongs to another domain are dropped individually.
	rec := h.ingest(batch("acme", "https://nimbus.test/", `{"t":"c","x":1,"y":1,"dw":10,"dh":10},{"t":"c","u":"https://shop.acme.test/","x":1,"y":1,"dw":10,"dh":10}`), nil)
	if res := decode[ingestResult](t, rec); rec.Code != http.StatusAccepted || res.Accepted != 1 || res.Rejected != 1 {
		t.Errorf("page host check: %d %+v", rec.Code, res)
	}
}

func TestIngestValidation(t *testing.T) {
	h := newHarness(t)
	cases := map[string]struct {
		body string
		code int
	}{
		"invalid json": {`{"site":`, http.StatusBadRequest},
		"no events":    {`{"site":"acme","sid":"s","e":[]}`, http.StatusBadRequest},
		"bad sid":      {`{"site":"acme","sid":"bad sid!","u":"https://acme.test/","e":[{"t":"c","x":1,"y":1}]}`, http.StatusBadRequest},
		"too large":    {`{"site":"acme","pad":"` + strings.Repeat("x", 300<<10) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for name, c := range cases {
		if rec := h.ingest(c.body, nil); rec.Code != c.code {
			t.Errorf("%s: got %d want %d (%s)", name, rec.Code, c.code, rec.Body)
		}
	}
	// Invalid individual events are counted, not fatal.
	rec := h.ingest(batch("acme", "https://acme.test/", `{"t":"zzz"},{"t":"c"},{"t":"n","k":"link"},{"t":"c","x":1,"y":2,"dw":10,"dh":10}`), nil)
	if res := decode[ingestResult](t, rec); res.Accepted != 1 || res.Rejected != 3 {
		t.Errorf("per-event validation: %+v", res)
	}
}

func TestIngestClockSkewAndBackfill(t *testing.T) {
	h := newHarness(t)
	old := time.Now().Add(-10 * 24 * time.Hour).UnixMilli()
	body := fmt.Sprintf(`{"site":"acme","sid":"s1","ts":%d,"u":"https://acme.test/","p":"pv","e":[{"t":"p","k":"load"}]}`, old)
	// Without backfill the client clock is ignored: the event lands "now".
	h.ingest(body, nil)
	// Backfill without the admin key is not honoured either.
	h.ingest(body, map[string]string{"X-Calor-Backfill": "1"})
	days, _ := h.st.Daily(ctxBG(), store.Filter{SiteID: "acme", To: time.Now().Add(-24 * time.Hour).UnixMilli()})
	for _, d := range days {
		if d.Pageviews != 0 {
			t.Fatalf("skewed client timestamps must not be trusted: %+v", d)
		}
	}
	h.ingest(body, map[string]string{"X-Calor-Backfill": "1", "Authorization": "Bearer " + key})
	k, _ := h.st.KPIs(ctxBG(), store.Filter{SiteID: "acme", To: time.Now().Add(-24 * time.Hour).UnixMilli()})
	if k.Pageviews != 1 {
		t.Errorf("admin backfill should keep client timestamps: %+v", k)
	}
}

func TestAdminAuthAndSites(t *testing.T) {
	h := newHarness(t)
	if rec := h.do("GET", "/api/sites", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no key: %d", rec.Code)
	}
	if rec := h.do("GET", "/api/sites", nil, map[string]string{"X-API-Key": "wrong"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: %d", rec.Code)
	}
	rec := h.do("GET", "/api/sites", nil, map[string]string{"X-API-Key": key})
	sites := decode[[]siteView](t, rec)
	if len(sites) != 2 || sites[1].Domain != "nimbus.test" {
		t.Fatalf("sites: %+v", sites)
	}
	if rec := h.admin("POST", "/api/sites", strings.NewReader(`{"id":"acme","name":"x","domain":"x.test"}`)); rec.Code != http.StatusConflict {
		t.Errorf("duplicate id: %d", rec.Code)
	}
	if rec := h.admin("POST", "/api/sites", strings.NewReader(`{"name":"x","domain":"not a domain"}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("bad domain: %d", rec.Code)
	}
	rec = h.admin("POST", "/api/sites", strings.NewReader(`{"name":"Generated","domain":"gen.test"}`))
	site := decode[store.Site](t, rec)
	if !strings.HasPrefix(site.ID, "site_") || len(site.ID) != 21 {
		t.Errorf("generated id: %q", site.ID)
	}
	// Newly registered sites can ingest immediately (registry invalidation).
	if rec := h.ingest(batch(site.ID, "https://gen.test/", `{"t":"p","k":"load"}`), nil); rec.Code != http.StatusAccepted {
		t.Errorf("ingest to new site: %d %s", rec.Code, rec.Body)
	}
	if rec := h.admin("GET", "/api/sites/missing/kpis", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown site analytics: %d", rec.Code)
	}
	if rec := h.admin("PATCH", "/api/sites/acme", strings.NewReader(`{"name":"Acme Coffee"}`)); rec.Code != http.StatusOK || decode[store.Site](t, rec).Name != "Acme Coffee" {
		t.Errorf("patch: %d %s", rec.Code, rec.Body)
	}
}

func TestSiteScopedAnalyticsEndpoints(t *testing.T) {
	h := newHarness(t)
	h.ingest(batch("acme", "https://acme.test/", acmeEvents), nil)
	h.ingest(batch("nimbus", "https://nimbus.test/", `{"t":"p","k":"load"},{"t":"c","x":5,"y":5,"dw":100,"dh":100}`), nil)

	rec := h.admin("GET", "/api/sites/acme/heatmap?url=https://acme.test/&cols=10&rows=20", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("heatmap: %d %s", rec.Code, rec.Body)
	}
	hm := decode[store.Heatmap](t, rec)
	if hm.Total != 2 || hm.Cols != 10 || hm.Rows != 20 || len(hm.Cells) != 1 || hm.Cells[0].N != 2 {
		t.Errorf("heatmap: %+v", hm)
	}
	if rec := h.admin("GET", "/api/sites/acme/heatmap", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("heatmap without url: %d", rec.Code)
	}
	if rec := h.admin("GET", "/api/sites/acme/kpis?type=bogus", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad type: %d", rec.Code)
	}
	for _, p := range []string{"kpis", "daily", "time-of-day", "scroll", "devices", "pages?sort=exits", "elements", "entry-exit", "referrers", "flow?url=https://acme.test/"} {
		rec := h.admin("GET", "/api/sites/acme/"+p+"&from=2020-01-01&tz=60", nil)
		if !strings.Contains(p, "?") {
			rec = h.admin("GET", "/api/sites/acme/"+p+"?from=2020-01-01&tz=60", nil)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	nimbus := decode[store.KPIs](t, h.admin("GET", "/api/sites/nimbus/kpis", nil))
	if nimbus.Clicks != 1 || nimbus.Pageviews != 1 {
		t.Errorf("site isolation: %+v", nimbus)
	}

	cmp := h.admin("GET", "/api/compare?sites=acme,nimbus", nil)
	if cmp.Code != http.StatusOK {
		t.Fatalf("compare: %d %s", cmp.Code, cmp.Body)
	}
	sites := decode[[]compareSite](t, cmp)
	if len(sites) != 2 || sites[0].KPIs.Clicks != 2 || sites[1].KPIs.Clicks != 1 || len(sites[0].Reach) != 4 {
		t.Errorf("compare: %+v", sites)
	}
	if rec := h.admin("GET", "/api/compare?sites=acme", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("compare needs 2 sites: %d", rec.Code)
	}
}

func TestResponseCacheAndGzip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, Config{CacheTTL: time.Minute, Logger: log.New(io.Discard, "", 0)})
	defer func() { srv.Close(); st.Close() }()
	h := &harness{t: t, srv: srv, st: st}
	h.createSite("acme", "Acme", "acme.test")
	get := func() *httptest.ResponseRecorder {
		return h.do("GET", "/api/sites/acme/kpis", nil, map[string]string{"Accept-Encoding": "gzip"})
	}
	if rec := get(); rec.Header().Get("X-Cache") != "miss" {
		t.Errorf("first request should miss")
	}
	if rec := get(); rec.Header().Get("X-Cache") != "hit" {
		t.Errorf("second request should hit")
	}
	// Screenshot upload bumps the site generation and invalidates the cache.
	h.admin("PUT", "/api/sites/acme/screenshot?url=https://acme.test/", bytes.NewReader(testPNG(t)))
	if rec := get(); rec.Header().Get("X-Cache") != "miss" {
		t.Errorf("cache should be invalidated by screenshot upload")
	}

	for i := 0; i < 40; i++ {
		h.ingest(batch("acme", fmt.Sprintf("https://acme.test/page-%02d", i), `{"t":"p","k":"load"}`), nil)
	}
	rec := h.do("GET", "/api/sites/acme/pages?limit=100", nil, map[string]string{"Accept-Encoding": "gzip"})
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("large responses should be gzipped")
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	var list store.PageList
	if err := json.NewDecoder(zr).Decode(&list); err != nil || list.Total != 40 {
		t.Errorf("gzip body: %v total=%d", err, list.Total)
	}
}

func testPNG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 64))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestScreenshotRoundTrip(t *testing.T) {
	h := newHarness(t)
	img := testPNG(t)
	rec := h.admin("PUT", "/api/sites/acme/screenshot?url=https://acme.test/", bytes.NewReader(img))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	if rec := h.admin("PUT", "/api/sites/acme/screenshot?url=https://acme.test/", strings.NewReader("not an image")); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("non-image upload: %d", rec.Code)
	}
	get := h.admin("GET", "/api/sites/acme/screenshot?url=https://acme.test/", nil)
	if get.Code != http.StatusOK || get.Header().Get("Content-Type") != "image/png" || !bytes.Equal(get.Body.Bytes(), img) {
		t.Fatalf("download: %d %s", get.Code, get.Header())
	}
	meta := h.admin("GET", "/api/sites/acme/screenshot/meta?url=https://acme.test/", nil)
	if !strings.Contains(meta.Body.String(), `"width":32,"height":64`) {
		t.Errorf("meta: %s", meta.Body)
	}
	if rec := h.admin("GET", "/api/sites/nimbus/screenshot?url=https://acme.test/", nil); rec.Code != http.StatusNotFound {
		t.Errorf("screenshots must be site-scoped: %d", rec.Code)
	}
}

func ctxBG() context.Context { return context.Background() }
