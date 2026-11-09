// Package store persists sites, events, screenshots and heatmap rollups in an
// embedded, pure-Go SQLite database (modernc.org/sqlite, no CGO).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Heatmap rollup resolution: clicks are pre-binned into a fixed grid of
// RollupCols x RollupRows cells (relative to document size) per hour.
const (
	RollupCols = 200
	RollupRows = 800
)

// Options tune SQLite for small machines. Zero values pick conservative defaults.
type Options struct {
	CacheKiB     int   // page cache per connection (default 4 MiB)
	MmapBytes    int64 // 0 disables mmap: reads then go through the kernel page cache instead of counting toward RSS
	MaxReadConns int
	// SoftHeapBytes caps SQLite's own allocations process-wide (page caches,
	// sorters); GOMEMLIMIT cannot see them. SQLite frees cache to stay under it.
	SoftHeapBytes int64
}

type Store struct {
	w    *sql.DB // single writer connection
	r    *sql.DB // small pool of read-only connections
	path string
}

var ErrNotFound = errors.New("not found")

// Open opens (and migrates) the database at path. Use ":memory:" only for
// throwaway use; tests use a temp file so reader and writer share data.
func Open(path string, opt Options) (*Store, error) {
	if opt.CacheKiB <= 0 {
		opt.CacheKiB = 4 * 1024
	}
	if opt.MmapBytes < 0 {
		opt.MmapBytes = 0
	}
	if opt.MaxReadConns <= 0 {
		opt.MaxReadConns = 2
	}
	if path != ":memory:" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path = abs
	}
	pragmas := []string{
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		"busy_timeout(5000)",
		"foreign_keys(ON)",
		fmt.Sprintf("cache_size(-%d)", opt.CacheKiB),
		fmt.Sprintf("mmap_size(%d)", opt.MmapBytes),
		"journal_size_limit(67108864)",
		"temp_store(FILE)",
	}
	if opt.SoftHeapBytes > 0 {
		pragmas = append(pragmas, fmt.Sprintf("soft_heap_limit(%d)", opt.SoftHeapBytes))
	}
	dsn := "file:" + path + "?"
	for i, p := range pragmas {
		if i > 0 {
			dsn += "&"
		}
		dsn += "_pragma=" + url.QueryEscape(p)
	}
	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxIdleTime(0)

	s := &Store{w: w, r: w, path: path}
	if err := s.migrate(context.Background()); err != nil {
		w.Close()
		return nil, err
	}
	if path != ":memory:" {
		r, err := sql.Open("sqlite", dsn+"&_pragma="+url.QueryEscape("query_only(1)"))
		if err != nil {
			w.Close()
			return nil, err
		}
		r.SetMaxOpenConns(opt.MaxReadConns)
		r.SetMaxIdleConns(2)
		r.SetConnMaxIdleTime(5 * time.Minute)
		s.r = r
	}
	return s, nil
}

func (s *Store) Close() error {
	var err error
	if s.r != s.w {
		err = s.r.Close()
	}
	if e := s.w.Close(); e != nil {
		err = e
	}
	return err
}

const schemaVersion = 1

var schema = []string{
	`CREATE TABLE IF NOT EXISTS sites (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		domain TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
		type TEXT NOT NULL,
		page_url TEXT NOT NULL,
		session_id TEXT NOT NULL,
		pageview_id TEXT NOT NULL DEFAULT '',
		ts INTEGER NOT NULL,
		x REAL,
		y REAL,
		viewport_w INTEGER NOT NULL DEFAULT 0,
		viewport_h INTEGER NOT NULL DEFAULT 0,
		doc_w INTEGER NOT NULL DEFAULT 0,
		doc_h INTEGER NOT NULL DEFAULT 0,
		selector TEXT NOT NULL DEFAULT '',
		el_tag TEXT NOT NULL DEFAULT '',
		el_id TEXT NOT NULL DEFAULT '',
		el_classes TEXT NOT NULL DEFAULT '',
		el_text TEXT NOT NULL DEFAULT '',
		el_label TEXT NOT NULL DEFAULT '',
		rage INTEGER NOT NULL DEFAULT 0,
		dead INTEGER NOT NULL DEFAULT 0,
		target_url TEXT NOT NULL DEFAULT '',
		nav_kind TEXT NOT NULL DEFAULT '',
		referrer TEXT NOT NULL DEFAULT '',
		redirect_count INTEGER NOT NULL DEFAULT 0,
		scroll_depth REAL,
		duration_ms INTEGER NOT NULL DEFAULT 0,
		received_at INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_events_site_type_ts ON events(site_id, type, ts)`,
	`CREATE INDEX IF NOT EXISTS idx_events_site_page_type_ts ON events(site_id, page_url, type, ts)`,
	`CREATE INDEX IF NOT EXISTS idx_events_site_session_ts ON events(site_id, session_id, ts)`,
	`CREATE INDEX IF NOT EXISTS idx_events_pageview ON events(pageview_id)`,
	`CREATE TABLE IF NOT EXISTS heatmap_rollup (
		site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
		page_url TEXT NOT NULL,
		type TEXT NOT NULL,
		device TEXT NOT NULL,
		hour INTEGER NOT NULL,
		gx INTEGER NOT NULL,
		gy INTEGER NOT NULL,
		n INTEGER NOT NULL,
		PRIMARY KEY (site_id, page_url, type, device, hour, gx, gy)
	) WITHOUT ROWID`,
	`CREATE TABLE IF NOT EXISTS screenshots (
		site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
		page_url TEXT NOT NULL,
		content_type TEXT NOT NULL,
		data BLOB NOT NULL,
		width INTEGER NOT NULL DEFAULT 0,
		height INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (site_id, page_url)
	)`,
}

func (s *Store) migrate(ctx context.Context) error {
	var v int
	if err := s.w.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v >= schemaVersion {
		return nil
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range schema {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// DeviceOf classifies a viewport width.
func DeviceOf(viewportW int) string {
	switch {
	case viewportW <= 0:
		return "unknown"
	case viewportW < 768:
		return "mobile"
	case viewportW < 1024:
		return "tablet"
	default:
		return "desktop"
	}
}

func deviceCond(col, device string) (string, bool) {
	switch device {
	case "mobile":
		return col + " > 0 AND " + col + " < 768", true
	case "tablet":
		return col + " >= 768 AND " + col + " < 1024", true
	case "desktop":
		return col + " >= 1024", true
	}
	return "", false
}

// NormalizeURL reduces a page URL to scheme://host/path (query and fragment
// dropped) so that heatmaps aggregate per page rather than per query string.
func NormalizeURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	out := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + path
	if len(out) > 2048 {
		out = out[:2048]
	}
	return out, true
}

// HostOf returns the lower-cased hostname (without port) of a URL.
func HostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
