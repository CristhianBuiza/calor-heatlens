package store

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"calor/api/internal/store/pgtest"
)

func TestRebind(t *testing.T) {
	if got := rebind(`a = ? AND (?::text = '' OR b = ?) LIMIT ?`); got != `a = $1 AND ($2::text = '' OR b = $3) LIMIT $4` {
		t.Errorf("rebind = %q", got)
	}
	if got := rebind(`SELECT 1`); got != `SELECT 1` {
		t.Errorf("rebind without placeholders = %q", got)
	}
}

func TestMigrationsVersionedAndIdempotent(t *testing.T) {
	dsn := pgtest.DSN(t)
	s, err := Open(ctx, dsn, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var n, max int
	if err := s.w.QueryRow(ctx, `SELECT COUNT(*), MAX(version) FROM schema_migrations`).Scan(&n, &max); err != nil {
		t.Fatal(err)
	}
	if n != SchemaVersion() || max != SchemaVersion() || n == 0 {
		t.Fatalf("schema_migrations: %d rows, max %d; want %d", n, max, SchemaVersion())
	}
	s.Close()

	s, err = Open(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	s.w.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n)
	if n != SchemaVersion() {
		t.Errorf("reopen re-ran migrations: %d rows", n)
	}
	if _, err := s.w.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES (999, 'future')`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(ctx, dsn, Options{}); err == nil || !strings.Contains(err.Error(), "newer than this binary") {
		t.Errorf("opening a newer schema: %v", err)
	}
}

func TestMigrationRunnerAppliesOnlyPending(t *testing.T) {
	pool, err := pgxpool.New(ctx, pgtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	v1 := fstest.MapFS{"migrations/0001_a.sql": {Data: []byte(`CREATE TABLE a (x int); INSERT INTO a VALUES (1);`)}}
	if err := migrate(ctx, pool, v1); err != nil {
		t.Fatal(err)
	}
	v2 := fstest.MapFS{
		"migrations/0001_a.sql": v1["migrations/0001_a.sql"],
		"migrations/0002_b.sql": {Data: []byte(`ALTER TABLE a ADD COLUMN y int NOT NULL DEFAULT 2; INSERT INTO a (x) VALUES (3);`)},
	}
	if err := migrate(ctx, pool, v2); err != nil {
		t.Fatal(err)
	}
	var rows, sum int
	pool.QueryRow(ctx, `SELECT COUNT(*), SUM(x + y) FROM a`).Scan(&rows, &sum)
	if rows != 2 || sum != 8 {
		t.Errorf("after v2: %d rows, sum %d (0001 must not run twice)", rows, sum)
	}
	bad := fstest.MapFS{"migrations/0001_a.sql": v1["migrations/0001_a.sql"], "migrations/0003_c.sql": {Data: []byte(`SELECT 1`)}}
	if err := migrate(ctx, pool, bad); err == nil || !strings.Contains(err.Error(), "without gaps") {
		t.Errorf("gap in numbering: %v", err)
	}
	broken := fstest.MapFS{
		"migrations/0001_a.sql": v1["migrations/0001_a.sql"],
		"migrations/0002_b.sql": v2["migrations/0002_b.sql"],
		"migrations/0003_c.sql": {Data: []byte(`CREATE TABLE c (x int); SELECT nope;`)},
	}
	if err := migrate(ctx, pool, broken); err == nil {
		t.Fatal("failing migration reported success")
	}
	var exists bool
	pool.QueryRow(ctx, `SELECT to_regclass('c') IS NOT NULL`).Scan(&exists)
	if exists {
		t.Error("a failed migration must roll back completely")
	}
}

func monthTS(y int, m time.Month, d int) int64 {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC).UnixMilli()
}

func TestRetentionDropsWholeMonthsAndChunksTheRest(t *testing.T) {
	s := newTestStore(t, "a")
	c, _ := s.CreateComponent(ctx, Component{SiteID: "a", Name: "x", Selector: "a"})
	page := "https://a.test/"
	var evs []Event
	for _, ts := range []int64{monthTS(2026, 1, 15), monthTS(2026, 2, 10), monthTS(2026, 2, 20), monthTS(2026, 3, 5)} {
		e := click("a", page, "s1", "p1", ts, 10, 10)
		e.Components = []string{c.ID}
		evs = append(evs, e)
	}
	mustInsert(t, s, evs...)

	parts := func() map[string]bool {
		list, err := listPartitions(ctx, s.w)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, p := range list {
			out[p.name] = p.attached
		}
		return out
	}
	for _, name := range []string{"events_y2026m01", "event_components_y2026m01", "events_y2026m03"} {
		if !parts()[name] {
			t.Fatalf("partition %s not created on insert: %v", name, parts())
		}
	}

	defer func(n int) { deleteChunk = n }(deleteChunk)
	deleteChunk = 1
	n, err := s.PruneEvents(ctx, monthTS(2026, 2, 15))
	if err != nil || n != 2 {
		t.Fatalf("pruned %d, %v; want 2", n, err)
	}
	p := parts()
	if _, ok := p["events_y2026m01"]; ok {
		t.Errorf("January partition should be dropped: %v", p)
	}
	if _, ok := p["event_components_y2026m01"]; ok {
		t.Errorf("January tag partition should be dropped: %v", p)
	}
	if !p["events_y2026m02"] || !p["event_components_y2026m02"] {
		t.Errorf("boundary month must stay: %v", p)
	}
	var events, tags, rollup int
	s.w.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM events), (SELECT COUNT(*) FROM event_components), (SELECT SUM(n) FROM heatmap_rollup)`).Scan(&events, &tags, &rollup)
	if events != 2 || tags != 2 || rollup != 4 {
		t.Errorf("after prune: %d events, %d tags, rollup %d; want 2, 2, 4 (rollups are kept)", events, tags, rollup)
	}

	// Writing into a dropped month recreates its partitions.
	mustInsert(t, s, click("a", page, "s2", "p2", monthTS(2026, 1, 20), 1, 1))
	if !parts()["events_y2026m01"] {
		t.Error("partition not recreated for a backfilled month")
	}
}

func TestRetagToleratesTagsWrittenAtIngest(t *testing.T) {
	s := newTestStore(t, "a")
	c, _ := s.CreateComponent(ctx, Component{SiteID: "a", Name: "x", Selector: "a"})
	e := click("a", "https://a.test/", "s1", "p1", t0, 1, 1)
	e.Components = []string{c.ID}
	mustInsert(t, s, e)
	var id, ts int64
	if err := s.r.QueryRow(ctx, `SELECT id, ts FROM events`).Scan(&id, &ts); err != nil {
		t.Fatal(err)
	}
	if err := s.insertTags(ctx, c.ID, []tagRow{{id, ts}, {id, ts}}); err != nil {
		t.Fatalf("duplicate tags must be ignored: %v", err)
	}
	if sums, _ := s.ComponentSummaries(ctx, Filter{SiteID: "a"}); sums[c.ID].Clicks != 1 {
		t.Errorf("summaries = %+v", sums)
	}
}

func TestEventIDsFollowBatchOrder(t *testing.T) {
	s := newTestStore(t, "a")
	var evs []Event
	for i := 0; i < 50; i++ {
		evs = append(evs, pageview("a", "https://a.test/", "s1", fmt.Sprintf("p%02d", 49-i), t0, "", 0))
	}
	mustInsert(t, s, evs...)
	rows, err := s.r.Query(ctx, `SELECT pageview_id FROM events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(got) != 50 {
		t.Fatalf("%d rows, %v", len(got), err)
	}
	for i, pv := range got {
		if want := fmt.Sprintf("p%02d", 49-i); pv != want {
			t.Fatalf("row %d by id = %s, want %s (ids must follow batch order)", i, pv, want)
		}
	}
}

// Row ids (ctids) repeat across partitions: deleting one site's rows chunk by
// chunk must not touch another site's rows at the same ctid in another month.
func TestDeleteSiteAcrossPartitionsKeepsOtherSites(t *testing.T) {
	s := newTestStore(t, "a", "b")
	page := "https://a.test/"
	mustInsert(t, s, click("a", page, "s1", "p1", monthTS(2026, 1, 10), 1, 1))
	mustInsert(t, s, click("b", "https://b.test/", "s2", "p2", monthTS(2026, 2, 10), 1, 1))
	mustInsert(t, s, click("b", "https://b.test/", "s3", "p3", monthTS(2026, 1, 11), 1, 1))
	mustInsert(t, s, click("a", page, "s4", "p4", monthTS(2026, 2, 11), 1, 1))
	var shared int
	s.r.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT ctid FROM events GROUP BY ctid HAVING COUNT(*) > 1) d`).Scan(&shared)
	if shared == 0 {
		t.Fatal("test setup: expected rows of different partitions to share a ctid")
	}

	defer func(n int) { deleteChunk = n }(deleteChunk)
	deleteChunk = 1
	if err := s.DeleteSite(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	var a, b int
	s.r.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE site_id = 'a'), COUNT(*) FILTER (WHERE site_id = 'b') FROM events`).Scan(&a, &b)
	if a != 0 || b != 2 {
		t.Errorf("after deleting site a: %d events of a, %d of b; want 0 and 2", a, b)
	}
}
