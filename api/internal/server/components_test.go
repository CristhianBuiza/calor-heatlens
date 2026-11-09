package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type componentResp struct {
	Component componentView `json:"component"`
	Tagged    int64         `json:"tagged"`
	Retagged  bool          `json:"retagged"`
}

func (h *harness) createComponent(site, body string) componentResp {
	h.t.Helper()
	rec := h.admin("POST", "/api/sites/"+site+"/components", strings.NewReader(body))
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("create component: %d %s", rec.Code, rec.Body)
	}
	return decode[componentResp](h.t, rec)
}

const taggedClicks = `{"t":"p","d":-900,"k":"load","sd":45},
	{"t":"c","d":-800,"x":100,"y":50,"dw":1280,"dh":2000,"el":{"sel":"body>main>section.hero>div.ctas>a.btn","tag":"a","cls":"btn","txt":"Shop"}},
	{"t":"c","d":-700,"x":500,"y":1500,"dw":1280,"dh":2000,"el":{"sel":"body>footer>form>button","tag":"button","txt":"Join","ca":"newsletter"}},
	{"t":"c","d":-600,"x":600,"y":900,"dw":1280,"dh":2000,"el":{"sel":"body>main>p","tag":"p"},"cmp":["%s","cmp_bogus"]}`

func TestComponentCRUDAndValidation(t *testing.T) {
	h := newHarness(t)
	if rec := h.do("GET", "/api/sites/acme/components", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated list: %d", rec.Code)
	}
	for body, want := range map[string]string{
		`{"name":"x"}`:                                               "selector or an attribute",
		`{"name":"","selector":"a"}`:                                 "name is required",
		`{"name":"x","selector":"a:hover"}`:                          "pseudo-class",
		`{"name":"x","selector":"h1 + p"}`:                           "sibling",
		`{"name":"x","selector":"a","pagePattern":"x"}`:              "must start with '/'",
		`{"name":"x","attrName":"Bad Name","attrValue":"v"}`:         "attribute name",
		`{"name":"x","attrValue":"` + strings.Repeat("v", 61) + `"}`: "too long",
		`{"name":"x","selector":"a","bogus":1}`:                      "invalid JSON",
	} {
		rec := h.admin("POST", "/api/sites/acme/components", strings.NewReader(body))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %s (want 400 mentioning %q)", body, rec.Code, rec.Body, want)
		}
	}

	c := h.createComponent("acme", `{"name":"Newsletter","attrValue":"newsletter"}`).Component
	if c.AttrName != "data-calor-component" || c.BrowserSelector != `[data-calor-component="newsletter"]` || !c.Evaluable {
		t.Errorf("attribute component defaults: %+v", c)
	}
	sel := h.createComponent("acme", `{"name":"Hero CTAs","selector":".hero .ctas","pagePattern":"/"}`).Component

	rec := h.admin("PATCH", "/api/sites/acme/components/"+sel.ID, strings.NewReader(`{"name":"Hero buttons"}`))
	up := decode[componentResp](t, rec)
	if rec.Code != 200 || up.Component.Name != "Hero buttons" || up.Retagged || up.Component.Selector != ".hero .ctas" {
		t.Errorf("rename: %d %+v", rec.Code, up)
	}
	rec = h.admin("PATCH", "/api/sites/acme/components/"+sel.ID, strings.NewReader(`{"selector":"a:focus"}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid update accepted: %d", rec.Code)
	}
	if rec := h.admin("GET", "/api/sites/nimbus/components/"+sel.ID, nil); rec.Code != http.StatusNotFound {
		t.Errorf("component visible through another site: %d", rec.Code)
	}

	list := decode[struct {
		Components []componentView `json:"components"`
		Limit      int             `json:"limit"`
	}](t, h.admin("GET", "/api/sites/acme/components", nil))
	if len(list.Components) != 2 || list.Limit != maxComponentsPerSite {
		t.Errorf("list = %+v", list)
	}

	if rec := h.admin("DELETE", "/api/sites/acme/components/"+c.ID, nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := h.admin("DELETE", "/api/sites/acme/components/"+c.ID, nil); rec.Code != http.StatusNotFound {
		t.Errorf("second delete: %d", rec.Code)
	}
}

func TestComponentTaggingAtIngestAndFilters(t *testing.T) {
	h := newHarness(t)
	hero := h.createComponent("acme", `{"name":"Hero CTAs","selector":".hero .ctas"}`).Component
	nl := h.createComponent("acme", `{"name":"Newsletter","attrValue":"newsletter"}`).Component
	other := h.createComponent("nimbus", `{"name":"Elsewhere","selector":"p"}`).Component
	pricing := h.createComponent("acme", `{"name":"Pricing","selector":"[data-role=price]"}`).Component
	if pricing.Evaluable {
		t.Error("arbitrary attribute selectors should be tracker-only")
	}

	// The third click carries a tracker-side match for a component that
	// exists, one from another site and one that does not exist.
	events := strings.Replace(taggedClicks, "%s", pricing.ID+`","`+other.ID, 1)
	if rec := h.ingest(batch("acme", "https://acme.test/", events), map[string]string{"Origin": "https://acme.test"}); rec.Code != http.StatusAccepted {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body)
	}

	stats := func(id string) map[string]any {
		rec := h.admin("GET", "/api/sites/acme/components/"+id+"/stats", nil)
		if rec.Code != 200 {
			t.Fatalf("stats %s: %d %s", id, rec.Code, rec.Body)
		}
		return decode[map[string]any](t, rec)["stats"].(map[string]any)
	}
	for id, want := range map[string]float64{hero.ID: 1, nl.ID: 1, pricing.ID: 1} {
		if got := stats(id)["clicks"]; got != want {
			t.Errorf("component %s clicks = %v, want %v", id, got, want)
		}
	}
	if rec := h.admin("GET", "/api/sites/nimbus/components/"+other.ID+"/stats", nil); decode[map[string]any](t, rec)["stats"].(map[string]any)["clicks"] != 0.0 {
		t.Errorf("tracker id from another site was accepted: %s", rec.Body)
	}

	hm := decode[map[string]any](t, h.admin("GET", "/api/sites/acme/heatmap?url=https://acme.test/&component="+hero.ID, nil))
	if hm["total"] != 1.0 || hm["source"] != "raw" {
		t.Errorf("scoped heatmap = %v", hm)
	}
	all := decode[map[string]any](t, h.admin("GET", "/api/sites/acme/heatmap?url=https://acme.test/", nil))
	if all["total"] != 3.0 {
		t.Errorf("unscoped heatmap total = %v", all["total"])
	}
	k := decode[map[string]any](t, h.admin("GET", "/api/sites/acme/kpis?component="+nl.ID, nil))
	if k["clicks"] != 1.0 || k["pageviews"] != 1.0 {
		t.Errorf("scoped KPIs = %v", k)
	}
	if rec := h.admin("GET", "/api/sites/acme/kpis?component="+other.ID, nil); rec.Code != http.StatusNotFound {
		t.Errorf("filter by another site's component: %d", rec.Code)
	}

	disc := decode[struct {
		Values []struct {
			Value       string `json:"value"`
			Clicks      int    `json:"clicks"`
			ComponentID string `json:"componentId"`
		} `json:"values"`
	}](t, h.admin("GET", "/api/sites/acme/components/discovered", nil))
	if len(disc.Values) != 1 || disc.Values[0].Value != "newsletter" || disc.Values[0].ComponentID != nl.ID {
		t.Errorf("discovered = %+v", disc)
	}

	// Creating a component tags existing clicks; changing its rules retags.
	late := h.createComponent("acme", `{"name":"Footer","selector":"footer"}`)
	if late.Tagged != 1 {
		t.Errorf("backfill tagged %d, want 1", late.Tagged)
	}
	rec := h.admin("PATCH", "/api/sites/acme/components/"+late.Component.ID, strings.NewReader(`{"selector":"main"}`))
	if up := decode[componentResp](t, rec); !up.Retagged || up.Tagged != 2 {
		t.Errorf("retag on rule change: %+v", up)
	}
}

func TestComponentPreviewAndTrackerConfig(t *testing.T) {
	h := newHarness(t)
	h.ingest(batch("acme", "https://acme.test/", strings.Replace(taggedClicks, "%s", "x", 1)), map[string]string{"Origin": "https://acme.test"})

	rec := h.admin("POST", "/api/sites/acme/components/preview", strings.NewReader(`{"selector":"main a, footer"}`))
	if rec.Code != 200 {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	p := decode[struct {
		Preview struct {
			MatchedClicks int  `json:"matchedClicks"`
			TotalClicks   int  `json:"totalClicks"`
			Evaluable     bool `json:"evaluable"`
		} `json:"preview"`
	}](t, rec)
	if p.Preview.MatchedClicks != 2 || p.Preview.TotalClicks != 3 || !p.Preview.Evaluable {
		t.Errorf("preview = %+v", p)
	}
	if rec := h.admin("POST", "/api/sites/acme/components/preview", strings.NewReader(`{"selector":"a::after"}`)); rec.Code != 400 {
		t.Errorf("invalid preview selector: %d", rec.Code)
	}

	h.createComponent("acme", `{"name":"Hero","selector":".hero","pagePattern":"/"}`)
	h.createComponent("acme", `{"name":"Newsletter","attrValue":"newsletter"}`)
	rec = h.do("GET", "/api/sites/acme/tracker-config", nil, map[string]string{"Origin": "https://acme.test"})
	if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(rec.Header().Get("Cache-Control"), "max-age=300") {
		t.Fatalf("tracker config: %d %v", rec.Code, rec.Header())
	}
	var cfg struct {
		C [][3]string `json:"c"`
	}
	json.Unmarshal(rec.Body.Bytes(), &cfg)
	if len(cfg.C) != 2 || cfg.C[0][1] != ".hero" || cfg.C[0][2] != "/" || cfg.C[1][1] != `[data-calor-component="newsletter"]` {
		t.Errorf("tracker config = %s", rec.Body)
	}
	if rec := h.do("GET", "/api/sites/nope/tracker-config", nil, nil); rec.Code != 404 {
		t.Errorf("unknown site config: %d", rec.Code)
	}
}
