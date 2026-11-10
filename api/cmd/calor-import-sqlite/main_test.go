package main

import (
	"context"
	"database/sql"
	"io"
	"log"
	"path/filepath"
	"strconv"
	"testing"

	"calor/api/internal/store"
	"calor/api/internal/store/pgtest"
)

// The SQLite schema as calor-api wrote it before the PostgreSQL migration
// (user_version 1, then 2 for component tagging).
var sqliteV1 = []string{
	`CREATE TABLE sites (id TEXT PRIMARY KEY, name TEXT NOT NULL, domain TEXT NOT NULL, created_at INTEGER NOT NULL)`,
	`CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
		type TEXT NOT NULL, page_url TEXT NOT NULL, session_id TEXT NOT NULL, pageview_id TEXT NOT NULL DEFAULT '', ts INTEGER NOT NULL,
		x REAL, y REAL, viewport_w INTEGER NOT NULL DEFAULT 0, viewport_h INTEGER NOT NULL DEFAULT 0, doc_w INTEGER NOT NULL DEFAULT 0,
		doc_h INTEGER NOT NULL DEFAULT 0, selector TEXT NOT NULL DEFAULT '', el_tag TEXT NOT NULL DEFAULT '', el_id TEXT NOT NULL DEFAULT '',
		el_classes TEXT NOT NULL DEFAULT '', el_text TEXT NOT NULL DEFAULT '', el_label TEXT NOT NULL DEFAULT '', rage INTEGER NOT NULL DEFAULT 0,
		dead INTEGER NOT NULL DEFAULT 0, target_url TEXT NOT NULL DEFAULT '', nav_kind TEXT NOT NULL DEFAULT '', referrer TEXT NOT NULL DEFAULT '',
		redirect_count INTEGER NOT NULL DEFAULT 0, scroll_depth REAL, duration_ms INTEGER NOT NULL DEFAULT 0, received_at INTEGER NOT NULL)`,
	`CREATE TABLE heatmap_rollup (site_id TEXT NOT NULL, page_url TEXT NOT NULL, type TEXT NOT NULL, device TEXT NOT NULL, hour INTEGER NOT NULL,
		gx INTEGER NOT NULL, gy INTEGER NOT NULL, n INTEGER NOT NULL, PRIMARY KEY (site_id, page_url, type, device, hour, gx, gy)) WITHOUT ROWID`,
	`CREATE TABLE screenshots (site_id TEXT NOT NULL, page_url TEXT NOT NULL, content_type TEXT NOT NULL, data BLOB NOT NULL,
		width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL, PRIMARY KEY (site_id, page_url))`,
	`PRAGMA user_version = 1`,
}

var sqliteV2 = []string{
	`ALTER TABLE events ADD COLUMN el_component TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE components (id TEXT PRIMARY KEY, site_id TEXT NOT NULL, name TEXT NOT NULL, selector TEXT NOT NULL DEFAULT '',
		attr_name TEXT NOT NULL DEFAULT '', attr_value TEXT NOT NULL DEFAULT '', page_pattern TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
	`CREATE TABLE event_components (component_id TEXT NOT NULL, ts INTEGER NOT NULL, event_id INTEGER NOT NULL,
		PRIMARY KEY (component_id, ts, event_id)) WITHOUT ROWID`,
	`PRAGMA user_version = 2`,
}

const (
	jan = int64(1_767_268_800_000) // 2026-01-01T12:00:00Z
	feb = int64(1_769_947_200_000) // 2026-02-01T12:00:00Z
)

func sqliteDB(t *testing.T, version int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "calor.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := append([]string{}, sqliteV1...)
	if version >= 2 {
		stmts = append(stmts, sqliteV2...)
	}
	stmts = append(stmts,
		`INSERT INTO sites VALUES ('a', 'Acme', 'a.test', 1)`,
		`INSERT INTO events (id, site_id, type, page_url, session_id, pageview_id, ts, nav_kind, scroll_depth, viewport_w, received_at)
			VALUES (7, 'a', 'pageview', 'https://a.test/', 's1', 'p1', `+itoa(jan)+`, 'load', 40, 1280, 1)`,
		`INSERT INTO events (id, site_id, type, page_url, session_id, pageview_id, ts, x, y, viewport_w, doc_w, doc_h, selector, el_tag, el_text, rage, received_at)
			VALUES (8, 'a', 'click', 'https://a.test/', 's1', 'p1', `+itoa(jan+1000)+`, 100, 50, 1280, 400, 400, 'a.cta', 'a', 'Buy', 1, 1)`,
		`INSERT INTO events (id, site_id, type, page_url, session_id, pageview_id, ts, x, y, viewport_w, doc_w, doc_h, selector, el_tag, received_at)
			VALUES (12, 'a', 'click', 'https://a.test/', 's2', 'p2', `+itoa(feb)+`, 300, 300, 390, 400, 400, 'footer a', 'a', 1)`,
		`INSERT INTO heatmap_rollup VALUES ('a', 'https://a.test/', 'click', 'desktop', `+itoa(jan/3_600_000)+`, 50, 100, 1)`,
		`INSERT INTO heatmap_rollup VALUES ('a', 'https://a.test/', 'click', 'mobile', `+itoa(feb/3_600_000)+`, 150, 600, 1)`,
		`INSERT INTO screenshots VALUES ('a', 'https://a.test/', 'image/png', x'89504e470d0a1a0a', 32, 64, 5)`,
	)
	if version >= 2 {
		stmts = append(stmts,
			`UPDATE events SET el_component = 'hero' WHERE id = 8`,
			`INSERT INTO components VALUES ('cmp_1', 'a', 'Hero', '.cta', '', '', '', 2, 3)`,
			`INSERT INTO event_components VALUES ('cmp_1', `+itoa(jan+1000)+`, 8)`,
		)
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return path
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestImportV2(t *testing.T) {
	dsn := pgtest.DSN(t)
	src := sqliteDB(t, 2)
	ctx := context.Background()
	if err := run(ctx, src, dsn, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, dsn, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f := store.Filter{SiteID: "a"}
	k, err := st.KPIs(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if k.Clicks != 2 || k.Pageviews != 1 || k.RageClicks != 1 || k.UniqueVisitors != 2 || k.AvgScrollDepth != 40 {
		t.Errorf("kpis after import: %+v", k)
	}
	hm, _ := st.Heatmap(ctx, store.Filter{SiteID: "a", PageURL: "https://a.test/"}, 4, 4)
	if hm.Source != "rollup" || hm.Total != 2 {
		t.Errorf("rollup heatmap after import: %+v", hm)
	}
	sums, _ := st.ComponentSummaries(ctx, f)
	if sums["cmp_1"].Clicks != 1 {
		t.Errorf("component tags after import: %+v", sums)
	}
	found, _ := st.DiscoveredAttributes(ctx, f, 10)
	if len(found) != 1 || found[0].Value != "hero" {
		t.Errorf("el_component after import: %+v", found)
	}
	shot, err := st.GetScreenshot(ctx, "a", "https://a.test/")
	if err != nil || len(shot.Data) != 8 || shot.Width != 32 {
		t.Errorf("screenshot after import: %v %+v", err, shot)
	}
	// New events continue after the highest imported id.
	if err := st.InsertEvents(ctx, []store.Event{{SiteID: "a", Type: store.TypePageview, PageURL: "https://a.test/", SessionID: "s3", PageviewID: "p3", TS: feb, NavKind: store.NavLoad}}); err != nil {
		t.Fatalf("insert after import: %v", err)
	}

	if err := run(ctx, src, dsn, log.New(io.Discard, "", 0)); err == nil {
		t.Error("importing into a non-empty database should fail")
	}
}

func TestImportV1(t *testing.T) {
	dsn := pgtest.DSN(t)
	ctx := context.Background()
	if err := run(ctx, sqliteDB(t, 1), dsn, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, dsn, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if k, _ := st.KPIs(ctx, store.Filter{SiteID: "a"}); k.Clicks != 2 {
		t.Errorf("v1 import: %+v", k)
	}
}

func TestFailedImportLeavesTargetEmpty(t *testing.T) {
	dsn := pgtest.DSN(t)
	src := sqliteDB(t, 2)
	db, err := sql.Open("sqlite", "file:"+src)
	if err != nil {
		t.Fatal(err)
	}
	// SQLite doesn't enforce foreign keys unless asked; PostgreSQL does.
	if _, err := db.Exec(`INSERT INTO events (site_id, type, page_url, session_id, ts, received_at) VALUES ('gone', 'pageview', 'https://x.test/', 's', ` + itoa(jan) + `, 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	ctx := context.Background()
	if err := run(ctx, src, dsn, log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("import of an orphaned event should fail")
	}
	st, err := store.Open(ctx, dsn, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if sites, _ := st.ListSites(ctx); len(sites) != 0 {
		t.Errorf("failed import left %d sites behind", len(sites))
	}
}
