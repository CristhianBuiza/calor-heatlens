// Package store persists sites, events, screenshots, components and heatmap
// rollups in PostgreSQL through pgx v5 connection pools.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Heatmap rollup resolution: clicks are pre-binned into a fixed grid of
// RollupCols x RollupRows cells (relative to document size) per hour.
const (
	RollupCols = 200
	RollupRows = 800
)

// Options size the connection pools. Zero values pick small defaults.
type Options struct {
	// ReadConns bounds concurrent analytics queries (default 4).
	ReadConns int32
	// WriteConns serves the ingest writer, admin writes and retention
	// (default 2, so a long retention delete never stalls ingest).
	WriteConns int32
	// MaxConnIdle closes pooled connections idle this long (default 5 min).
	MaxConnIdle time.Duration
}

type Store struct {
	w     *pgxpool.Pool // ingest writer, admin writes, retention
	r     *pgxpool.Pool // read-only analytics queries
	parts partitions
}

var ErrNotFound = errors.New("not found")

// Open connects to the database named by dsn (a postgres:// URL or a libpq
// key=value string; empty uses the PG* environment variables), applies
// pending migrations and makes sure this and next month's partitions exist.
func Open(ctx context.Context, dsn string, opt Options) (*Store, error) {
	if opt.ReadConns <= 0 {
		opt.ReadConns = 4
	}
	if opt.WriteConns <= 0 {
		opt.WriteConns = 2
	}
	if opt.MaxConnIdle <= 0 {
		opt.MaxConnIdle = 5 * time.Minute
	}
	wcfg, err := poolConfig(dsn, opt.WriteConns, 1, opt.MaxConnIdle, nil)
	if err != nil {
		return nil, err
	}
	rcfg, err := poolConfig(dsn, opt.ReadConns, 0, opt.MaxConnIdle, map[string]string{"default_transaction_read_only": "on"})
	if err != nil {
		return nil, err
	}
	w, err := pgxpool.NewWithConfig(ctx, wcfg)
	if err != nil {
		return nil, err
	}
	if err := w.Ping(ctx); err != nil {
		w.Close()
		return nil, err
	}
	r, err := pgxpool.NewWithConfig(ctx, rcfg)
	if err != nil {
		w.Close()
		return nil, err
	}
	s := &Store{w: w, r: r}
	if err := s.migrate(ctx); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.parts.load(ctx, w); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.EnsureUpcomingPartitions(ctx, time.Now()); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func poolConfig(dsn string, maxConns, minConns int32, idle time.Duration, params map[string]string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("database url: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	cfg.MaxConnIdleTime = idle
	rp := cfg.ConnConfig.RuntimeParams
	if rp["application_name"] == "" {
		rp["application_name"] = "calor-api"
	}
	for k, v := range params {
		rp[k] = v
	}
	return cfg, nil
}

func (s *Store) Close() error {
	s.r.Close()
	s.w.Close()
	return nil
}

// Ping checks that the database answers.
func (s *Store) Ping(ctx context.Context) error { return s.w.Ping(ctx) }

// ---- SQL helpers ----

// rebind turns ? placeholders into $1, $2, … so conditions can be composed
// without tracking argument positions. Store SQL never contains a literal '?'.
func rebind(q string) string {
	if strings.IndexByte(q, '?') < 0 {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 16)
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

func (s *Store) query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	return s.r.Query(ctx, rebind(q), args...)
}

func (s *Store) queryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return s.r.QueryRow(ctx, rebind(q), args...)
}

func (s *Store) exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return s.w.Exec(ctx, rebind(q), args...)
}

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// ---- classification helpers ----

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
