package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultComponentAttr is recorded by the tracker on every click (closest
// ancestor value), so components tagged with it need no tracker config.
const DefaultComponentAttr = "data-calor-component"

type Component struct {
	ID          string `json:"id"`
	SiteID      string `json:"siteId"`
	Name        string `json:"name"`
	Selector    string `json:"selector"`
	AttrName    string `json:"attrName"`
	AttrValue   string `json:"attrValue"`
	PagePattern string `json:"pagePattern"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// SameRules reports whether a and b match the same clicks.
func (a Component) SameRules(b Component) bool {
	return a.Selector == b.Selector && a.AttrName == b.AttrName && a.AttrValue == b.AttrValue && a.PagePattern == b.PagePattern
}

// ComponentMatcher is a compiled component: a click belongs to it when it is
// on a page matching PagePattern and inside an element matching the CSS
// selector or carrying the attribute (either rule suffices).
type ComponentMatcher struct {
	ID          string
	sel         *Selector
	attrName    string
	attrValue   string
	pagePattern string
}

// AttrSelector renders the attribute rule as a CSS attribute selector.
func AttrSelector(name, value string) string {
	if name == "" {
		return ""
	}
	if value == "" {
		return "[" + name + "]"
	}
	return "[" + name + `="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"]`
}

// BrowserSelector is the single selector the tracker evaluates with closest().
func (c Component) BrowserSelector() string {
	parts := []string{}
	if c.Selector != "" {
		parts = append(parts, c.Selector)
	}
	if a := AttrSelector(c.AttrName, c.AttrValue); a != "" {
		parts = append(parts, a)
	}
	return strings.Join(parts, ", ")
}

func CompileComponent(c Component) (*ComponentMatcher, error) {
	m := &ComponentMatcher{ID: c.ID, attrName: c.AttrName, attrValue: c.AttrValue, pagePattern: c.PagePattern}
	if c.Selector != "" {
		sel, err := ParseSelector(c.Selector)
		if err != nil {
			return nil, err
		}
		m.sel = sel
	}
	return m, nil
}

// ServerEvaluable reports whether recorded clicks can be matched without the
// browser, which is what backfill and previews rely on.
func (m *ComponentMatcher) ServerEvaluable() bool {
	if m.attrName != "" && m.attrName != DefaultComponentAttr {
		return false
	}
	return m.sel == nil || m.sel.ServerEvaluable()
}

func (m *ComponentMatcher) Match(c *Click) bool {
	var els []elem
	return m.match(c, &els)
}

// match lazily builds the click's ancestry into *els so callers matching many
// components parse the recorded selector once.
func (m *ComponentMatcher) match(c *Click, els *[]elem) bool {
	if !MatchPagePattern(m.pagePattern, c.PageURL) {
		return false
	}
	if m.attrName == DefaultComponentAttr && c.Component != "" && (m.attrValue == "" || m.attrValue == c.Component) {
		return true
	}
	if m.sel == nil {
		return false
	}
	if *els == nil {
		*els, _ = c.chain()
	}
	return m.sel.matchChain(c, *els)
}

// MatchComponents appends to dst the ids of the components in ms that contain c
// and are not already in dst.
func MatchComponents(ms []*ComponentMatcher, c *Click, dst []string) []string {
	var els []elem
next:
	for _, m := range ms {
		for _, id := range dst {
			if id == m.ID {
				continue next
			}
		}
		if m.match(c, &els) {
			dst = append(dst, m.ID)
		}
	}
	return dst
}

// MatchPage reports whether the component applies to pageURL at all.
func (m *ComponentMatcher) MatchPage(pageURL string) bool {
	return MatchPagePattern(m.pagePattern, pageURL)
}

func NewComponentID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "cmp_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

const componentCols = `id, site_id, name, selector, attr_name, attr_value, page_pattern, created_at, updated_at`

func scanComponent(sc interface{ Scan(...any) error }) (Component, error) {
	var c Component
	err := sc.Scan(&c.ID, &c.SiteID, &c.Name, &c.Selector, &c.AttrName, &c.AttrValue, &c.PagePattern, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (s *Store) ListComponents(ctx context.Context, siteID string) ([]Component, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+componentCols+` FROM components WHERE site_id = ? ORDER BY name, id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Component{}
	for rows.Next() {
		c, err := scanComponent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetComponent(ctx context.Context, siteID, id string) (Component, error) {
	c, err := scanComponent(s.r.QueryRowContext(ctx, `SELECT `+componentCols+` FROM components WHERE site_id = ? AND id = ?`, siteID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Component{}, ErrNotFound
	}
	return c, err
}

func (s *Store) CountComponents(ctx context.Context, siteID string) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM components WHERE site_id = ?`, siteID).Scan(&n)
	return n, err
}

func (s *Store) CreateComponent(ctx context.Context, c Component) (Component, error) {
	if c.ID == "" {
		c.ID = NewComponentID()
	}
	c.CreatedAt = time.Now().UnixMilli()
	c.UpdatedAt = c.CreatedAt
	_, err := s.w.ExecContext(ctx, `INSERT INTO components (`+componentCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		c.ID, c.SiteID, c.Name, c.Selector, c.AttrName, c.AttrValue, c.PagePattern, c.CreatedAt, c.UpdatedAt)
	if err != nil && (strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY KEY")) {
		return Component{}, ErrExists
	}
	return c, err
}

func (s *Store) UpdateComponent(ctx context.Context, c Component) (Component, error) {
	c.UpdatedAt = time.Now().UnixMilli()
	res, err := s.w.ExecContext(ctx, `UPDATE components SET name = ?, selector = ?, attr_name = ?, attr_value = ?, page_pattern = ?, updated_at = ?
		WHERE site_id = ? AND id = ?`, c.Name, c.Selector, c.AttrName, c.AttrValue, c.PagePattern, c.UpdatedAt, c.SiteID, c.ID)
	if err != nil {
		return Component{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Component{}, ErrNotFound
	}
	return c, nil
}

func (s *Store) DeleteComponent(ctx context.Context, siteID, id string) error {
	if _, err := s.GetComponent(ctx, siteID, id); err != nil {
		return err
	}
	if err := s.clearComponentTags(ctx, id); err != nil {
		return err
	}
	_, err := s.w.ExecContext(ctx, `DELETE FROM components WHERE site_id = ? AND id = ?`, siteID, id)
	return err
}

func (s *Store) clearComponentTags(ctx context.Context, id string) error {
	_, err := s.deleteChunked(ctx, "event_components", "component_id, ts, event_id", "component_id = ?", id)
	return err
}

// allComponentIDs lists component ids of one site, or of every site when siteID is "".
func (s *Store) allComponentIDs(ctx context.Context, siteID string) ([]string, error) {
	q, args := `SELECT id FROM components`, []any{}
	if siteID != "" {
		q += ` WHERE site_id = ?`
		args = append(args, siteID)
	}
	rows, err := s.w.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// clickKey identifies clicks that are indistinguishable to the matcher.
func clickKey(c *Click) string {
	return c.PageURL + "\x00" + c.Selector + "\x00" + c.Tag + "\x00" + c.ID + "\x00" + c.Classes + "\x00" + c.Component
}

// backfillBatch bounds both the rows read per query and the tags written per
// transaction, so retagging a large site never holds much memory or WAL.
var backfillBatch = 5000

// RetagComponent replaces the stored tags of m with the clicks recorded for
// siteID that the server-side matcher accepts. It reads in id order with
// keyset pagination and memoises match results per distinct element, so the
// cost is one pass over the site's clicks plus one match per element.
func (s *Store) RetagComponent(ctx context.Context, siteID string, m *ComponentMatcher) (int64, error) {
	if err := s.clearComponentTags(ctx, m.ID); err != nil {
		return 0, err
	}
	memo := map[string]bool{}
	var last, tagged int64
	for {
		rows, err := s.r.QueryContext(ctx, `SELECT id, ts, page_url, selector, el_tag, el_id, el_classes, el_component FROM events
			WHERE site_id = ? AND type = 'click' AND id > ? ORDER BY id LIMIT ?`, siteID, last, backfillBatch)
		if err != nil {
			return tagged, err
		}
		var hits []tagRow
		n := 0
		for rows.Next() {
			var c Click
			var h tagRow
			if err := rows.Scan(&h.id, &h.ts, &c.PageURL, &c.Selector, &c.Tag, &c.ID, &c.Classes, &c.Component); err != nil {
				rows.Close()
				return tagged, err
			}
			n++
			last = h.id
			k := clickKey(&c)
			ok, seen := memo[k]
			if !seen {
				ok = m.Match(&c)
				if len(memo) > 100_000 {
					memo = map[string]bool{}
				}
				memo[k] = ok
			}
			if ok {
				hits = append(hits, h)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return tagged, err
		}
		if len(hits) > 0 {
			if err := s.insertTags(ctx, m.ID, hits); err != nil {
				return tagged, err
			}
			tagged += int64(len(hits))
		}
		if n < backfillBatch {
			return tagged, nil
		}
	}
}

type tagRow struct{ id, ts int64 }

func (s *Store) insertTags(ctx context.Context, componentID string, rows []tagRow) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO event_components (component_id, ts, event_id) VALUES (?,?,?)`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range rows {
		if _, err := st.ExecContext(ctx, componentID, r.ts, r.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- preview ----

type MatchElement struct {
	PageURL  string `json:"pageUrl"`
	Selector string `json:"selector"`
	Label    string `json:"label"`
	Clicks   int64  `json:"clicks"`
}

type MatchPreview struct {
	MatchedClicks int64          `json:"matchedClicks"`
	TotalClicks   int64          `json:"totalClicks"`
	Share         float64        `json:"share"`
	Pages         []PageCount    `json:"pages"`
	Elements      []MatchElement `json:"elements"`
	// Evaluable is false when the rule uses attributes the tracker does not
	// record; such components only collect clicks from new traffic.
	Evaluable bool `json:"evaluable"`
}

// PreviewComponent counts recorded clicks in f that m would tag. Clicks are
// grouped by element in SQL first, so the matcher runs once per element.
func (s *Store) PreviewComponent(ctx context.Context, f Filter, m *ComponentMatcher) (MatchPreview, error) {
	out := MatchPreview{Pages: []PageCount{}, Elements: []MatchElement{}, Evaluable: m.ServerEvaluable()}
	f.Component = ""
	c := f.base(true)
	c.add("type = 'click'")
	rows, err := s.r.QueryContext(ctx, `SELECT page_url, selector, el_tag, el_id, el_classes, el_component,
		MAX(el_text), MAX(el_label), COUNT(*)
		FROM events`+c.sql()+` GROUP BY page_url, selector, el_tag, el_id, el_classes, el_component`, c.args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	pages := map[string]int64{}
	for rows.Next() {
		var cl Click
		var text, label string
		var n int64
		if err := rows.Scan(&cl.PageURL, &cl.Selector, &cl.Tag, &cl.ID, &cl.Classes, &cl.Component, &text, &label, &n); err != nil {
			return out, err
		}
		out.TotalClicks += n
		if !m.Match(&cl) {
			continue
		}
		out.MatchedClicks += n
		pages[cl.PageURL] += n
		out.Elements = append(out.Elements, MatchElement{PageURL: cl.PageURL, Selector: cl.Selector, Label: ElementLabel(label, text, cl.ID, cl.Tag), Clicks: n})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.Share = ratio(out.MatchedClicks, out.TotalClicks)
	for u, n := range pages {
		out.Pages = append(out.Pages, PageCount{URL: u, Count: n, Share: ratio(n, out.MatchedClicks)})
	}
	sort.Slice(out.Pages, func(i, j int) bool {
		if out.Pages[i].Count != out.Pages[j].Count {
			return out.Pages[i].Count > out.Pages[j].Count
		}
		return out.Pages[i].URL < out.Pages[j].URL
	})
	sort.Slice(out.Elements, func(i, j int) bool {
		if out.Elements[i].Clicks != out.Elements[j].Clicks {
			return out.Elements[i].Clicks > out.Elements[j].Clicks
		}
		return out.Elements[i].Selector < out.Elements[j].Selector
	})
	if len(out.Pages) > 10 {
		out.Pages = out.Pages[:10]
	}
	if len(out.Elements) > 10 {
		out.Elements = out.Elements[:10]
	}
	return out, nil
}

// ---- per-component analytics ----

type ComponentSummary struct {
	Clicks   int64 `json:"clicks"`
	Visitors int64 `json:"visitors"`
	LastSeen int64 `json:"lastSeen"`
}

// ComponentSummaries returns click and visitor counts per component in f.
func (s *Store) ComponentSummaries(ctx context.Context, f Filter) (map[string]ComponentSummary, error) {
	c := &cond{}
	c.add("c.site_id = ?", f.SiteID)
	if f.From > 0 {
		c.add("ec.ts >= ?", f.From)
	}
	if f.To > 0 {
		c.add("ec.ts < ?", f.To)
	}
	if dc, ok := deviceCond("e.viewport_w", f.Device); ok {
		c.add(dc)
	}
	if f.PageURL != "" {
		c.add("e.page_url = ?", f.PageURL)
	}
	rows, err := s.r.QueryContext(ctx, `SELECT ec.component_id, COUNT(*), COUNT(DISTINCT e.session_id), MAX(ec.ts)
		FROM components c JOIN event_components ec ON ec.component_id = c.id JOIN events e ON e.id = ec.event_id`+
		c.sql()+` GROUP BY ec.component_id`, c.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ComponentSummary{}
	for rows.Next() {
		var id string
		var v ComponentSummary
		if err := rows.Scan(&id, &v.Clicks, &v.Visitors, &v.LastSeen); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

type ComponentStats struct {
	Clicks         int64       `json:"clicks"`
	Visitors       int64       `json:"visitors"`
	RageClicks     int64       `json:"rageClicks"`
	DeadClicks     int64       `json:"deadClicks"`
	SiteClicks     int64       `json:"siteClicks"`
	ClickShare     float64     `json:"clickShare"` // % of all clicks in range
	Pageviews      int64       `json:"exposedPageviews"`
	ClickedViews   int64       `json:"clickedPageviews"`
	ClickRate      float64     `json:"clickRate"` // % of exposed pageviews with a click on the component
	FirstSeen      int64       `json:"firstSeen"`
	LastSeen       int64       `json:"lastSeen"`
	Pages          []PageCount `json:"pages"`
	ExposureSource string      `json:"exposureSource"` // pattern | clicked-pages
}

// ComponentStats summarises one component. Exposure (the denominator of the
// click rate) is pageviews of pages matching the component's page pattern,
// or, without a pattern, of pages where the component was clicked at all.
func (s *Store) ComponentStats(ctx context.Context, f Filter, m *ComponentMatcher, hasPattern bool) (ComponentStats, error) {
	st := ComponentStats{Pages: []PageCount{}}
	scoped := f
	c := scoped.base(true)
	c.add("type = 'click'")
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT session_id), COALESCE(SUM(rage), 0), COALESCE(SUM(dead), 0),
		COUNT(DISTINCT pageview_id), COALESCE(MIN(ts), 0), COALESCE(MAX(ts), 0) FROM events`+c.sql(), c.args...).
		Scan(&st.Clicks, &st.Visitors, &st.RageClicks, &st.DeadClicks, &st.ClickedViews, &st.FirstSeen, &st.LastSeen); err != nil {
		return st, err
	}
	all := f
	all.Component = ""
	c = all.base(true)
	c.add("type = 'click'")
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`+c.sql(), c.args...).Scan(&st.SiteClicks); err != nil {
		return st, err
	}
	st.ClickShare = ratio(st.Clicks, st.SiteClicks)

	c = scoped.base(true)
	c.add("type = 'click'")
	rows, err := s.r.QueryContext(ctx, `SELECT page_url, COUNT(*) FROM events`+c.sql()+` GROUP BY page_url ORDER BY 2 DESC, 1`, c.args...)
	if err != nil {
		return st, err
	}
	clicked := map[string]bool{}
	for rows.Next() {
		var p PageCount
		if err := rows.Scan(&p.URL, &p.Count); err != nil {
			rows.Close()
			return st, err
		}
		clicked[p.URL] = true
		p.Share = ratio(p.Count, st.Clicks)
		st.Pages = append(st.Pages, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}
	if len(st.Pages) > 20 {
		st.Pages = st.Pages[:20]
	}

	c = all.base(true)
	c.add("type = 'pageview'")
	rows, err = s.r.QueryContext(ctx, `SELECT page_url, COUNT(*) FROM events`+c.sql()+` GROUP BY page_url`, c.args...)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	st.ExposureSource = "clicked-pages"
	if hasPattern {
		st.ExposureSource = "pattern"
	}
	for rows.Next() {
		var u string
		var n int64
		if err := rows.Scan(&u, &n); err != nil {
			return st, err
		}
		if (hasPattern && m.MatchPage(u)) || (!hasPattern && clicked[u]) {
			st.Pageviews += n
		}
	}
	st.ClickRate = ratio(st.ClickedViews, st.Pageviews)
	return st, rows.Err()
}

type DiscoveredAttr struct {
	Value     string `json:"value"`
	Clicks    int64  `json:"clicks"`
	Pages     int64  `json:"pages"`
	Component string `json:"componentId,omitempty"` // set when a component already targets this value
}

// DiscoveredAttributes lists data-calor-component values seen on clicks in f.
func (s *Store) DiscoveredAttributes(ctx context.Context, f Filter, limit int) ([]DiscoveredAttr, error) {
	f.Component = ""
	c := f.base(true)
	c.add("type = 'click'")
	c.add("el_component <> ''")
	rows, err := s.r.QueryContext(ctx, `SELECT el_component, COUNT(*), COUNT(DISTINCT page_url) FROM events`+c.sql()+
		` GROUP BY el_component ORDER BY 2 DESC, 1 LIMIT `+strconv.Itoa(limit), c.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscoveredAttr{}
	for rows.Next() {
		var d DiscoveredAttr
		if err := rows.Scan(&d.Value, &d.Clicks, &d.Pages); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
