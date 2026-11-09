package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSelectorAcceptsSubset(t *testing.T) {
	for _, s := range []string{
		"button",
		"*",
		".hero .ctas",
		"div.grid > div.card:nth-of-type(2) a",
		"#signup, .newsletter form",
		`[data-calor-component="newsletter"]`,
		"[data-x=y]",
		"[hidden]",
		`a.md\:flex`,
		`#\31 23`,
		"header.top>nav.main>a:nth-of-type(3)",
		"main section#pricing",
	} {
		if _, err := ParseSelector(s); err != nil {
			t.Errorf("ParseSelector(%q): %v", s, err)
		}
	}
}

func TestParseSelectorRejects(t *testing.T) {
	for s, want := range map[string]string{
		"":                        "empty",
		"a:hover":                 "pseudo-class",
		"li:nth-child(2)":         "pseudo-class",
		"h1 + p":                  "sibling",
		"h1 ~ p":                  "sibling",
		"a[href^=http]":           "attribute",
		"a,":                      "end of selector",
		"div >":                   "end of selector",
		"::before":                "pseudo-class",
		"a:nth-of-type(odd)":      "positive whole number",
		"svg|rect":                "namespaces",
		`[data-x="unterminated]`:  "unclosed",
		strings.Repeat("a ", 200): "longer than",
	} {
		_, err := ParseSelector(s)
		if err == nil {
			t.Errorf("ParseSelector(%q) accepted", s)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ParseSelector(%q) = %v, want mention of %q", s, err, want)
		}
	}
}

func TestSelectorMatchesRecordedPath(t *testing.T) {
	clickOn := func(sel, id, cls string) *Click {
		return &Click{PageURL: "https://acme.test/", Selector: sel, ID: id, Classes: cls}
	}
	nested := clickOn("body>main>section.hero>div.ctas>a.btn.primary", "", "btn primary big")
	idRooted := clickOn("section#pricing>div.grid>div.card:nth-of-type(2)>button.buy", "", "buy")
	cases := []struct {
		sel  string
		c    *Click
		want bool
	}{
		{".hero .ctas", nested, true},
		{".ctas", nested, true},
		{"a.big", nested, true}, // third class comes from el_classes
		{"section.hero > div.ctas > a", nested, true},
		{"main > div.ctas", nested, false},
		{"main div.ctas", nested, true},
		{"html body .btn", nested, true},
		{".footer a", nested, false},
		{"nav, .ctas", nested, true},
		{"div.card:nth-of-type(2)", idRooted, true},
		{"div.card:nth-of-type(1)", idRooted, false},
		{"#pricing .card button", idRooted, true},
		{"#pricing > .grid", idRooted, true},
		{"main #pricing", idRooted, false}, // ancestors above the id were not recorded
		{"div.grid:nth-of-type(1)", idRooted, true},
		{"[data-calor-component=newsletter]", nested, false},
	}
	for _, tc := range cases {
		s, err := ParseSelector(tc.sel)
		if err != nil {
			t.Fatalf("%q: %v", tc.sel, err)
		}
		if got := s.MatchClick(tc.c); got != tc.want {
			t.Errorf("%q on %q = %v, want %v", tc.sel, tc.c.Selector, got, tc.want)
		}
	}

	attr, _ := ParseSelector(`[data-calor-component="newsletter"]`)
	if !attr.MatchClick(&Click{Selector: "div>form>button", Component: "newsletter"}) {
		t.Error("attribute selector should match the recorded closest component value")
	}
	if attr.MatchClick(&Click{Selector: "div>form>button", Component: "footer"}) {
		t.Error("attribute selector matched a different value")
	}
	if other, _ := ParseSelector("[data-x=y]"); other.ServerEvaluable() {
		t.Error("arbitrary attribute selectors cannot be evaluated server-side")
	}
}

func TestPagePatterns(t *testing.T) {
	for _, tc := range []struct {
		pat, url string
		want     bool
	}{
		{"", "https://a.test/x", true},
		{"/", "https://a.test/", true},
		{"/", "https://a.test/x", false},
		{"/products/*", "https://a.test/products/beans", true},
		{"/products/*", "https://a.test/products", false},
		{"/blog/*/comments", "https://a.test/blog/2026/post/comments", true},
		{"*", "https://a.test/anything/at/all", true},
		{"/pricing", "https://a.test/pricing", true},
	} {
		if got := MatchPagePattern(tc.pat, tc.url); got != tc.want {
			t.Errorf("MatchPagePattern(%q, %q) = %v", tc.pat, tc.url, got)
		}
	}
	if ValidatePagePattern("products/*") == nil || ValidatePagePattern("/a?b") == nil {
		t.Error("invalid page patterns accepted")
	}
}

func tagged(e Event, sel, cls, comp string) Event {
	e.Selector, e.ElTag, e.ElClasses, e.ElComponent = sel, "a", cls, comp
	return e
}

func TestRetagFilterPreviewAndStats(t *testing.T) {
	s := newTestStore(t, "a", "b")
	home, shop := "https://a.test/", "https://a.test/shop"
	cta := "body>main>section.hero>div.ctas>a.btn"
	mustInsert(t, s,
		pageview("a", home, "s1", "p1", t0, "", 50),
		tagged(click("a", home, "s1", "p1", t0+1000, 100, 100), cta, "btn", ""),
		tagged(click("a", home, "s1", "p1", t0+2000, 110, 100), cta, "btn", ""),
		tagged(click("a", home, "s1", "p1", t0+3000, 300, 300), "body>footer>a", "", "newsletter"),
		pageview("a", shop, "s2", "p2", t0+hour, "", 50),
		tagged(click("a", shop, "s2", "p2", t0+hour+1000, 100, 100), cta, "btn", ""),
		pageview("a", home, "s3", "p3", t0+2*hour, "", 50),
		tagged(click("b", "https://b.test/", "s9", "p9", t0, 1, 1), cta, "btn", ""),
	)

	hero, err := s.CreateComponent(ctx, Component{SiteID: "a", Name: "Hero CTAs", Selector: ".hero .ctas", PagePattern: "/"})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := CompileComponent(hero)
	n, err := s.RetagComponent(ctx, "a", m)
	if err != nil || n != 2 {
		t.Fatalf("retag = %d, %v; want 2 home-page clicks", n, err)
	}
	// Retagging is idempotent.
	if n, _ := s.RetagComponent(ctx, "a", m); n != 2 {
		t.Fatalf("second retag = %d", n)
	}

	f := Filter{SiteID: "a", Component: hero.ID}
	k, err := s.KPIs(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if k.Clicks != 2 || k.Pageviews != 3 {
		t.Errorf("scoped KPIs clicks=%d pageviews=%d, want 2 and 3 (pageviews stay unscoped)", k.Clicks, k.Pageviews)
	}
	hm, err := s.Heatmap(ctx, Filter{SiteID: "a", PageURL: home, Component: hero.ID, From: t0, To: t0 + 24*hour}, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if hm.Total != 2 || hm.Source != "raw" {
		t.Errorf("scoped heatmap total=%d source=%s", hm.Total, hm.Source)
	}
	els, _ := s.Elements(ctx, f, 10, 0)
	if els.TotalClicks != 2 || len(els.Rows) != 1 {
		t.Errorf("scoped elements = %+v", els)
	}

	st, err := s.ComponentStats(ctx, f, m, true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Clicks != 2 || st.Visitors != 1 || st.SiteClicks != 4 || st.ClickShare != 50 {
		t.Errorf("stats = %+v", st)
	}
	if st.Pageviews != 2 || st.ClickedViews != 1 || st.ClickRate != 50 {
		t.Errorf("exposure = %d pageviews, %d clicked, rate %v; want 2, 1, 50", st.Pageviews, st.ClickedViews, st.ClickRate)
	}

	nl, _ := CompileComponent(Component{ID: "x", AttrName: DefaultComponentAttr, AttrValue: "newsletter"})
	p, err := s.PreviewComponent(ctx, Filter{SiteID: "a"}, nl)
	if err != nil {
		t.Fatal(err)
	}
	if p.MatchedClicks != 1 || p.TotalClicks != 4 || !p.Evaluable || len(p.Elements) != 1 {
		t.Errorf("attribute preview = %+v", p)
	}
	anyPage, _ := CompileComponent(Component{ID: "y", Selector: "a.btn"})
	if p, _ := s.PreviewComponent(ctx, Filter{SiteID: "a"}, anyPage); p.MatchedClicks != 3 || len(p.Pages) != 2 {
		t.Errorf("selector preview = %+v", p)
	}

	sums, err := s.ComponentSummaries(ctx, Filter{SiteID: "a"})
	if err != nil || sums[hero.ID].Clicks != 2 {
		t.Errorf("summaries = %+v, %v", sums, err)
	}
	found, _ := s.DiscoveredAttributes(ctx, Filter{SiteID: "a"}, 10)
	if len(found) != 1 || found[0].Value != "newsletter" || found[0].Clicks != 1 {
		t.Errorf("discovered = %+v", found)
	}

	// Pruning and deleting remove tags in bounded chunks.
	if _, err := s.PruneEvents(ctx, t0+1500); err != nil {
		t.Fatal(err)
	}
	if sums, _ := s.ComponentSummaries(ctx, Filter{SiteID: "a"}); sums[hero.ID].Clicks != 1 {
		t.Errorf("after prune: %+v", sums)
	}
	if err := s.DeleteComponent(ctx, "a", hero.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_components`).Scan(&left)
	if left != 0 {
		t.Errorf("%d tags left after delete", left)
	}
}

func TestInsertEventsWritesTags(t *testing.T) {
	s := newTestStore(t, "a")
	c, _ := s.CreateComponent(ctx, Component{SiteID: "a", Name: "x", Selector: "a"})
	e := click("a", "https://a.test/", "s1", "p1", t0, 1, 1)
	e.Components = []string{c.ID, c.ID}
	mustInsert(t, s, e)
	if sums, _ := s.ComponentSummaries(ctx, Filter{SiteID: "a"}); sums[c.ID].Clicks != 1 {
		t.Errorf("summaries = %+v", sums)
	}
	if err := s.DeleteSite(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	var n int
	s.r.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM event_components) + (SELECT COUNT(*) FROM components)`).Scan(&n)
	if n != 0 {
		t.Errorf("%d component rows left after site delete", n)
	}
}

func TestMigrateFromV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range schemaV1 {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Exec(`PRAGMA user_version = 1`)
	db.Exec(`INSERT INTO sites VALUES ('a','a','a.test',0)`)
	db.Exec(`INSERT INTO events (site_id, type, page_url, session_id, ts, received_at) VALUES ('a','click','https://a.test/','s',1,1)`)
	db.Close()

	s, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var v int
	var comp string
	s.r.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v)
	if err := s.r.QueryRowContext(ctx, `SELECT el_component FROM events`).Scan(&comp); err != nil || v != len(migrations) {
		t.Fatalf("after migrate: version %d, el_component %q, %v", v, comp, err)
	}
}
