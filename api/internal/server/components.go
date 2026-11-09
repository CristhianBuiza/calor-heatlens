package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"calor/api/internal/store"
)

const maxComponentsPerSite = 200

// compRegistry caches each site's compiled components for ingest tagging
// and the tracker config endpoint; CRUD handlers invalidate it.
type compRegistry struct {
	st    *store.Store
	mu    sync.RWMutex
	sites map[string]*siteComponents
}

type siteComponents struct {
	list     []store.Component
	matchers []*store.ComponentMatcher
	ids      map[string]bool
}

func (c *compRegistry) get(ctx context.Context, siteID string) (*siteComponents, error) {
	c.mu.RLock()
	sc := c.sites[siteID]
	c.mu.RUnlock()
	if sc != nil {
		return sc, nil
	}
	list, err := c.st.ListComponents(ctx, siteID)
	if err != nil {
		return nil, err
	}
	sc = &siteComponents{list: list, ids: map[string]bool{}}
	for _, comp := range list {
		m, err := store.CompileComponent(comp)
		if err != nil {
			continue
		}
		sc.matchers = append(sc.matchers, m)
		sc.ids[comp.ID] = true
	}
	c.mu.Lock()
	if c.sites == nil {
		c.sites = map[string]*siteComponents{}
	}
	c.sites[siteID] = sc
	c.mu.Unlock()
	return sc, nil
}

func (c *compRegistry) invalidate(siteID string) {
	c.mu.Lock()
	delete(c.sites, siteID)
	c.mu.Unlock()
}

// tagClicks assigns component ids to click events: ids the tracker matched in
// the browser (kept only if they belong to the site) plus server-side matches
// on the recorded element path.
func (s *Server) tagClicks(ctx context.Context, siteID string, events []store.Event, fromTracker [][]string) {
	sc, err := s.comps.get(ctx, siteID)
	if err != nil || len(sc.ids) == 0 {
		return
	}
	for i := range events {
		e := &events[i]
		if e.Type != store.TypeClick {
			continue
		}
		var ids []string
		for _, id := range fromTracker[i] {
			if sc.ids[id] && !contains(ids, id) {
				ids = append(ids, id)
			}
		}
		click := store.Click{PageURL: e.PageURL, Selector: e.Selector, Tag: e.ElTag, ID: e.ElID, Classes: e.ElClasses, Component: e.ElComponent}
		e.Components = store.MatchComponents(sc.matchers, &click, ids)
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// ---- validation ----

var attrNameRe = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,63}$`)

type componentInput struct {
	Name        *string `json:"name"`
	Selector    *string `json:"selector"`
	AttrName    *string `json:"attrName"`
	AttrValue   *string `json:"attrValue"`
	PagePattern *string `json:"pagePattern"`
}

// apply merges in onto c and validates the result.
func (in componentInput) apply(c *store.Component) error {
	if in.Name != nil {
		c.Name = clip(*in.Name, 80)
	}
	if in.Selector != nil {
		c.Selector = strings.TrimSpace(*in.Selector)
	}
	if in.AttrName != nil {
		c.AttrName = strings.ToLower(strings.TrimSpace(*in.AttrName))
	}
	if in.AttrValue != nil {
		c.AttrValue = strings.TrimSpace(*in.AttrValue)
	}
	if in.PagePattern != nil {
		c.PagePattern = strings.TrimSpace(*in.PagePattern)
	}
	return validateComponent(c)
}

func validateComponent(c *store.Component) error {
	if c.Name == "" {
		return errors.New("name is required")
	}
	if c.AttrValue != "" && c.AttrName == "" {
		c.AttrName = store.DefaultComponentAttr
	}
	if c.Selector == "" && c.AttrName == "" {
		return errors.New("a CSS selector or an attribute is required")
	}
	if c.Selector != "" {
		if _, err := store.ParseSelector(c.Selector); err != nil {
			return errors.New("selector: " + err.Error())
		}
	}
	if c.AttrName != "" {
		if !attrNameRe.MatchString(c.AttrName) {
			return errors.New("attribute name must be a lowercase HTML attribute name, e.g. data-calor-component")
		}
		limit := 100
		if c.AttrName == store.DefaultComponentAttr {
			// The tracker records at most 60 characters of this attribute.
			limit = 60
		}
		if len(c.AttrValue) > limit {
			return errors.New("attribute value is too long")
		}
		if _, err := store.ParseSelector(store.AttrSelector(c.AttrName, c.AttrValue)); err != nil {
			return errors.New("attribute: " + err.Error())
		}
	}
	if err := store.ValidatePagePattern(c.PagePattern); err != nil {
		return err
	}
	return nil
}

// ---- handlers ----

type componentView struct {
	store.Component
	BrowserSelector string `json:"browserSelector"`
	Evaluable       bool   `json:"evaluable"`
	Clicks          int64  `json:"clicks"`
	Visitors        int64  `json:"visitors"`
	LastSeen        int64  `json:"lastSeen"`
}

func viewOf(c store.Component, sum store.ComponentSummary) componentView {
	v := componentView{Component: c, BrowserSelector: c.BrowserSelector(), Clicks: sum.Clicks, Visitors: sum.Visitors, LastSeen: sum.LastSeen}
	if m, err := store.CompileComponent(c); err == nil {
		v.Evaluable = m.ServerEvaluable()
	}
	return v
}

func (s *Server) componentFrom(r *http.Request) (store.Component, error) {
	c, err := s.store.GetComponent(r.Context(), siteFrom(r).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		return c, httpError{http.StatusNotFound, "unknown component"}
	}
	return c, err
}

func (s *Server) handleListComponents(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	f.Component = ""
	list, err := s.store.ListComponents(r.Context(), f.SiteID)
	if err != nil {
		return nil, err
	}
	sums, err := s.store.ComponentSummaries(r.Context(), f)
	if err != nil {
		return nil, err
	}
	out := make([]componentView, 0, len(list))
	for _, c := range list {
		out = append(out, viewOf(c, sums[c.ID]))
	}
	return map[string]any{"components": out, "limit": maxComponentsPerSite}, nil
}

func (s *Server) handleGetComponent(r *http.Request) (any, error) {
	c, err := s.componentFrom(r)
	if err != nil {
		return nil, err
	}
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	f.Component = ""
	sums, err := s.store.ComponentSummaries(r.Context(), f)
	if err != nil {
		return nil, err
	}
	return viewOf(c, sums[c.ID]), nil
}

func (s *Server) handleDiscoveredComponents(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	found, err := s.store.DiscoveredAttributes(r.Context(), f, intParam(r, "limit", 50, 1, 200))
	if err != nil {
		return nil, err
	}
	sc, err := s.comps.get(r.Context(), f.SiteID)
	if err != nil {
		return nil, err
	}
	byValue := map[string]string{}
	for _, c := range sc.list {
		if c.AttrName == store.DefaultComponentAttr && c.AttrValue != "" {
			byValue[c.AttrValue] = c.ID
		}
	}
	for i := range found {
		found[i].Component = byValue[found[i].Value]
	}
	return map[string]any{"attribute": store.DefaultComponentAttr, "values": found}, nil
}

// retag rebuilds a component's tags from recorded clicks. Queued events are
// written first so clicks that arrived before the change are included.
func (s *Server) retag(ctx context.Context, c store.Component) (int64, error) {
	m, err := store.CompileComponent(c)
	if err != nil {
		return 0, err
	}
	s.queue.Flush()
	return s.store.RetagComponent(ctx, c.SiteID, m)
}

func (s *Server) componentsChanged(ctx context.Context, siteID string) {
	s.comps.invalidate(siteID)
	s.sites.invalidate(ctx, siteID)
}

// checkComponentConflict writes a 409 when another component of the site
// already has c's name or attribute value; names compare case-insensitively.
func (s *Server) checkComponentConflict(w http.ResponseWriter, r *http.Request, c store.Component) bool {
	sc, err := s.comps.get(r.Context(), c.SiteID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return false
	}
	for _, o := range sc.list {
		if o.ID == c.ID {
			continue
		}
		msg := ""
		switch {
		case strings.EqualFold(o.Name, c.Name):
			msg = "a component named " + strconv.Quote(o.Name) + " already exists"
		case c.AttrValue != "" && o.AttrName == c.AttrName && o.AttrValue == c.AttrValue:
			msg = c.AttrName + "=" + strconv.Quote(c.AttrValue) + " is already used by " + strconv.Quote(o.Name)
		default:
			continue
		}
		writeJSON(w, r, http.StatusConflict, map[string]any{"error": msg, "conflictsWith": o.ID})
		return false
	}
	return true
}

func (s *Server) handleCreateComponent(w http.ResponseWriter, r *http.Request) {
	site := siteFrom(r)
	var in componentInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	c := store.Component{SiteID: site.ID}
	if err := in.apply(&c); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.store.CountComponents(r.Context(), site.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	if n >= maxComponentsPerSite {
		writeError(w, r, http.StatusConflict, "component limit reached for this site")
		return
	}
	if !s.checkComponentConflict(w, r, c) {
		return
	}
	c, err = s.store.CreateComponent(r.Context(), c)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	s.componentsChanged(r.Context(), site.ID)
	tagged, err := s.retag(r.Context(), c)
	if err != nil {
		s.cfg.Logger.Printf("retag %s: %v", c.ID, err)
	}
	s.sites.invalidate(r.Context(), site.ID)
	v := viewOf(c, store.ComponentSummary{Clicks: tagged})
	writeJSON(w, r, http.StatusCreated, map[string]any{"component": v, "tagged": tagged})
}

func (s *Server) handleUpdateComponent(w http.ResponseWriter, r *http.Request) {
	old, err := s.componentFrom(r)
	if err != nil {
		var he httpError
		if errors.As(err, &he) {
			writeError(w, r, he.status, he.msg)
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	var in componentInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	c := old
	if err := in.apply(&c); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if !s.checkComponentConflict(w, r, c) {
		return
	}
	c, err = s.store.UpdateComponent(r.Context(), c)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	s.componentsChanged(r.Context(), c.SiteID)
	resp := map[string]any{"retagged": false}
	if !c.SameRules(old) {
		tagged, err := s.retag(r.Context(), c)
		if err != nil {
			s.cfg.Logger.Printf("retag %s: %v", c.ID, err)
		}
		s.sites.invalidate(r.Context(), c.SiteID)
		resp["retagged"], resp["tagged"] = true, tagged
	}
	resp["component"] = viewOf(c, store.ComponentSummary{})
	writeJSON(w, r, http.StatusOK, resp)
}

func (s *Server) handleDeleteComponent(w http.ResponseWriter, r *http.Request) {
	site := siteFrom(r)
	err := s.store.DeleteComponent(r.Context(), site.ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "unknown component")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	s.componentsChanged(r.Context(), site.ID)
	w.WriteHeader(http.StatusNoContent)
}

// handlePreviewComponent validates a draft rule and counts the recorded
// clicks it would tag, without saving anything.
func (s *Server) handlePreviewComponent(w http.ResponseWriter, r *http.Request) {
	var in componentInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	name := "preview"
	in.Name = &name
	c := store.Component{SiteID: siteFrom(r).ID}
	if err := in.apply(&c); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	f, err := s.siteFilter(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	f.Component = ""
	m, err := store.CompileComponent(c)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.store.PreviewComponent(r.Context(), f, m)
	if err != nil {
		s.cfg.Logger.Printf("preview: %v", err)
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"preview": p, "browserSelector": c.BrowserSelector(), "attrName": c.AttrName})
}

func (s *Server) handleComponentStats(r *http.Request) (any, error) {
	c, err := s.componentFrom(r)
	if err != nil {
		return nil, err
	}
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	f.Component = c.ID
	m, err := store.CompileComponent(c)
	if err != nil {
		return nil, err
	}
	st, err := s.store.ComponentStats(r.Context(), f, m, c.PagePattern != "")
	if err != nil {
		return nil, err
	}
	return map[string]any{"component": viewOf(c, store.ComponentSummary{Clicks: st.Clicks, Visitors: st.Visitors, LastSeen: st.LastSeen}), "stats": st}, nil
}

// handleTrackerConfig is public: it only exposes selectors, which are
// visible in the site's own markup anyway.
func (s *Server) handleTrackerConfig(w http.ResponseWriter, r *http.Request) {
	site, ok := s.sites.get(r.Context(), r.PathValue("site"))
	if !ok {
		writeError(w, r, http.StatusNotFound, "unknown site")
		return
	}
	sc, err := s.comps.get(r.Context(), site.ID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	rules := make([][3]string, 0, len(sc.list))
	for _, c := range sc.list {
		rules = append(rules, [3]string{c.ID, c.BrowserSelector(), c.PagePattern})
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, r, http.StatusOK, map[string]any{"c": rules})
}
