package store

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Filter scopes every aggregation. SiteID is required; zero From/To mean unbounded.
type Filter struct {
	SiteID      string
	PageURL     string
	From, To    int64 // unix ms, To exclusive
	Types       []string
	Device      string // mobile | tablet | desktop | "" (all)
	TZOffsetMin int    // minutes east of UTC, for hour-of-day / day buckets
}

type cond struct {
	parts []string
	args  []any
}

func (c *cond) add(s string, args ...any) {
	c.parts = append(c.parts, s)
	c.args = append(c.args, args...)
}

func (c *cond) sql() string { return " WHERE " + strings.Join(c.parts, " AND ") }

// base builds site/time/device (and optionally page) conditions on events.
func (f Filter) base(withPage bool) *cond {
	c := &cond{}
	c.add("site_id = ?", f.SiteID)
	if withPage && f.PageURL != "" {
		c.add("page_url = ?", f.PageURL)
	}
	if f.From > 0 {
		c.add("ts >= ?", f.From)
	}
	if f.To > 0 {
		c.add("ts < ?", f.To)
	}
	if dc, ok := deviceCond("viewport_w", f.Device); ok {
		c.add(dc)
	}
	return c
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (f Filter) tzSeconds() int64 { return int64(f.TZOffsetMin) * 60 }

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func ratio(a, b int64) float64 {
	if b == 0 {
		return 0
	}
	return round1(float64(a) * 100 / float64(b))
}

// ---- KPIs ----

type KPIs struct {
	Clicks          int64   `json:"clicks"`
	Pageviews       int64   `json:"pageviews"`
	UniqueVisitors  int64   `json:"uniqueVisitors"`
	Pages           int64   `json:"pages"`
	RageClicks      int64   `json:"rageClicks"`
	DeadClicks      int64   `json:"deadClicks"`
	Navigations     int64   `json:"navigations"`
	AvgScrollDepth  float64 `json:"avgScrollDepth"`
	AvgTimeOnPageMs int64   `json:"avgTimeOnPageMs"`
	Entries         int64   `json:"entries"`
	Exits           int64   `json:"exits"`
	Bounces         int64   `json:"bounces"`
	BounceRate      float64 `json:"bounceRate"`
	ExitRate        float64 `json:"exitRate"`
	PagesPerSession float64 `json:"pagesPerSession"`
	ClicksPerView   float64 `json:"clicksPerView"`
}

func (s *Store) KPIs(ctx context.Context, f Filter) (KPIs, error) {
	var k KPIs
	c := f.base(true)
	err := s.r.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(type = 'click'), 0),
		COALESCE(SUM(type = 'pageview'), 0),
		COUNT(DISTINCT session_id),
		COUNT(DISTINCT page_url),
		COALESCE(SUM(type = 'click' AND rage = 1), 0),
		COALESCE(SUM(type = 'click' AND dead = 1), 0),
		COALESCE(SUM(type = 'navigation' AND nav_kind IN ('link','pushstate','replacestate','popstate')), 0)
		FROM events`+c.sql(), c.args...).
		Scan(&k.Clicks, &k.Pageviews, &k.UniqueVisitors, &k.Pages, &k.RageClicks, &k.DeadClicks, &k.Navigations)
	if err != nil {
		return k, err
	}
	sc, _, err := s.scroll(ctx, f)
	if err != nil {
		return k, err
	}
	k.AvgScrollDepth = sc.AvgDepth
	k.AvgTimeOnPageMs = sc.AvgTimeMs
	ee, tot, err := s.entryExit(ctx, f)
	if err != nil {
		return k, err
	}
	if f.PageURL != "" {
		p := ee[f.PageURL]
		k.Entries, k.Exits, k.Bounces = p.Entries, p.Exits, p.Bounces
		k.BounceRate = ratio(p.Bounces, p.Entries)
	} else {
		k.Entries, k.Exits, k.Bounces = tot.Sessions, tot.Sessions, tot.Bounces
		k.BounceRate = ratio(tot.Bounces, tot.Sessions)
		if tot.Sessions > 0 {
			k.PagesPerSession = round1(float64(tot.Pageviews) / float64(tot.Sessions))
		}
	}
	k.ExitRate = ratio(k.Exits, k.Pageviews)
	if k.Pageviews > 0 {
		k.ClicksPerView = round1(float64(k.Clicks) / float64(k.Pageviews))
	}
	return k, nil
}

// ---- scroll depth & engagement ----

type ScrollStats struct {
	Pageviews int64     `json:"pageviews"`
	AvgDepth  float64   `json:"avgDepth"`
	AvgTimeMs int64     `json:"avgTimeMs"`
	Reach     []Reach   `json:"reach"`   // share of pageviews reaching 25/50/75/100%
	Buckets   []float64 `json:"buckets"` // share of pageviews whose max depth falls in each 10% band
}

type Reach struct {
	Depth     int     `json:"depth"`
	Pageviews int64   `json:"pageviews"`
	Share     float64 `json:"share"`
}

type pageScroll struct {
	depthSum float64
	n        int64
	timeSum  int64
	timeN    int64
}

// scroll computes per-pageview max scroll depth; it also returns per-page sums.
func (s *Store) scroll(ctx context.Context, f Filter) (ScrollStats, map[string]*pageScroll, error) {
	c := f.base(true)
	c.add("type IN ('pageview','scroll')")
	c.add("pageview_id <> ''")
	rows, err := s.r.QueryContext(ctx, `SELECT MAX(page_url), MAX(COALESCE(scroll_depth, 0)), MAX(duration_ms)
		FROM events`+c.sql()+` GROUP BY pageview_id`, c.args...)
	if err != nil {
		return ScrollStats{}, nil, err
	}
	defer rows.Close()
	thresholds := []int{25, 50, 75, 100}
	reach := make([]int64, len(thresholds))
	buckets := make([]int64, 10)
	per := map[string]*pageScroll{}
	var n, timeN int64
	var depthSum float64
	var timeSum int64
	for rows.Next() {
		var page string
		var d float64
		var dur int64
		if err := rows.Scan(&page, &d, &dur); err != nil {
			return ScrollStats{}, nil, err
		}
		n++
		depthSum += d
		for i, t := range thresholds {
			// Rounding and sub-pixel layout keep "bottom of page" a hair under 100.
			if d >= float64(t)-1 {
				reach[i]++
			}
		}
		b := int(d / 10)
		if b > 9 {
			b = 9
		}
		if b < 0 {
			b = 0
		}
		buckets[b]++
		ps := per[page]
		if ps == nil {
			ps = &pageScroll{}
			per[page] = ps
		}
		ps.depthSum += d
		ps.n++
		if dur > 0 {
			timeSum += dur
			timeN++
			ps.timeSum += dur
			ps.timeN++
		}
	}
	if err := rows.Err(); err != nil {
		return ScrollStats{}, nil, err
	}
	st := ScrollStats{Pageviews: n, Reach: make([]Reach, len(thresholds)), Buckets: make([]float64, 10)}
	for i, t := range thresholds {
		st.Reach[i] = Reach{Depth: t, Pageviews: reach[i], Share: ratio(reach[i], n)}
	}
	for i := range buckets {
		st.Buckets[i] = ratio(buckets[i], n)
	}
	if n > 0 {
		st.AvgDepth = round1(depthSum / float64(n))
	}
	if timeN > 0 {
		st.AvgTimeMs = timeSum / timeN
	}
	return st, per, nil
}

func (s *Store) Scroll(ctx context.Context, f Filter) (ScrollStats, error) {
	st, _, err := s.scroll(ctx, f)
	return st, err
}

// ---- entries / exits ----

type pageEntryExit struct{ Entries, Exits, Bounces int64 }

type sessionTotals struct{ Sessions, Bounces, Pageviews int64 }

// entryExit derives entry pages (first pageview per session), exit pages
// (last pageview per session) and bounces (single-pageview sessions).
// The page filter is intentionally ignored: entries/exits are session-level.
func (s *Store) entryExit(ctx context.Context, f Filter) (map[string]pageEntryExit, sessionTotals, error) {
	c := f.base(false)
	c.add("type = 'pageview'")
	rows, err := s.r.QueryContext(ctx, `WITH pv AS (
			SELECT page_url,
				ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY ts, id) AS rf,
				ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY ts DESC, id DESC) AS rl,
				COUNT(*) OVER (PARTITION BY session_id) AS c
			FROM events`+c.sql()+`
		)
		SELECT page_url, SUM(rf = 1), SUM(rl = 1), SUM(rf = 1 AND c = 1), SUM(CASE WHEN rf = 1 THEN c ELSE 0 END)
		FROM pv WHERE rf = 1 OR rl = 1 GROUP BY page_url`, c.args...)
	if err != nil {
		return nil, sessionTotals{}, err
	}
	defer rows.Close()
	out := map[string]pageEntryExit{}
	var tot sessionTotals
	for rows.Next() {
		var u string
		var p pageEntryExit
		var pvs int64
		if err := rows.Scan(&u, &p.Entries, &p.Exits, &p.Bounces, &pvs); err != nil {
			return nil, tot, err
		}
		out[u] = p
		tot.Sessions += p.Entries
		tot.Bounces += p.Bounces
		tot.Pageviews += pvs
	}
	return out, tot, rows.Err()
}

type PageCount struct {
	URL   string  `json:"url"`
	Count int64   `json:"count"`
	Share float64 `json:"share"`
}

func topPages(m map[string]pageEntryExit, total int64, limit int, pick func(pageEntryExit) int64) []PageCount {
	out := []PageCount{}
	for u, p := range m {
		if v := pick(p); v > 0 {
			out = append(out, PageCount{URL: u, Count: v, Share: ratio(v, total)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].URL < out[j].URL
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

type EntryExit struct {
	Entries    []PageCount `json:"entries"`
	Exits      []PageCount `json:"exits"`
	ExitPoints []ExitPoint `json:"exitPoints"`
}

func (s *Store) EntryExit(ctx context.Context, f Filter, limit int) (EntryExit, error) {
	m, tot, err := s.entryExit(ctx, f)
	if err != nil {
		return EntryExit{}, err
	}
	pts, err := s.ExitPoints(ctx, f, limit)
	if err != nil {
		return EntryExit{}, err
	}
	return EntryExit{
		Entries:    topPages(m, tot.Sessions, limit, func(p pageEntryExit) int64 { return p.Entries }),
		Exits:      topPages(m, tot.Sessions, limit, func(p pageEntryExit) int64 { return p.Exits }),
		ExitPoints: pts,
	}, nil
}

// ExitPoint is the last element clicked on a session's final page view
// (empty selector: the visitor left without clicking).
type ExitPoint struct {
	PageURL  string  `json:"pageUrl"`
	Selector string  `json:"selector"`
	Label    string  `json:"label"`
	Tag      string  `json:"tag"`
	Count    int64   `json:"count"`
	Share    float64 `json:"share"`
}

func (s *Store) ExitPoints(ctx context.Context, f Filter, limit int) ([]ExitPoint, error) {
	c := f.base(false)
	c.add("type = 'pageview'")
	args := append([]any{}, c.args...)
	args = append(args, f.PageURL, f.PageURL, f.SiteID, limit)
	rows, err := s.r.QueryContext(ctx, `WITH last_pv AS (
			SELECT pageview_id, page_url FROM (
				SELECT pageview_id, page_url,
					ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY ts DESC, id DESC) AS rn
				FROM events`+c.sql()+`
			) WHERE rn = 1 AND (? = '' OR page_url = ?)
		), last_click AS (
			SELECT e.pageview_id, e.selector, e.el_label, e.el_text, e.el_id, e.el_tag,
				ROW_NUMBER() OVER (PARTITION BY e.pageview_id ORDER BY e.ts DESC, e.id DESC) AS rn
			FROM events e JOIN last_pv l ON e.pageview_id = l.pageview_id
			WHERE e.site_id = ? AND e.type = 'click'
		)
		SELECT l.page_url, COALESCE(lc.selector, ''), COALESCE(lc.el_label, ''), COALESCE(lc.el_text, ''),
			COALESCE(lc.el_id, ''), COALESCE(lc.el_tag, ''), COUNT(*),
			SUM(COUNT(*)) OVER ()
		FROM last_pv l LEFT JOIN last_click lc ON lc.pageview_id = l.pageview_id AND lc.rn = 1
		GROUP BY 1, 2, 3, 4, 5, 6
		ORDER BY 7 DESC, 1, 2 LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExitPoint{}
	for rows.Next() {
		var p ExitPoint
		var label, text, id string
		var total int64
		if err := rows.Scan(&p.PageURL, &p.Selector, &label, &text, &id, &p.Tag, &p.Count, &total); err != nil {
			return nil, err
		}
		if p.Selector == "" {
			p.Label = "(left without clicking)"
		} else {
			p.Label = ElementLabel(label, text, id, p.Tag)
		}
		p.Share = ratio(p.Count, total)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- pages table ----

type PageRow struct {
	URL            string  `json:"url"`
	Path           string  `json:"path"`
	Clicks         int64   `json:"clicks"`
	UniqueVisitors int64   `json:"uniqueVisitors"`
	Pageviews      int64   `json:"pageviews"`
	AvgScrollDepth float64 `json:"avgScrollDepth"`
	AvgTimeMs      int64   `json:"avgTimeMs"`
	Entries        int64   `json:"entries"`
	Exits          int64   `json:"exits"`
	ExitRate       float64 `json:"exitRate"`
	BounceRate     float64 `json:"bounceRate"`
	RageClicks     int64   `json:"rageClicks"`
	DeadClicks     int64   `json:"deadClicks"`
	FirstSeen      int64   `json:"firstSeen"`
	LastSeen       int64   `json:"lastSeen"`
	HasScreenshot  bool    `json:"hasScreenshot"`
}

type PageList struct {
	Total  int       `json:"total"`
	Limit  int       `json:"limit"`
	Offset int       `json:"offset"`
	Sort   string    `json:"sort"`
	Order  string    `json:"order"`
	Rows   []PageRow `json:"rows"`
}

type PageQuery struct {
	Sort   string // clicks | uniqueVisitors | pageviews | avgScrollDepth | entries | exits | exitRate | url | lastSeen
	Order  string // asc | desc
	Limit  int
	Offset int
	Search string
}

func pathOf(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[j:]
		}
		return "/"
	}
	return u
}

func (s *Store) Pages(ctx context.Context, f Filter, q PageQuery) (PageList, error) {
	c := f.base(false)
	if q.Search != "" {
		c.add("instr(page_url, ?) > 0", q.Search)
	}
	rows, err := s.r.QueryContext(ctx, `SELECT page_url,
		COALESCE(SUM(type = 'click'), 0), COALESCE(SUM(type = 'pageview'), 0), COUNT(DISTINCT session_id),
		COALESCE(SUM(type = 'click' AND rage = 1), 0), COALESCE(SUM(type = 'click' AND dead = 1), 0),
		MIN(ts), MAX(ts)
		FROM events`+c.sql()+` GROUP BY page_url`, c.args...)
	if err != nil {
		return PageList{}, err
	}
	list := []PageRow{}
	for rows.Next() {
		var r PageRow
		if err := rows.Scan(&r.URL, &r.Clicks, &r.Pageviews, &r.UniqueVisitors, &r.RageClicks, &r.DeadClicks, &r.FirstSeen, &r.LastSeen); err != nil {
			rows.Close()
			return PageList{}, err
		}
		r.Path = pathOf(r.URL)
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PageList{}, err
	}
	_, per, err := s.scroll(ctx, Filter{SiteID: f.SiteID, From: f.From, To: f.To, Device: f.Device})
	if err != nil {
		return PageList{}, err
	}
	ee, _, err := s.entryExit(ctx, f)
	if err != nil {
		return PageList{}, err
	}
	shots, err := s.screenshotSet(ctx, f.SiteID)
	if err != nil {
		return PageList{}, err
	}
	for i := range list {
		r := &list[i]
		if ps := per[r.URL]; ps != nil && ps.n > 0 {
			r.AvgScrollDepth = round1(ps.depthSum / float64(ps.n))
			if ps.timeN > 0 {
				r.AvgTimeMs = ps.timeSum / ps.timeN
			}
		}
		p := ee[r.URL]
		r.Entries, r.Exits = p.Entries, p.Exits
		r.ExitRate = ratio(p.Exits, r.Pageviews)
		r.BounceRate = ratio(p.Bounces, p.Entries)
		r.HasScreenshot = shots[r.URL]
	}
	key := map[string]func(r *PageRow) float64{
		"clicks":         func(r *PageRow) float64 { return float64(r.Clicks) },
		"uniqueVisitors": func(r *PageRow) float64 { return float64(r.UniqueVisitors) },
		"pageviews":      func(r *PageRow) float64 { return float64(r.Pageviews) },
		"avgScrollDepth": func(r *PageRow) float64 { return r.AvgScrollDepth },
		"avgTimeMs":      func(r *PageRow) float64 { return float64(r.AvgTimeMs) },
		"entries":        func(r *PageRow) float64 { return float64(r.Entries) },
		"exits":          func(r *PageRow) float64 { return float64(r.Exits) },
		"exitRate":       func(r *PageRow) float64 { return r.ExitRate },
		"bounceRate":     func(r *PageRow) float64 { return r.BounceRate },
		"rageClicks":     func(r *PageRow) float64 { return float64(r.RageClicks) },
		"lastSeen":       func(r *PageRow) float64 { return float64(r.LastSeen) },
	}
	if q.Sort != "url" && key[q.Sort] == nil {
		q.Sort = "clicks"
	}
	if q.Order != "asc" {
		q.Order = "desc"
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := &list[i], &list[j]
		if q.Sort == "url" {
			if q.Order == "asc" {
				return a.Path < b.Path
			}
			return a.Path > b.Path
		}
		ka, kb := key[q.Sort](a), key[q.Sort](b)
		if ka == kb {
			return a.URL < b.URL
		}
		if q.Order == "asc" {
			return ka < kb
		}
		return ka > kb
	})
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 50
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	out := PageList{Total: len(list), Limit: q.Limit, Offset: q.Offset, Sort: q.Sort, Order: q.Order, Rows: []PageRow{}}
	if q.Offset < len(list) {
		end := q.Offset + q.Limit
		if end > len(list) {
			end = len(list)
		}
		out.Rows = list[q.Offset:end]
	}
	return out, nil
}

// ---- time series ----

type Day struct {
	Date      string `json:"date"`
	Clicks    int64  `json:"clicks"`
	Pageviews int64  `json:"pageviews"`
	Visitors  int64  `json:"visitors"`
}

func (s *Store) Daily(ctx context.Context, f Filter) ([]Day, error) {
	c := f.base(true)
	off := f.tzSeconds()
	args := append([]any{off}, c.args...)
	rows, err := s.r.QueryContext(ctx, `SELECT (ts / 1000 + ?) / 86400 AS d,
		COALESCE(SUM(type = 'click'), 0), COALESCE(SUM(type = 'pageview'), 0), COUNT(DISTINCT session_id)
		FROM events`+c.sql()+` GROUP BY d ORDER BY d`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[int64]Day{}
	var minD, maxD int64 = math.MaxInt64, math.MinInt64
	for rows.Next() {
		var d int64
		var v Day
		if err := rows.Scan(&d, &v.Clicks, &v.Pageviews, &v.Visitors); err != nil {
			return nil, err
		}
		byDay[d] = v
		minD, maxD = min(minD, d), max(maxD, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if f.From > 0 {
		minD = (f.From/1000 + off) / 86400
	}
	if f.To > 0 {
		maxD = ((f.To-1)/1000 + off) / 86400
	}
	out := []Day{}
	if minD > maxD {
		return out, nil
	}
	if maxD-minD > 400 {
		minD = maxD - 400
	}
	for d := minD; d <= maxD; d++ {
		v := byDay[d]
		v.Date = time.Unix(d*86400, 0).UTC().Format("2006-01-02")
		out = append(out, v)
	}
	return out, nil
}

// TimeOfDay is click volume by local hour and weekday (0 = Sunday).
type TimeOfDay struct {
	Hours  [24]int64     `json:"hours"`
	Days   [7]int64      `json:"days"`
	Matrix [7][24]int64  `json:"matrix"`
	Total  int64         `json:"total"`
	Peak   TimeOfDayPeak `json:"peak"`
}

type TimeOfDayPeak struct {
	Hour int `json:"hour"`
	Day  int `json:"day"`
}

func (s *Store) TimeOfDay(ctx context.Context, f Filter) (TimeOfDay, error) {
	c := f.base(true)
	c.add("type = 'click'")
	off := f.tzSeconds()
	args := append([]any{off, off}, c.args...)
	rows, err := s.r.QueryContext(ctx, `SELECT ((ts / 1000 + ?) / 86400 + 4) % 7 AS dow, ((ts / 1000 + ?) % 86400) / 3600 AS h, COUNT(*)
		FROM events`+c.sql()+` GROUP BY dow, h`, args...)
	if err != nil {
		return TimeOfDay{}, err
	}
	defer rows.Close()
	var t TimeOfDay
	for rows.Next() {
		var d, h int
		var n int64
		if err := rows.Scan(&d, &h, &n); err != nil {
			return t, err
		}
		if d < 0 || d > 6 || h < 0 || h > 23 {
			continue
		}
		t.Matrix[d][h] += n
		t.Hours[h] += n
		t.Days[d] += n
		t.Total += n
	}
	for h := range t.Hours {
		if t.Hours[h] > t.Hours[t.Peak.Hour] {
			t.Peak.Hour = h
		}
	}
	for d := range t.Days {
		if t.Days[d] > t.Days[t.Peak.Day] {
			t.Peak.Day = d
		}
	}
	return t, rows.Err()
}

// ---- devices ----

type DeviceStat struct {
	Device    string  `json:"device"`
	Pageviews int64   `json:"pageviews"`
	Visitors  int64   `json:"visitors"`
	Clicks    int64   `json:"clicks"`
	Share     float64 `json:"share"`
}

func (s *Store) Devices(ctx context.Context, f Filter) ([]DeviceStat, error) {
	f.Device = ""
	c := f.base(true)
	rows, err := s.r.QueryContext(ctx, `SELECT
		CASE WHEN viewport_w <= 0 THEN 'unknown' WHEN viewport_w < 768 THEN 'mobile' WHEN viewport_w < 1024 THEN 'tablet' ELSE 'desktop' END AS dev,
		COALESCE(SUM(type = 'pageview'), 0), COUNT(DISTINCT session_id), COALESCE(SUM(type = 'click'), 0)
		FROM events`+c.sql()+` GROUP BY dev ORDER BY 2 DESC`, c.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceStat{}
	var total int64
	for rows.Next() {
		var d DeviceStat
		if err := rows.Scan(&d.Device, &d.Pageviews, &d.Visitors, &d.Clicks); err != nil {
			return nil, err
		}
		total += d.Pageviews
		out = append(out, d)
	}
	for i := range out {
		out[i].Share = ratio(out[i].Pageviews, total)
	}
	return out, rows.Err()
}

// ---- elements ----

type ElementStat struct {
	PageURL  string  `json:"pageUrl"`
	Selector string  `json:"selector"`
	Label    string  `json:"label"`
	Tag      string  `json:"tag"`
	ID       string  `json:"id"`
	Classes  string  `json:"classes"`
	Text     string  `json:"text"`
	Aria     string  `json:"ariaLabel"`
	Clicks   int64   `json:"clicks"`
	Visitors int64   `json:"visitors"`
	Rage     int64   `json:"rageClicks"`
	Dead     int64   `json:"deadClicks"`
	Share    float64 `json:"share"`
	RelX     float64 `json:"relX"` // mean click position relative to document size (0..1)
	RelY     float64 `json:"relY"`
}

type ElementList struct {
	TotalClicks int64         `json:"totalClicks"`
	Rows        []ElementStat `json:"rows"`
}

// ElementLabel picks the most human-readable description of an element.
func ElementLabel(aria, text, id, tag string) string {
	switch {
	case aria != "":
		return aria
	case text != "":
		return text
	case id != "":
		return "#" + id
	case tag != "":
		return "<" + tag + ">"
	}
	return "(unknown element)"
}

func (s *Store) Elements(ctx context.Context, f Filter, limit, offset int) (ElementList, error) {
	c := f.base(true)
	c.add("type = 'click'")
	var total int64
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`+c.sql(), c.args...).Scan(&total); err != nil {
		return ElementList{}, err
	}
	args := append(append([]any{}, c.args...), limit, offset)
	rows, err := s.r.QueryContext(ctx, `SELECT page_url, selector, el_tag, el_id, el_classes, el_text, el_label,
		COUNT(*), COUNT(DISTINCT session_id), SUM(rage), SUM(dead),
		COALESCE(AVG(x / NULLIF(doc_w, 0)), 0), COALESCE(AVG(y / NULLIF(doc_h, 0)), 0)
		FROM events`+c.sql()+`
		GROUP BY page_url, selector, el_tag, el_id, el_classes, el_text, el_label
		ORDER BY 8 DESC, 2 LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return ElementList{}, err
	}
	defer rows.Close()
	out := ElementList{TotalClicks: total, Rows: []ElementStat{}}
	for rows.Next() {
		var e ElementStat
		if err := rows.Scan(&e.PageURL, &e.Selector, &e.Tag, &e.ID, &e.Classes, &e.Text, &e.Aria,
			&e.Clicks, &e.Visitors, &e.Rage, &e.Dead, &e.RelX, &e.RelY); err != nil {
			return out, err
		}
		e.Label = ElementLabel(e.Aria, e.Text, e.ID, e.Tag)
		e.Share = ratio(e.Clicks, total)
		e.RelX, e.RelY = math.Round(e.RelX*1000)/1000, math.Round(e.RelY*1000)/1000
		out.Rows = append(out.Rows, e)
	}
	return out, rows.Err()
}

// ---- navigation flow ----

type FlowEdge struct {
	URL      string  `json:"url"`
	Kind     string  `json:"kind"`
	External bool    `json:"external"`
	Count    int64   `json:"count"`
	Share    float64 `json:"share"`
}

type Flow struct {
	Pageviews  int64       `json:"pageviews"`
	Inbound    []FlowEdge  `json:"inbound"`
	Outbound   []FlowEdge  `json:"outbound"`
	Unloads    int64       `json:"unloads"`
	ExitPoints []ExitPoint `json:"exitPoints"`
}

// Flow summarises how visitors arrive at (pageview referrers, SPA transitions)
// and leave (link clicks, SPA transitions, unloads) a page.
func (s *Store) Flow(ctx context.Context, f Filter, limit int) (Flow, error) {
	out := Flow{Inbound: []FlowEdge{}, Outbound: []FlowEdge{}}
	pageHost := HostOf(f.PageURL)

	c := f.base(true)
	c.add("type = 'pageview'")
	rows, err := s.r.QueryContext(ctx, `SELECT referrer, nav_kind, COUNT(*), SUM(COUNT(*)) OVER ()
		FROM events`+c.sql()+` GROUP BY referrer, nav_kind ORDER BY 3 DESC LIMIT ?`, append(c.args, limit)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var e FlowEdge
		var total int64
		if err := rows.Scan(&e.URL, &e.Kind, &e.Count, &total); err != nil {
			rows.Close()
			return out, err
		}
		if e.URL == "" {
			e.URL = "(direct)"
		} else {
			e.External = HostOf(e.URL) != pageHost
		}
		out.Pageviews = total
		e.Share = ratio(e.Count, total)
		out.Inbound = append(out.Inbound, e)
	}
	rows.Close()

	c = f.base(true)
	c.add("type = 'navigation'")
	c.add("nav_kind IN ('link','pushstate','replacestate','popstate')")
	rows, err = s.r.QueryContext(ctx, `SELECT target_url, nav_kind, COUNT(*), SUM(COUNT(*)) OVER ()
		FROM events`+c.sql()+` GROUP BY target_url, nav_kind ORDER BY 3 DESC LIMIT ?`, append(c.args, limit)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var e FlowEdge
		var total int64
		if err := rows.Scan(&e.URL, &e.Kind, &e.Count, &total); err != nil {
			rows.Close()
			return out, err
		}
		e.External = HostOf(e.URL) != pageHost
		e.Share = ratio(e.Count, total)
		out.Outbound = append(out.Outbound, e)
	}
	rows.Close()

	c = f.base(true)
	c.add("type = 'navigation'")
	c.add("nav_kind = 'unload'")
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`+c.sql(), c.args...).Scan(&out.Unloads); err != nil {
		return out, err
	}
	out.ExitPoints, err = s.ExitPoints(ctx, f, limit)
	return out, err
}

// ---- referrers ----

type Referrer struct {
	Source string  `json:"source"`
	Count  int64   `json:"count"`
	Share  float64 `json:"share"`
}

// Referrers groups landing pageviews (full page loads) by referring host;
// internal referrers (same domain as the site) are excluded.
func (s *Store) Referrers(ctx context.Context, f Filter, siteDomain string, limit int) ([]Referrer, error) {
	c := f.base(true)
	c.add("type = 'pageview'")
	c.add("nav_kind = 'load'")
	rows, err := s.r.QueryContext(ctx, `SELECT referrer, COUNT(*) FROM events`+c.sql()+` GROUP BY referrer`, c.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	agg := map[string]int64{}
	var total int64
	for rows.Next() {
		var ref string
		var n int64
		if err := rows.Scan(&ref, &n); err != nil {
			return nil, err
		}
		src := "(direct)"
		if ref != "" {
			h := HostOf(ref)
			if h == "" || DomainMatches(h, siteDomain) {
				continue
			}
			src = strings.TrimPrefix(h, "www.")
		}
		agg[src] += n
		total += n
	}
	out := []Referrer{}
	for k, v := range agg {
		out = append(out, Referrer{Source: k, Count: v, Share: ratio(v, total)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Source < out[j].Source
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, rows.Err()
}

// DomainMatches reports whether host is domain or one of its subdomains.
func DomainMatches(host, domain string) bool {
	host, domain = strings.ToLower(host), strings.ToLower(domain)
	return domain != "" && (host == domain || strings.HasSuffix(host, "."+domain))
}

// ---- heatmap ----

type Cell struct {
	X int   `json:"x"`
	Y int   `json:"y"`
	N int64 `json:"n"`
}

type Heatmap struct {
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	Total     int64  `json:"total"`
	Max       int64  `json:"max"`
	Visitors  int64  `json:"visitors"`
	DocW      int    `json:"docW"`
	DocH      int    `json:"docH"`
	ViewportW int    `json:"viewportW"`
	Source    string `json:"source"` // rollup | raw
	Cells     []Cell `json:"cells"`
}

func heatTypes(types []string) []string {
	out := []string{}
	for _, t := range types {
		if t == TypeClick || t == TypeNavigation {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		out = []string{TypeClick}
	}
	return out
}

// Heatmap bins positioned events into cols x rows cells relative to document
// size. Hour-aligned ranges are served from the pre-aggregated rollup table.
func (s *Store) Heatmap(ctx context.Context, f Filter, cols, rows int) (Heatmap, error) {
	types := heatTypes(f.Types)
	h := Heatmap{Cols: cols, Rows: rows, Cells: []Cell{}}
	const hour = 3_600_000
	useRollup := f.From%hour == 0 && f.To%hour == 0

	var q string
	var args []any
	if useRollup {
		h.Source = "rollup"
		c := &cond{}
		c.add("site_id = ?", f.SiteID)
		c.add("page_url = ?", f.PageURL)
		c.add("type IN ("+placeholders(len(types))+")", toAny(types)...)
		if f.From > 0 {
			c.add("hour >= ?", f.From/hour)
		}
		if f.To > 0 {
			c.add("hour < ?", f.To/hour)
		}
		if f.Device != "" {
			c.add("device = ?", f.Device)
		}
		q = `SELECT MIN(gx * ? / ` + itoa(RollupCols) + `, ? - 1) AS cx, MIN(gy * ? / ` + itoa(RollupRows) + `, ? - 1) AS cy, SUM(n)
			FROM heatmap_rollup` + c.sql() + ` GROUP BY cx, cy`
		args = append([]any{cols, cols, rows, rows}, c.args...)
	} else {
		h.Source = "raw"
		c := f.base(true)
		c.add("type IN ("+placeholders(len(types))+")", toAny(types)...)
		c.add("x IS NOT NULL AND x >= 0 AND y >= 0 AND doc_w > 0 AND doc_h > 0")
		q = `SELECT MIN(CAST(x * ? / doc_w AS INTEGER), ? - 1) AS cx, MIN(CAST(y * ? / doc_h AS INTEGER), ? - 1) AS cy, COUNT(*)
			FROM events` + c.sql() + ` GROUP BY cx, cy`
		args = append([]any{cols, cols, rows, rows}, c.args...)
	}
	rs, err := s.r.QueryContext(ctx, q, args...)
	if err != nil {
		return h, err
	}
	for rs.Next() {
		var cell Cell
		if err := rs.Scan(&cell.X, &cell.Y, &cell.N); err != nil {
			rs.Close()
			return h, err
		}
		h.Total += cell.N
		if cell.N > h.Max {
			h.Max = cell.N
		}
		h.Cells = append(h.Cells, cell)
	}
	rs.Close()
	if err := rs.Err(); err != nil {
		return h, err
	}

	c := f.base(true)
	c.add("type IN ("+placeholders(len(types))+")", toAny(types)...)
	c.add("x IS NOT NULL")
	err = s.r.QueryRowContext(ctx, `SELECT COUNT(DISTINCT session_id), CAST(COALESCE(AVG(doc_w), 0) AS INTEGER),
		CAST(COALESCE(AVG(doc_h), 0) AS INTEGER), CAST(COALESCE(AVG(viewport_w), 0) AS INTEGER)
		FROM events`+c.sql(), c.args...).Scan(&h.Visitors, &h.DocW, &h.DocH, &h.ViewportW)
	return h, err
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
