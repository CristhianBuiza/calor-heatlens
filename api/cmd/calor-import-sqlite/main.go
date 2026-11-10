// Command calor-import-sqlite copies a Calor SQLite database (schema version
// 1 or 2, as written by calor-api before the PostgreSQL migration) into an
// empty PostgreSQL database, in one transaction, keeping every id so the
// component tags and heatmap rollups stay consistent.
//
//	calor-import-sqlite -sqlite /var/lib/calor/calor.db -database-url postgres://calor@localhost/calor
//
// Stop the old API first so the SQLite file does not change during the copy.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	_ "modernc.org/sqlite"

	"calor/api/internal/store"
)

func main() {
	src := flag.String("sqlite", "", "path to the SQLite database file (required)")
	dsn := flag.String("database-url", os.Getenv("CALOR_DATABASE_URL"), "target PostgreSQL URL (default $CALOR_DATABASE_URL)")
	flag.Parse()
	logger := log.New(os.Stderr, "import ", log.LstdFlags)
	if *src == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(context.Background(), *src, *dsn, logger); err != nil {
		logger.Fatal(err)
	}
}

// table describes one copy: the SQLite SELECT (columns in target order), the
// target columns, and how to turn a scanned SQLite row into COPY values.
type table struct {
	name   string
	query  string
	cols   []string
	scan   func(*sql.Rows) ([]any, error)
	needV2 bool
}

func run(ctx context.Context, srcPath, dsn string, logger *log.Logger) error {
	if _, err := os.Stat(srcPath); err != nil {
		return err
	}
	src, err := sql.Open("sqlite", "file:"+srcPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer src.Close()
	var version int
	if err := src.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read %s: %w", srcPath, err)
	}
	if version < 1 || version > 2 {
		return fmt.Errorf("%s has schema version %d; this importer reads versions 1 and 2", srcPath, version)
	}

	st, err := store.Open(ctx, dsn, store.Options{})
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer st.Close()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	var nonEmpty bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sites) OR EXISTS (SELECT 1 FROM events)`).Scan(&nonEmpty); err != nil {
		return err
	}
	if nonEmpty {
		return errors.New("target database already has data; import into an empty database")
	}

	if err := createPartitions(ctx, src, st); err != nil {
		return err
	}

	elComponent := "el_component"
	if version < 2 {
		elComponent = "''"
	}
	tables := []table{
		{name: "sites", query: `SELECT id, name, domain, created_at FROM sites`,
			cols: []string{"id", "name", "domain", "created_at"}, scan: scanN(4)},
		{name: "components", needV2: true,
			query: `SELECT id, site_id, name, selector, attr_name, attr_value, page_pattern, created_at, updated_at FROM components`,
			cols:  []string{"id", "site_id", "name", "selector", "attr_name", "attr_value", "page_pattern", "created_at", "updated_at"}, scan: scanN(9)},
		{name: "events", query: `SELECT id, site_id, type, page_url, session_id, pageview_id, ts, x, y,
			viewport_w, viewport_h, doc_w, doc_h, selector, el_tag, el_id, el_classes, el_text, el_label, ` + elComponent + `,
			rage, dead, target_url, nav_kind, referrer, redirect_count, scroll_depth, duration_ms, received_at FROM events ORDER BY id`,
			cols: store.EventColumns, scan: scanEvent},
		{name: "event_components", needV2: true, query: `SELECT component_id, ts, event_id FROM event_components`,
			cols: []string{"component_id", "ts", "event_id"}, scan: scanN(3)},
		{name: "heatmap_rollup", query: `SELECT site_id, page_url, type, device, hour, gx, gy, n FROM heatmap_rollup`,
			cols: []string{"site_id", "page_url", "type", "device", "hour", "gx", "gy", "n"}, scan: scanN(8)},
		{name: "screenshots", query: `SELECT site_id, page_url, content_type, data, width, height, updated_at FROM screenshots`,
			cols: []string{"site_id", "page_url", "content_type", "data", "width", "height", "updated_at"}, scan: scanN(7)},
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	copied := map[string]int64{}
	for _, t := range tables {
		if t.needV2 && version < 2 {
			continue
		}
		start := time.Now()
		n, err := copyTable(ctx, src, tx, t)
		if err != nil {
			return fmt.Errorf("copy %s: %w", t.name, err)
		}
		copied[t.name] = n
		logger.Printf("%-16s %9d rows  %s", t.name, n, time.Since(start).Round(time.Millisecond))
	}
	if _, err := tx.Exec(ctx, `SELECT setval(pg_get_serial_sequence('events', 'id'), COALESCE((SELECT MAX(id) FROM events), 0) + 1, false)`); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `ANALYZE sites, components, events, event_components, heatmap_rollup, screenshots`); err != nil {
		return err
	}
	return verify(ctx, src, conn, tables, version, copied, logger)
}

// createPartitions makes the monthly partitions every imported event needs.
func createPartitions(ctx context.Context, src *sql.DB, st *store.Store) error {
	rows, err := src.QueryContext(ctx, `SELECT DISTINCT strftime('%Y-%m', ts / 1000, 'unixepoch') FROM events`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var tss []int64
	for rows.Next() {
		var m sql.NullString
		if err := rows.Scan(&m); err != nil {
			return err
		}
		t, err := time.Parse("2006-01", m.String)
		if err != nil {
			return fmt.Errorf("event month %q: %w", m.String, err)
		}
		tss = append(tss, t.UnixMilli())
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return st.EnsurePartitions(ctx, tss...)
}

func copyTable(ctx context.Context, src *sql.DB, tx pgx.Tx, t table) (int64, error) {
	rows, err := src.QueryContext(ctx, t.query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var rowErr error
	n, err := tx.CopyFrom(ctx, pgx.Identifier{t.name}, t.cols, pgx.CopyFromFunc(func() ([]any, error) {
		if !rows.Next() {
			rowErr = rows.Err()
			return nil, rowErr
		}
		return t.scan(rows)
	}))
	if err == nil {
		err = rowErr
	}
	return n, err
}

func scanN(n int) func(*sql.Rows) ([]any, error) {
	return func(rows *sql.Rows) ([]any, error) {
		vals := make([]any, n)
		ptrs := make([]any, n)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		err := rows.Scan(ptrs...)
		return vals, err
	}
}

func scanEvent(rows *sql.Rows) ([]any, error) {
	var (
		id, ts, receivedAt, durationMs                     int64
		vw, vh, dw, dh, rage, dead, redirects              int
		site, typ, page, sid, pv, sel, tag, elID, cls, txt string
		label, comp, target, kind, ref                     string
		x, y, depth                                        sql.NullFloat64
	)
	err := rows.Scan(&id, &site, &typ, &page, &sid, &pv, &ts, &x, &y, &vw, &vh, &dw, &dh,
		&sel, &tag, &elID, &cls, &txt, &label, &comp, &rage, &dead, &target, &kind, &ref, &redirects, &depth, &durationMs, &receivedAt)
	return []any{id, site, typ, page, sid, pv, ts, nullable(x), nullable(y), vw, vh, dw, dh,
		sel, tag, elID, cls, txt, label, comp, rage != 0, dead != 0, target, kind, ref, redirects, nullable(depth), durationMs, receivedAt}, err
}

func nullable(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}
	return &f.Float64
}

// verify compares row counts and a few aggregates between source and target.
func verify(ctx context.Context, src *sql.DB, conn *pgx.Conn, tables []table, version int, copied map[string]int64, logger *log.Logger) error {
	ok := true
	for _, t := range tables {
		if t.needV2 && version < 2 {
			continue
		}
		var a, b int64
		if err := src.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t.name).Scan(&a); err != nil {
			return err
		}
		if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM `+t.name).Scan(&b); err != nil {
			return err
		}
		if a != b || copied[t.name] != a {
			logger.Printf("MISMATCH %s: sqlite %d, postgres %d", t.name, a, b)
			ok = false
		}
	}
	checks := []string{
		`SELECT COALESCE(SUM(id), 0) FROM events`,
		`SELECT COALESCE(SUM(ts), 0) FROM events`,
		`SELECT COUNT(*) FROM events WHERE rage <> 0`,
		`SELECT COALESCE(SUM(n), 0) FROM heatmap_rollup`,
		`SELECT COALESCE(SUM(LENGTH(data)), 0) FROM screenshots`,
	}
	pgChecks := []string{
		`SELECT COALESCE(SUM(id), 0)::bigint FROM events`,
		`SELECT COALESCE(SUM(ts), 0)::bigint FROM events`,
		`SELECT COUNT(*) FROM events WHERE rage`,
		`SELECT COALESCE(SUM(n), 0)::bigint FROM heatmap_rollup`,
		`SELECT COALESCE(SUM(length(data)), 0)::bigint FROM screenshots`,
	}
	for i := range checks {
		var a, b int64
		if err := src.QueryRowContext(ctx, checks[i]).Scan(&a); err != nil {
			return err
		}
		if err := conn.QueryRow(ctx, pgChecks[i]).Scan(&b); err != nil {
			return err
		}
		if a != b {
			logger.Printf("MISMATCH %s: sqlite %d, postgres %d", checks[i], a, b)
			ok = false
		}
	}
	if !ok {
		return errors.New("verification failed")
	}
	logger.Printf("verified: row counts, id/ts sums, rage flags, rollup totals and screenshot bytes match")
	return nil
}
