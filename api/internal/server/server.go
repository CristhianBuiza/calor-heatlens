// Package server exposes the Calor HTTP API.
package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"calor/api/internal/store"
)

type Config struct {
	AdminKey      string
	TrackerDir    string        // served at /tracker/ when set
	MaxBodyBytes  int64         // ingest body limit
	MaxBatch      int           // max events per ingest request
	StrictOrigin  bool          // reject ingest when Origin/page host does not match the site domain
	CacheTTL      time.Duration // aggregation response cache; 0 disables
	CacheEntries  int
	QueueBatches  int // ingest queue capacity (requests)
	FlushInterval time.Duration
	Logger        *log.Logger
}

func (c *Config) defaults() {
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = 256 << 10
	}
	if c.MaxBatch <= 0 {
		c.MaxBatch = 200
	}
	if c.CacheEntries <= 0 {
		c.CacheEntries = 256
	}
	if c.QueueBatches <= 0 {
		c.QueueBatches = 2048
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = 200 * time.Millisecond
	}
	if c.Logger == nil {
		c.Logger = log.New(os.Stderr, "", log.LstdFlags)
	}
}

type Server struct {
	cfg   Config
	store *store.Store
	queue *Queue
	cache *Cache
	sites *siteRegistry
	comps *compRegistry
	mux   *http.ServeMux
}

func New(st *store.Store, cfg Config) *Server {
	cfg.defaults()
	s := &Server{
		cfg:   cfg,
		store: st,
		queue: NewQueue(st, cfg.QueueBatches, cfg.FlushInterval, cfg.Logger),
		cache: NewCache(cfg.CacheTTL, cfg.CacheEntries),
		sites: &siteRegistry{st: st},
		comps: &compRegistry{st: st},
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

// Close drains the ingest queue.
func (s *Server) Close() { s.queue.Close() }

// Flush blocks until all queued events are written.
func (s *Server) Flush() { s.queue.Flush() }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "queued": s.queue.Len()})
	})

	m.HandleFunc("POST /api/events", s.cors(s.handleIngest))
	m.HandleFunc("OPTIONS /api/events", s.cors(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	admin := func(pattern string, h http.HandlerFunc) {
		m.HandleFunc(pattern, s.cors(s.auth(h)))
	}
	m.HandleFunc("OPTIONS /api/", s.cors(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	admin("GET /api/auth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "authRequired": s.cfg.AdminKey != ""})
	})
	admin("GET /api/sites", s.handleListSites)
	admin("POST /api/sites", s.handleCreateSite)
	admin("GET /api/sites/{site}", s.withSite(s.handleGetSite))
	admin("PATCH /api/sites/{site}", s.withSite(s.handleUpdateSite))
	admin("DELETE /api/sites/{site}", s.withSite(s.handleDeleteSite))

	admin("GET /api/sites/{site}/kpis", s.withSite(s.cached(s.handleKPIs)))
	admin("GET /api/sites/{site}/daily", s.withSite(s.cached(s.handleDaily)))
	admin("GET /api/sites/{site}/time-of-day", s.withSite(s.cached(s.handleTimeOfDay)))
	admin("GET /api/sites/{site}/scroll", s.withSite(s.cached(s.handleScroll)))
	admin("GET /api/sites/{site}/devices", s.withSite(s.cached(s.handleDevices)))
	admin("GET /api/sites/{site}/pages", s.withSite(s.cached(s.handlePages)))
	admin("GET /api/sites/{site}/elements", s.withSite(s.cached(s.handleElements)))
	admin("GET /api/sites/{site}/entry-exit", s.withSite(s.cached(s.handleEntryExit)))
	admin("GET /api/sites/{site}/referrers", s.withSite(s.cached(s.handleReferrers)))
	admin("GET /api/sites/{site}/flow", s.withSite(s.cached(s.handleFlow)))
	admin("GET /api/sites/{site}/heatmap", s.withSite(s.cached(s.handleHeatmap)))
	admin("GET /api/sites/{site}/screenshot", s.withSite(s.handleGetScreenshot))
	admin("GET /api/sites/{site}/screenshot/meta", s.withSite(s.handleScreenshotMeta))
	admin("PUT /api/sites/{site}/screenshot", s.withSite(s.handlePutScreenshot))
	admin("POST /api/sites/{site}/screenshot", s.withSite(s.handlePutScreenshot))
	admin("DELETE /api/sites/{site}/screenshot", s.withSite(s.handleDeleteScreenshot))
	admin("GET /api/compare", s.cached(s.handleCompare))

	admin("GET /api/sites/{site}/components", s.withSite(s.cached(s.handleListComponents)))
	admin("POST /api/sites/{site}/components", s.withSite(s.handleCreateComponent))
	admin("POST /api/sites/{site}/components/preview", s.withSite(s.handlePreviewComponent))
	admin("GET /api/sites/{site}/components/discovered", s.withSite(s.cached(s.handleDiscoveredComponents)))
	admin("GET /api/sites/{site}/components/{id}", s.withSite(s.cached(s.handleGetComponent)))
	admin("PATCH /api/sites/{site}/components/{id}", s.withSite(s.handleUpdateComponent))
	admin("DELETE /api/sites/{site}/components/{id}", s.withSite(s.handleDeleteComponent))
	admin("GET /api/sites/{site}/components/{id}/stats", s.withSite(s.cached(s.handleComponentStats)))
	m.HandleFunc("GET /api/sites/{site}/tracker-config", s.cors(s.handleTrackerConfig))

	if s.cfg.TrackerDir != "" {
		fs := http.StripPrefix("/tracker/", http.FileServer(http.Dir(filepath.Clean(s.cfg.TrackerDir))))
		m.HandleFunc("GET /tracker/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			fs.ServeHTTP(w, r)
		})
	}
}

// ---- middleware ----

func (s *Server) cors(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Access-Control-Allow-Origin", "*")
		// Browsers only read these on preflight; beacons never preflight.
		if r.Method == http.MethodOptions {
			hd.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			hd.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Calor-Backfill")
			hd.Set("Access-Control-Max-Age", "86400")
		}
		h(w, r)
	}
}

func (s *Server) isAdmin(r *http.Request) bool {
	if s.cfg.AdminKey == "" {
		return true
	}
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(s.cfg.AdminKey)) == 1
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAdmin(r) {
			writeError(w, r, http.StatusUnauthorized, "missing or invalid API key")
			return
		}
		h(w, r)
	}
}

type ctxKey int

const siteKey ctxKey = 1

func siteFrom(r *http.Request) store.Site {
	site, _ := r.Context().Value(siteKey).(store.Site)
	return site
}

func (s *Server) withSite(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		site, ok := s.sites.get(r.Context(), r.PathValue("site"))
		if !ok {
			writeError(w, r, http.StatusNotFound, "unknown site")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), siteKey, site)))
	}
}

// cached memoises successful GET JSON responses for CacheTTL. Keys include
// the site generation so site/screenshot changes invalidate immediately.
func (s *Server) cached(h func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path + "?" + r.URL.Query().Encode()
		if id := r.PathValue("site"); id != "" {
			key = s.sites.generation(id) + "|" + key
		} else {
			key = s.sites.generation("*") + "|" + key
		}
		if e, ok := s.cache.Get(key); ok {
			w.Header().Set("X-Cache", "hit")
			writeRaw(w, r, http.StatusOK, e)
			return
		}
		v, err := h(r)
		if err != nil {
			var he httpError
			if errors.As(err, &he) {
				writeError(w, r, he.status, he.msg)
				return
			}
			s.cfg.Logger.Printf("error %s: %v", r.URL.Path, err)
			writeError(w, r, http.StatusInternalServerError, "internal error")
			return
		}
		b, err := json.Marshal(v)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "encode error")
			return
		}
		e := s.cache.Put(key, b)
		w.Header().Set("X-Cache", "miss")
		writeRaw(w, r, http.StatusOK, e)
	}
}

type httpError struct {
	status int
	msg    string
}

func (e httpError) Error() string { return e.msg }

func badRequest(msg string) error { return httpError{http.StatusBadRequest, msg} }

// ---- response helpers ----

const gzipMin = 1024

var gzPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed); return w }}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzPool.Get().(*gzip.Writer)
	zw.Reset(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	gzPool.Put(zw)
	return buf.Bytes()
}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

func writeRaw(w http.ResponseWriter, r *http.Request, status int, e *CacheEntry) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	if len(e.Raw) >= gzipMin {
		h.Set("Vary", "Accept-Encoding")
	}
	if len(e.Raw) >= gzipMin && acceptsGzip(r) {
		h.Set("Content-Encoding", "gzip")
		w.WriteHeader(status)
		_, _ = w.Write(e.Gzip())
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(e.Raw)
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
	writeRaw(w, r, status, &CacheEntry{Raw: b})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	writeJSON(w, r, status, map[string]string{"error": msg})
}

// Logging wraps a handler with one access-log line per request.
func Logging(l *log.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		defer func() {
			if rec := recover(); rec != nil {
				l.Printf("panic %s %s: %v", r.Method, r.URL.Path, rec)
				http.Error(sw, "internal error", http.StatusInternalServerError)
			}
			if r.URL.Path != "/api/events" || sw.status >= 400 {
				l.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Microsecond))
			}
		}()
		h.ServeHTTP(sw, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
