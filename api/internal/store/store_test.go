package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

var ctx = context.Background()

func newTestStore(t *testing.T, sites ...string) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, id := range sites {
		if _, err := s.CreateSite(ctx, Site{ID: id, Name: id, Domain: id + ".test"}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func fp(v float64) *float64 { return &v }

const (
	hour = int64(3_600_000)
	t0   = int64(1_767_225_600_000) // 2026-01-01T00:00:00Z, a Thursday
)

func click(site, page, sid, pv string, ts int64, x, y float64) Event {
	return Event{SiteID: site, Type: TypeClick, PageURL: page, SessionID: sid, PageviewID: pv, TS: ts,
		X: fp(x), Y: fp(y), ViewportW: 1280, ViewportH: 800, DocW: 400, DocH: 400}
}

func pageview(site, page, sid, pv string, ts int64, ref string, depth float64) Event {
	return Event{SiteID: site, Type: TypePageview, PageURL: page, SessionID: sid, PageviewID: pv, TS: ts,
		NavKind: NavLoad, Referrer: ref, ScrollDepth: fp(depth), ViewportW: 1280, ViewportH: 800}
}

func mustInsert(t *testing.T, s *Store, evs ...Event) {
	t.Helper()
	if err := s.InsertEvents(ctx, evs); err != nil {
		t.Fatal(err)
	}
}

func cellMap(h Heatmap) map[[2]int]int64 {
	m := map[[2]int]int64{}
	for _, c := range h.Cells {
		m[[2]int{c.X, c.Y}] = c.N
	}
	return m
}

func TestHeatmapBinningRollupMatchesRaw(t *testing.T) {
	s := newTestStore(t, "a")
	page := "https://a.test/"
	mustInsert(t, s,
		click("a", page, "s1", "p1", t0+10, 10, 10),   // cell 0,0
		click("a", page, "s1", "p1", t0+20, 90, 90),   // cell 0,0
		click("a", page, "s2", "p2", t0+30, 150, 10),  // cell 1,0
		click("a", page, "s2", "p2", t0+40, 399, 399), // cell 3,3
		click("a", page, "s3", "p3", t0+50, 900, 900), // beyond document: clamped to 3,3
	)
	want := map[[2]int]int64{{0, 0}: 2, {1, 0}: 1, {3, 3}: 2}

	rollup, err := s.Heatmap(ctx, Filter{SiteID: "a", PageURL: page}, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if rollup.Source != "rollup" {
		t.Fatalf("expected rollup source, got %s", rollup.Source)
	}
	raw, err := s.Heatmap(ctx, Filter{SiteID: "a", PageURL: page, From: t0 + 1}, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Source != "raw" {
		t.Fatalf("expected raw source, got %s", raw.Source)
	}
	for name, h := range map[string]Heatmap{"rollup": rollup, "raw": raw} {
		got := cellMap(h)
		if len(got) != len(want) {
			t.Fatalf("%s: cells = %v, want %v", name, got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: cell %v = %d, want %d", name, k, got[k], v)
			}
		}
		if h.Total != 5 || h.Max != 2 || h.Visitors != 3 || h.DocW != 400 {
			t.Errorf("%s: total=%d max=%d visitors=%d docW=%d", name, h.Total, h.Max, h.Visitors, h.DocW)
		}
	}
}

func TestHeatmapFilters(t *testing.T) {
	s := newTestStore(t, "a", "b")
	page := "https://a.test/"
	mobile := click("a", page, "s1", "p1", t0+2*hour, 10, 10)
	mobile.ViewportW = 390
	nav := click("a", page, "s1", "p1", t0+2*hour, 300, 300)
	nav.Type, nav.NavKind, nav.TargetURL = TypeNavigation, NavLink, "https://a.test/next"
	mustInsert(t, s,
		click("a", page, "s1", "p1", t0, 10, 10),
		click("a", page, "s1", "p1", t0+5*hour, 10, 10),
		mobile, nav,
		click("b", page, "s9", "p9", t0, 10, 10), // other site, same URL
	)
	cases := []struct {
		name string
		f    Filter
		want int64
	}{
		{"all clicks", Filter{SiteID: "a", PageURL: page}, 3},
		{"site isolation", Filter{SiteID: "b", PageURL: page}, 1},
		{"time range", Filter{SiteID: "a", PageURL: page, From: t0 + hour, To: t0 + 3*hour}, 1},
		{"time range raw", Filter{SiteID: "a", PageURL: page, From: t0 + 1, To: t0 + 3*hour}, 1},
		{"device", Filter{SiteID: "a", PageURL: page, Device: "desktop"}, 2},
		{"navigation type", Filter{SiteID: "a", PageURL: page, Types: []string{TypeNavigation}}, 1},
		{"click+navigation", Filter{SiteID: "a", PageURL: page, Types: []string{TypeClick, TypeNavigation}}, 4},
	}
	for _, c := range cases {
		h, err := s.Heatmap(ctx, c.f, 10, 10)
		if err != nil {
			t.Fatal(err)
		}
		if h.Total != c.want {
			t.Errorf("%s: total = %d, want %d", c.name, h.Total, c.want)
		}
	}
}

// Three sessions: s1 A->B, s2 A (bounce), s3 B->A.
func seedSessions(t *testing.T, s *Store) (string, string) {
	a, b := "https://a.test/", "https://a.test/b"
	mustInsert(t, s,
		pageview("a", a, "s1", "p1", t0, "https://google.com/search", 40),
		pageview("a", b, "s1", "p2", t0+1000, a, 100),
		pageview("a", a, "s2", "p3", t0+2000, "", 20),
		pageview("a", b, "s3", "p4", t0+3000, "", 60),
		pageview("a", a, "s3", "p5", t0+4000, b, 80),
		Event{SiteID: "a", Type: TypeScroll, PageURL: a, SessionID: "s1", PageviewID: "p1", TS: t0 + 900, ScrollDepth: fp(76), DurationMs: 9000},
		click("a", a, "s1", "p1", t0+500, 10, 10),
		Event{SiteID: "a", Type: TypeNavigation, PageURL: a, SessionID: "s1", PageviewID: "p1", TS: t0 + 600, NavKind: NavLink, TargetURL: b},
		Event{SiteID: "a", Type: TypeNavigation, PageURL: a, SessionID: "s1", PageviewID: "p1", TS: t0 + 700, NavKind: NavLink, TargetURL: "https://twitter.com/acme"},
		Event{SiteID: "a", Type: TypeNavigation, PageURL: a, SessionID: "s2", PageviewID: "p3", TS: t0 + 2500, NavKind: NavUnload},
	)
	cta := click("a", a, "s3", "p5", t0+4500, 200, 200)
	cta.Selector, cta.ElTag, cta.ElText = "a.cta", "a", "Start free trial"
	mustInsert(t, s, cta)
	return a, b
}

func TestKPIsEntriesExitsBounces(t *testing.T) {
	s := newTestStore(t, "a")
	a, b := seedSessions(t, s)

	k, err := s.KPIs(ctx, Filter{SiteID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if k.Pageviews != 5 || k.UniqueVisitors != 3 || k.Clicks != 2 || k.Pages != 2 {
		t.Fatalf("site kpis: %+v", k)
	}
	if k.Bounces != 1 || k.BounceRate != 33.3 || k.PagesPerSession != 1.7 {
		t.Errorf("bounce/pps: %+v", k)
	}
	if k.Navigations != 2 {
		t.Errorf("navigations = %d, want 2 (unload excluded)", k.Navigations)
	}

	ka, err := s.KPIs(ctx, Filter{SiteID: "a", PageURL: a})
	if err != nil {
		t.Fatal(err)
	}
	if ka.Entries != 2 || ka.Exits != 2 || ka.Bounces != 1 || ka.BounceRate != 50 {
		t.Errorf("page A entries/exits: %+v", ka)
	}
	kb, _ := s.KPIs(ctx, Filter{SiteID: "a", PageURL: b})
	if kb.Entries != 1 || kb.Exits != 1 || kb.ExitRate != 50 {
		t.Errorf("page B entries/exits: %+v", kb)
	}

	ee, err := s.EntryExit(ctx, Filter{SiteID: "a"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ee.Entries) != 2 || ee.Entries[0].URL != a || ee.Entries[0].Count != 2 || ee.Entries[0].Share != 66.7 {
		t.Errorf("entries: %+v", ee.Entries)
	}
	if len(ee.Exits) != 2 || ee.Exits[0].URL != a || ee.Exits[0].Count != 2 {
		t.Errorf("exits: %+v", ee.Exits)
	}
	// Exit points: s1 left from B without clicking, s2 left A without clicking,
	// s3 last clicked "Start free trial" on A.
	got := map[string]int64{}
	for _, p := range ee.ExitPoints {
		got[p.PageURL+"|"+p.Label] = p.Count
	}
	if got[a+"|Start free trial"] != 1 || got[a+"|(left without clicking)"] != 1 || got[b+"|(left without clicking)"] != 1 {
		t.Errorf("exit points: %+v", ee.ExitPoints)
	}
}

func TestScrollDepthDistribution(t *testing.T) {
	s := newTestStore(t, "a")
	a, _ := seedSessions(t, s)
	st, err := s.Scroll(ctx, Filter{SiteID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	// Per-pageview max depth: p1=76 (scroll event beats pageview 40), p2=100, p3=20, p4=60, p5=80.
	if st.Pageviews != 5 || st.AvgDepth != 67.2 {
		t.Fatalf("scroll: %+v", st)
	}
	want := map[int]int64{25: 4, 50: 4, 75: 3, 100: 1}
	for _, r := range st.Reach {
		if r.Pageviews != want[r.Depth] {
			t.Errorf("reach %d%% = %d, want %d", r.Depth, r.Pageviews, want[r.Depth])
		}
	}
	if st.AvgTimeMs != 9000 {
		t.Errorf("avg time = %d", st.AvgTimeMs)
	}
	if st.Buckets[7] != 20 || st.Buckets[9] != 20 {
		t.Errorf("buckets: %v", st.Buckets)
	}
	pa, _ := s.Scroll(ctx, Filter{SiteID: "a", PageURL: a})
	if pa.Pageviews != 3 || pa.AvgDepth != 58.7 {
		t.Errorf("page scroll: %+v", pa)
	}
}

func TestTimeOfDayWithOffset(t *testing.T) {
	s := newTestStore(t, "a")
	page := "https://a.test/"
	mustInsert(t, s,
		click("a", page, "s1", "p1", t0+9*hour, 1, 1),   // Thu 09:00 UTC
		click("a", page, "s1", "p1", t0+9*hour+5, 1, 1), // Thu 09:00 UTC
		click("a", page, "s1", "p1", t0+23*hour, 1, 1),  // Thu 23:00 UTC
	)
	tod, err := s.TimeOfDay(ctx, Filter{SiteID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if tod.Hours[9] != 2 || tod.Hours[23] != 1 || tod.Days[4] != 3 || tod.Peak.Hour != 9 || tod.Total != 3 {
		t.Fatalf("utc: %+v", tod)
	}
	// UTC+2: 09:00 -> 11:00 Thu, 23:00 Thu -> 01:00 Fri.
	tod, _ = s.TimeOfDay(ctx, Filter{SiteID: "a", TZOffsetMin: 120})
	if tod.Hours[11] != 2 || tod.Hours[1] != 1 || tod.Matrix[5][1] != 1 || tod.Matrix[4][11] != 2 {
		t.Fatalf("utc+2: %+v", tod)
	}
}

func TestDailyFillsGaps(t *testing.T) {
	s := newTestStore(t, "a")
	page := "https://a.test/"
	mustInsert(t, s,
		pageview("a", page, "s1", "p1", t0, "", 50),
		click("a", page, "s1", "p1", t0+1, 1, 1),
		pageview("a", page, "s2", "p2", t0+2*24*hour, "", 50),
	)
	days, err := s.Daily(ctx, Filter{SiteID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 || days[0].Date != "2026-01-01" || days[0].Clicks != 1 || days[1].Pageviews != 0 || days[2].Visitors != 1 {
		t.Fatalf("daily: %+v", days)
	}
}

func TestElementsLabelsAndOrder(t *testing.T) {
	s := newTestStore(t, "a")
	page := "https://a.test/"
	buy := click("a", page, "s1", "p1", t0, 100, 100)
	buy.Selector, buy.ElTag, buy.ElText, buy.ElLabel = "button.buy", "button", "Buy", "Add to cart"
	logo := click("a", page, "s1", "p1", t0, 10, 10)
	logo.Selector, logo.ElTag, logo.ElID = "img#logo", "img", "logo"
	rage := buy
	rage.Rage = true
	rage.SessionID = "s2"
	dead := click("a", page, "s2", "p2", t0, 300, 300)
	dead.Selector, dead.ElTag, dead.Dead = "div.hero", "div", true
	mustInsert(t, s, buy, rage, logo, dead)

	el, err := s.Elements(ctx, Filter{SiteID: "a", PageURL: page}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if el.TotalClicks != 4 || len(el.Rows) != 3 {
		t.Fatalf("elements: %+v", el)
	}
	top := el.Rows[0]
	if top.Label != "Add to cart" || top.Clicks != 2 || top.Visitors != 2 || top.Rage != 1 || top.Share != 50 || top.RelX != 0.25 {
		t.Errorf("top element: %+v", top)
	}
	labels := map[string]string{}
	for _, r := range el.Rows {
		labels[r.Selector] = r.Label
	}
	if labels["img#logo"] != "#logo" || labels["div.hero"] != "<div>" {
		t.Errorf("labels: %v", labels)
	}
}

func TestPagesTableSortAndPaginate(t *testing.T) {
	s := newTestStore(t, "a")
	a, b := seedSessions(t, s)
	list, err := s.Pages(ctx, Filter{SiteID: "a"}, PageQuery{Sort: "pageviews", Order: "desc"})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 2 || list.Rows[0].URL != a || list.Rows[0].Pageviews != 3 || list.Rows[0].Path != "/" {
		t.Fatalf("pages: %+v", list)
	}
	ra := list.Rows[0]
	if ra.Entries != 2 || ra.Exits != 2 || ra.UniqueVisitors != 3 || ra.AvgScrollDepth != 58.7 || ra.Clicks != 2 {
		t.Errorf("row A: %+v", ra)
	}
	asc, _ := s.Pages(ctx, Filter{SiteID: "a"}, PageQuery{Sort: "avgScrollDepth", Order: "asc", Limit: 1, Offset: 1})
	if asc.Total != 2 || len(asc.Rows) != 1 || asc.Rows[0].URL != b {
		t.Errorf("sorted/paginated: %+v", asc)
	}
}

func TestFlowAndReferrers(t *testing.T) {
	s := newTestStore(t, "a")
	a, b := seedSessions(t, s)
	fl, err := s.Flow(ctx, Filter{SiteID: "a", PageURL: a}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if fl.Pageviews != 3 || fl.Unloads != 1 || len(fl.Outbound) != 2 {
		t.Fatalf("flow: %+v", fl)
	}
	in := map[string]FlowEdge{}
	for _, e := range fl.Inbound {
		in[e.URL] = e
	}
	if in["(direct)"].Count != 1 || !in["https://google.com/search"].External || in[b].External {
		t.Errorf("inbound: %+v", fl.Inbound)
	}
	out := map[string]FlowEdge{}
	for _, e := range fl.Outbound {
		out[e.URL] = e
	}
	if !out["https://twitter.com/acme"].External || out[b].External || out[b].Count != 1 {
		t.Errorf("outbound: %+v", fl.Outbound)
	}

	refs, err := s.Referrers(ctx, Filter{SiteID: "a"}, "a.test", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, r := range refs {
		got[r.Source] = r.Count
	}
	if got["google.com"] != 1 || got["(direct)"] != 2 || len(got) != 2 {
		t.Errorf("referrers (internal excluded): %+v", refs)
	}
}

func TestDevicesAndSiteDelete(t *testing.T) {
	s := newTestStore(t, "a")
	page := "https://a.test/"
	m := pageview("a", page, "s2", "p2", t0, "", 10)
	m.ViewportW = 390
	mustInsert(t, s, pageview("a", page, "s1", "p1", t0, "", 10), m)
	devs, err := s.Devices(ctx, Filter{SiteID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 || devs[0].Share != 50 {
		t.Fatalf("devices: %+v", devs)
	}
	mustInsert(t, s, click("a", page, "s1", "p1", t0+1000, 10, 10), click("a", page, "s1", "p1", t0+2000, 300, 900))
	defer func(n int) { deleteChunk = n }(deleteChunk)
	deleteChunk = 1 // exercise the chunk loop
	if err := s.DeleteSite(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	k, _ := s.KPIs(ctx, Filter{SiteID: "a"})
	if k.Pageviews != 0 || k.Clicks != 0 {
		t.Errorf("events should be deleted with the site: %+v", k)
	}
	var rollups int
	if err := s.w.QueryRow(`SELECT COUNT(*) FROM heatmap_rollup WHERE site_id = 'a'`).Scan(&rollups); err != nil || rollups != 0 {
		t.Errorf("rollups left after delete: %d %v", rollups, err)
	}
	if err := s.DeleteSite(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: %v, want ErrNotFound", err)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"https://Example.com/a/b?x=1#frag": "https://example.com/a/b",
		"http://example.com":               "http://example.com/",
		"https://example.com:8443/":        "https://example.com:8443/",
	}
	for in, want := range cases {
		if got, ok := NormalizeURL(in); !ok || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "javascript:alert(1)", "/relative", "ftp://x.com/"} {
		if _, ok := NormalizeURL(bad); ok {
			t.Errorf("NormalizeURL(%q) should fail", bad)
		}
	}
	if !DomainMatches("www.acme.test", "acme.test") || DomainMatches("evilacme.test", "acme.test") {
		t.Error("DomainMatches subdomain logic")
	}
}
