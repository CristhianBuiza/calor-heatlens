package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"calor/api/internal/store"
)

// parseTime accepts unix ms, RFC3339 or YYYY-MM-DD (interpreted in the
// request's tz offset).
func parseTime(v string, tzMin int) (int64, error) {
	if v == "" {
		return 0, nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UnixMilli(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, time.FixedZone("", tzMin*60)); err == nil {
		return t.UnixMilli(), nil
	}
	return 0, badRequest("invalid time: " + v)
}

func intParam(r *http.Request, name string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return max(lo, min(hi, v))
}

// filterFrom reads the common query parameters: url, from, to, type, device, tz.
// "to" given as a date is inclusive (the whole day).
func filterFrom(r *http.Request, siteID string) (store.Filter, error) {
	q := r.URL.Query()
	f := store.Filter{SiteID: siteID, TZOffsetMin: intParam(r, "tz", 0, -14*60, 14*60)}
	var err error
	if f.From, err = parseTime(q.Get("from"), f.TZOffsetMin); err != nil {
		return f, err
	}
	if f.To, err = parseTime(q.Get("to"), f.TZOffsetMin); err != nil {
		return f, err
	}
	if to := q.Get("to"); len(to) == 10 && strings.Count(to, "-") == 2 {
		f.To += 24 * 3600 * 1000
	}
	if u := q.Get("url"); u != "" {
		n, ok := store.NormalizeURL(u)
		if !ok {
			return f, badRequest("invalid url")
		}
		f.PageURL = n
	}
	switch t := q.Get("type"); t {
	case "", "click":
		f.Types = []string{store.TypeClick}
	case "navigation":
		f.Types = []string{store.TypeNavigation}
	case "all":
		f.Types = []string{store.TypeClick, store.TypeNavigation}
	default:
		return f, badRequest("type must be click, navigation or all")
	}
	switch d := q.Get("device"); d {
	case "", "all":
	case "mobile", "tablet", "desktop":
		f.Device = d
	default:
		return f, badRequest("device must be mobile, tablet or desktop")
	}
	return f, nil
}

func (s *Server) siteFilter(r *http.Request) (store.Filter, error) {
	return filterFrom(r, siteFrom(r).ID)
}

func requirePage(f store.Filter) error {
	if f.PageURL == "" {
		return badRequest("url is required")
	}
	return nil
}

func (s *Server) handleKPIs(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	return s.store.KPIs(r.Context(), f)
}

func (s *Server) handleDaily(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	return s.store.Daily(r.Context(), f)
}

func (s *Server) handleTimeOfDay(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	return s.store.TimeOfDay(r.Context(), f)
}

func (s *Server) handleScroll(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	return s.store.Scroll(r.Context(), f)
}

func (s *Server) handleDevices(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	return s.store.Devices(r.Context(), f)
}

func (s *Server) handlePages(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	return s.store.Pages(r.Context(), f, store.PageQuery{
		Sort:   q.Get("sort"),
		Order:  q.Get("order"),
		Limit:  intParam(r, "limit", 50, 1, 500),
		Offset: intParam(r, "offset", 0, 0, 1<<30),
		Search: strings.TrimSpace(q.Get("q")),
	})
}

func (s *Server) handleElements(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	return s.store.Elements(r.Context(), f, intParam(r, "limit", 20, 1, 200), intParam(r, "offset", 0, 0, 1<<30))
}

func (s *Server) handleEntryExit(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	return s.store.EntryExit(r.Context(), f, intParam(r, "limit", 10, 1, 100))
}

func (s *Server) handleReferrers(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	return s.store.Referrers(r.Context(), f, siteFrom(r).Domain, intParam(r, "limit", 10, 1, 100))
}

func (s *Server) handleFlow(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	if err := requirePage(f); err != nil {
		return nil, err
	}
	return s.store.Flow(r.Context(), f, intParam(r, "limit", 10, 1, 100))
}

func (s *Server) handleHeatmap(r *http.Request) (any, error) {
	f, err := s.siteFilter(r)
	if err != nil {
		return nil, err
	}
	if err := requirePage(f); err != nil {
		return nil, err
	}
	cols := intParam(r, "cols", 50, 4, 200)
	rows := intParam(r, "rows", 50, 4, 800)
	return s.store.Heatmap(r.Context(), f, cols, rows)
}

type compareSite struct {
	Site     store.Site         `json:"site"`
	KPIs     store.KPIs         `json:"kpis"`
	Daily    []store.Day        `json:"daily"`
	Hours    [24]int64          `json:"hours"`
	Days     [7]int64           `json:"days"`
	Reach    []store.Reach      `json:"reach"`
	Devices  []store.DeviceStat `json:"devices"`
	TopPages []store.PageRow    `json:"topPages"`
}

// handleCompare returns KPIs and chart series for 2..6 sites with one filter.
func (s *Server) handleCompare(r *http.Request) (any, error) {
	ids := strings.Split(r.URL.Query().Get("sites"), ",")
	out := []compareSite{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		site, ok := s.sites.get(r.Context(), id)
		if !ok {
			return nil, httpError{http.StatusNotFound, "unknown site: " + id}
		}
		if len(out) == 6 {
			return nil, badRequest("compare supports at most 6 sites")
		}
		f, err := filterFrom(r, site.ID)
		if err != nil {
			return nil, err
		}
		f.PageURL = ""
		c := compareSite{Site: site}
		ctx := r.Context()
		if c.KPIs, err = s.store.KPIs(ctx, f); err != nil {
			return nil, err
		}
		if c.Daily, err = s.store.Daily(ctx, f); err != nil {
			return nil, err
		}
		tod, err := s.store.TimeOfDay(ctx, f)
		if err != nil {
			return nil, err
		}
		c.Hours, c.Days = tod.Hours, tod.Days
		sc, err := s.store.Scroll(ctx, f)
		if err != nil {
			return nil, err
		}
		c.Reach = sc.Reach
		if c.Devices, err = s.store.Devices(ctx, f); err != nil {
			return nil, err
		}
		pages, err := s.store.Pages(ctx, f, store.PageQuery{Sort: "pageviews", Limit: 5})
		if err != nil {
			return nil, err
		}
		c.TopPages = pages.Rows
		out = append(out, c)
	}
	if len(out) < 2 {
		return nil, badRequest("pass at least two site ids: ?sites=a,b")
	}
	return out, nil
}
