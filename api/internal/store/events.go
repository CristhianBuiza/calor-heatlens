package store

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Event types.
const (
	TypeClick      = "click"
	TypeNavigation = "navigation"
	TypePageview   = "pageview"
	TypeScroll     = "scroll"
)

// Navigation kinds. Pageview events use load/pushstate/replacestate/popstate;
// navigation events use link/pushstate/replacestate/popstate/unload.
const (
	NavLoad         = "load"
	NavLink         = "link"
	NavPushState    = "pushstate"
	NavReplaceState = "replacestate"
	NavPopState     = "popstate"
	NavUnload       = "unload"
)

type Event struct {
	SiteID     string
	Type       string
	PageURL    string
	SessionID  string
	PageviewID string
	TS         int64 // unix ms
	X, Y       *float64
	ViewportW  int
	ViewportH  int
	DocW       int
	DocH       int

	Selector  string
	ElTag     string
	ElID      string
	ElClasses string
	ElText    string
	ElLabel   string
	// ElComponent is the closest data-calor-component value; Components are
	// the ids of the site's components containing the click.
	ElComponent string
	Components  []string
	Rage        bool
	Dead        bool

	TargetURL     string
	NavKind       string
	Referrer      string
	RedirectCount int

	ScrollDepth *float64 // 0..100
	DurationMs  int64
}

type Site struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	CreatedAt int64  `json:"createdAt"`
}

// rollupCell returns the fixed-resolution rollup cell for a positioned event.
func rollupCell(e *Event) (int, int, bool) {
	if e.X == nil || e.Y == nil || e.DocW <= 0 || e.DocH <= 0 || *e.X < 0 || *e.Y < 0 {
		return 0, 0, false
	}
	gx := int(*e.X * RollupCols / float64(e.DocW))
	gy := int(*e.Y * RollupRows / float64(e.DocH))
	if gx >= RollupCols {
		gx = RollupCols - 1
	}
	if gy >= RollupRows {
		gy = RollupRows - 1
	}
	return gx, gy, true
}

// EventColumns is the column order InsertEvents copies into events.
var EventColumns = []string{
	"id", "site_id", "type", "page_url", "session_id", "pageview_id", "ts", "x", "y",
	"viewport_w", "viewport_h", "doc_w", "doc_h",
	"selector", "el_tag", "el_id", "el_classes", "el_text", "el_label", "el_component", "rage", "dead",
	"target_url", "nav_kind", "referrer", "redirect_count", "scroll_depth", "duration_ms", "received_at",
}

type rollupKey struct {
	site, page, typ, device string
	hour                    int32
	gx, gy                  int16
}

// InsertEvents writes all events in one transaction: ids are reserved from
// the sequence up front so events and their component tags go in with COPY,
// and the hourly heatmap rollup is aggregated in Go and upserted in a single
// statement.
func (s *Store) InsertEvents(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tss := make([]int64, len(events))
	for i := range events {
		tss[i] = events[i].TS
	}
	if err := s.EnsurePartitions(ctx, tss...); err != nil {
		return err
	}
	tx, err := s.w.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Like SQLite's synchronous=NORMAL: a crash can lose the last few hundred
	// milliseconds of events but never corrupts; admin writes stay synchronous.
	if _, err := tx.Exec(ctx, `SET LOCAL synchronous_commit = off`); err != nil {
		return err
	}
	ids, err := reserveEventIDs(ctx, tx, len(events))
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"events"}, EventColumns, pgx.CopyFromSlice(len(events), func(i int) ([]any, error) {
		e := &events[i]
		return []any{ids[i], e.SiteID, e.Type, e.PageURL, e.SessionID, e.PageviewID, e.TS, e.X, e.Y,
			e.ViewportW, e.ViewportH, e.DocW, e.DocH,
			e.Selector, e.ElTag, e.ElID, e.ElClasses, e.ElText, e.ElLabel, e.ElComponent, e.Rage, e.Dead,
			e.TargetURL, e.NavKind, e.Referrer, e.RedirectCount, e.ScrollDepth, e.DurationMs, now}, nil
	})); err != nil {
		return err
	}

	var tags [][]any
	cells := map[rollupKey]int32{}
	for i := range events {
		e := &events[i]
		for j, c := range e.Components {
			if !contains(e.Components[:j], c) {
				tags = append(tags, []any{c, e.TS, ids[i]})
			}
		}
		if e.Type == TypeClick || e.Type == TypeNavigation {
			if gx, gy, ok := rollupCell(e); ok {
				cells[rollupKey{e.SiteID, e.PageURL, e.Type, DeviceOf(e.ViewportW), int32(e.TS / 3_600_000), int16(gx), int16(gy)}]++
			}
		}
	}
	if len(tags) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"event_components"}, []string{"component_id", "ts", "event_id"}, pgx.CopyFromRows(tags)); err != nil {
			return err
		}
	}
	if err := upsertRollup(ctx, tx, cells); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// reserveEventIDs takes n ascending ids from the events identity sequence,
// so ids follow batch order like SQLite's AUTOINCREMENT did.
func reserveEventIDs(ctx context.Context, tx pgx.Tx, n int) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT nextval(pg_get_serial_sequence('events', 'id')) FROM generate_series(1, $1) ORDER BY 1`, n)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err == nil && len(ids) != n {
		err = errors.New("reserved fewer event ids than requested")
	}
	return ids, err
}

// upsertRollup adds the per-cell counts in one statement. Keys are sorted so
// concurrent writers (the API and an import) lock rows in the same order.
func upsertRollup(ctx context.Context, tx pgx.Tx, cells map[rollupKey]int32) error {
	if len(cells) == 0 {
		return nil
	}
	keys := make([]rollupKey, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		switch {
		case a.site != b.site:
			return a.site < b.site
		case a.page != b.page:
			return a.page < b.page
		case a.typ != b.typ:
			return a.typ < b.typ
		case a.device != b.device:
			return a.device < b.device
		case a.hour != b.hour:
			return a.hour < b.hour
		case a.gx != b.gx:
			return a.gx < b.gx
		}
		return a.gy < b.gy
	})
	n := len(keys)
	site, page, typ, dev := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	hour, cnt := make([]int32, n), make([]int32, n)
	gx, gy := make([]int16, n), make([]int16, n)
	for i, k := range keys {
		site[i], page[i], typ[i], dev[i], hour[i], gx[i], gy[i], cnt[i] = k.site, k.page, k.typ, k.device, k.hour, k.gx, k.gy, cells[k]
	}
	_, err := tx.Exec(ctx, `INSERT INTO heatmap_rollup (site_id, page_url, type, device, hour, gx, gy, n)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::integer[], $6::smallint[], $7::smallint[], $8::integer[])
		ON CONFLICT (site_id, page_url, type, device, hour, gx, gy) DO UPDATE SET n = heatmap_rollup.n + excluded.n`,
		site, page, typ, dev, hour, gx, gy, cnt)
	return err
}

// PruneEvents deletes raw events older than cutoff (unix ms). Months that
// end before the cutoff are dropped as whole partitions; the rest of the
// boundary month is deleted in chunks. Heatmap rollups are kept so
// long-range heatmaps survive raw-event retention.
func (s *Store) PruneEvents(ctx context.Context, cutoff int64) (int64, error) {
	n, err := s.dropPartitionsBefore(ctx, cutoff)
	if err != nil {
		return n, err
	}
	ids, err := s.allComponentIDs(ctx, "")
	if err != nil {
		return n, err
	}
	for _, id := range ids {
		if _, err := s.deleteChunked(ctx, "event_components", "component_id = ? AND ts < ?", id, cutoff); err != nil {
			return n, err
		}
	}
	m, err := s.deleteChunked(ctx, "events", "ts < ?", cutoff)
	return n + m, err
}

// deleteChunk bounds rows per delete transaction, so one huge DELETE never
// holds row locks for long or produces a burst of WAL and dead tuples that
// autovacuum only sees at the end.
var deleteChunk = 5000

// deleteChunked deletes the rows of table matching where, deleteChunk rows per
// transaction. Each chunk selects the physical row ids (partition, ctid) of
// matching rows and deletes them per partition with a TID scan. A
// `DELETE … WHERE key IN (SELECT key … LIMIT n)` semi-join instead turns into
// a nested loop over every matching row whenever statistics underestimate
// them, as they do for a site created since the last ANALYZE. where is
// re-checked on delete, so a ctid reused by a new row in between is skipped.
func (s *Store) deleteChunked(ctx context.Context, table, where string, args ...any) (int64, error) {
	sel := rebind(`SELECT tableoid::regclass::text, ctid FROM ` + table + ` WHERE ` + where + ` LIMIT ` + strconv.Itoa(deleteChunk))
	var total int64
	for {
		found, n, err := s.deleteChunk(ctx, sel, where, args)
		total += n
		if err != nil || found < deleteChunk {
			return total, err
		}
	}
}

func (s *Store) deleteChunk(ctx context.Context, sel, where string, args []any) (found int, deleted int64, err error) {
	tx, err := s.w.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, sel, args...)
	if err != nil {
		return 0, 0, err
	}
	byPart := map[string][]pgtype.TID{}
	var order []string
	for rows.Next() {
		var part string
		var tid pgtype.TID
		if err := rows.Scan(&part, &tid); err != nil {
			rows.Close()
			return 0, 0, err
		}
		if _, ok := byPart[part]; !ok {
			order = append(order, part)
		}
		byPart[part] = append(byPart[part], tid)
		found++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	for _, part := range order {
		tag, err := tx.Exec(ctx, rebind(`DELETE FROM `+part+` WHERE ctid = ANY(?::tid[]) AND `+where),
			append([]any{byPart[part]}, args...)...)
		if err != nil {
			return 0, 0, err
		}
		deleted += tag.RowsAffected()
	}
	return found, deleted, tx.Commit(ctx)
}

// ---- sites ----

var ErrExists = errors.New("already exists")

func NewSiteID() string {
	var b [10]byte
	_, _ = rand.Read(b[:])
	return "site_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

func (s *Store) CreateSite(ctx context.Context, site Site) (Site, error) {
	if site.ID == "" {
		site.ID = NewSiteID()
	}
	site.CreatedAt = time.Now().UnixMilli()
	_, err := s.exec(ctx, `INSERT INTO sites (id, name, domain, created_at) VALUES (?,?,?,?)`,
		site.ID, site.Name, site.Domain, site.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Site{}, ErrExists
		}
		return Site{}, err
	}
	return site, nil
}

func (s *Store) GetSite(ctx context.Context, id string) (Site, error) {
	var site Site
	err := s.queryRow(ctx, `SELECT id, name, domain, created_at FROM sites WHERE id = ?`, id).
		Scan(&site.ID, &site.Name, &site.Domain, &site.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	return site, err
}

func (s *Store) ListSites(ctx context.Context) ([]Site, error) {
	rows, err := s.query(ctx, `SELECT id, name, domain, created_at FROM sites ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		var site Site
		if err := rows.Scan(&site.ID, &site.Name, &site.Domain, &site.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, site)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSite(ctx context.Context, site Site) (Site, error) {
	tag, err := s.exec(ctx, `UPDATE sites SET name = ?, domain = ? WHERE id = ?`, site.Name, site.Domain, site.ID)
	if err != nil {
		return Site{}, err
	}
	if tag.RowsAffected() == 0 {
		return Site{}, ErrNotFound
	}
	return s.GetSite(ctx, site.ID)
}

func (s *Store) DeleteSite(ctx context.Context, id string) error {
	var one int
	if err := s.w.QueryRow(ctx, `SELECT 1 FROM sites WHERE id = $1`, id).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	cids, err := s.allComponentIDs(ctx, id)
	if err != nil {
		return err
	}
	for _, cid := range cids {
		if err := s.clearComponentTags(ctx, cid); err != nil {
			return err
		}
	}
	if _, err := s.deleteChunked(ctx, "events", "site_id = ?", id); err != nil {
		return err
	}
	if _, err := s.deleteChunked(ctx, "heatmap_rollup", "site_id = ?", id); err != nil {
		return err
	}
	_, err = s.exec(ctx, `DELETE FROM sites WHERE id = ?`, id)
	return err
}

type SiteStat struct {
	Pageviews   int64
	LastEventAt int64
}

func (s *Store) SiteStats(ctx context.Context) (map[string]SiteStat, error) {
	rows, err := s.query(ctx, `SELECT site_id, COUNT(*), MAX(ts) FROM events WHERE type = 'pageview' GROUP BY site_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]SiteStat{}
	for rows.Next() {
		var id string
		var st SiteStat
		if err := rows.Scan(&id, &st.Pageviews, &st.LastEventAt); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}

// ---- screenshots ----

type Screenshot struct {
	ContentType string
	Data        []byte
	Width       int
	Height      int
	UpdatedAt   int64
}

func (s *Store) PutScreenshot(ctx context.Context, siteID, pageURL string, shot Screenshot) error {
	_, err := s.exec(ctx, `INSERT INTO screenshots (site_id, page_url, content_type, data, width, height, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (site_id, page_url) DO UPDATE SET content_type = excluded.content_type, data = excluded.data,
			width = excluded.width, height = excluded.height, updated_at = excluded.updated_at`,
		siteID, pageURL, shot.ContentType, shot.Data, shot.Width, shot.Height, time.Now().UnixMilli())
	return err
}

func (s *Store) GetScreenshot(ctx context.Context, siteID, pageURL string) (Screenshot, error) {
	var shot Screenshot
	err := s.queryRow(ctx, `SELECT content_type, data, width, height, updated_at FROM screenshots WHERE site_id = ? AND page_url = ?`,
		siteID, pageURL).Scan(&shot.ContentType, &shot.Data, &shot.Width, &shot.Height, &shot.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Screenshot{}, ErrNotFound
	}
	return shot, err
}

type ScreenshotMeta struct {
	Width     int   `json:"width"`
	Height    int   `json:"height"`
	UpdatedAt int64 `json:"updatedAt"`
}

func (s *Store) ScreenshotMeta(ctx context.Context, siteID, pageURL string) (*ScreenshotMeta, error) {
	var m ScreenshotMeta
	err := s.queryRow(ctx, `SELECT width, height, updated_at FROM screenshots WHERE site_id = ? AND page_url = ?`,
		siteID, pageURL).Scan(&m.Width, &m.Height, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) DeleteScreenshot(ctx context.Context, siteID, pageURL string) error {
	_, err := s.exec(ctx, `DELETE FROM screenshots WHERE site_id = ? AND page_url = ?`, siteID, pageURL)
	return err
}

func (s *Store) screenshotSet(ctx context.Context, siteID string) (map[string]bool, error) {
	rows, err := s.query(ctx, `SELECT page_url FROM screenshots WHERE site_id = ?`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out[u] = true
	}
	return out, rows.Err()
}
