package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"strings"
	"time"
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
	Rage      bool
	Dead      bool

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

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
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

// InsertEvents writes all events in a single transaction and updates the
// hourly heatmap rollup for positioned click/navigation events.
func (s *Store) InsertEvents(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ins, err := tx.PrepareContext(ctx, `INSERT INTO events (
		site_id, type, page_url, session_id, pageview_id, ts, x, y,
		viewport_w, viewport_h, doc_w, doc_h,
		selector, el_tag, el_id, el_classes, el_text, el_label, rage, dead,
		target_url, nav_kind, referrer, redirect_count, scroll_depth, duration_ms, received_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	roll, err := tx.PrepareContext(ctx, `INSERT INTO heatmap_rollup (site_id, page_url, type, device, hour, gx, gy, n)
		VALUES (?,?,?,?,?,?,?,1)
		ON CONFLICT (site_id, page_url, type, device, hour, gx, gy) DO UPDATE SET n = n + 1`)
	if err != nil {
		return err
	}
	defer roll.Close()
	now := time.Now().UnixMilli()
	for i := range events {
		e := &events[i]
		var x, y, depth any
		if e.X != nil {
			x = *e.X
		}
		if e.Y != nil {
			y = *e.Y
		}
		if e.ScrollDepth != nil {
			depth = *e.ScrollDepth
		}
		if _, err := ins.ExecContext(ctx, e.SiteID, e.Type, e.PageURL, e.SessionID, e.PageviewID, e.TS, x, y,
			e.ViewportW, e.ViewportH, e.DocW, e.DocH,
			e.Selector, e.ElTag, e.ElID, e.ElClasses, e.ElText, e.ElLabel, b2i(e.Rage), b2i(e.Dead),
			e.TargetURL, e.NavKind, e.Referrer, e.RedirectCount, depth, e.DurationMs, now); err != nil {
			return err
		}
		if e.Type == TypeClick || e.Type == TypeNavigation {
			if gx, gy, ok := rollupCell(e); ok {
				if _, err := roll.ExecContext(ctx, e.SiteID, e.PageURL, e.Type, DeviceOf(e.ViewportW),
					e.TS/3_600_000, gx, gy); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

// PruneEvents deletes raw events older than cutoff (unix ms). Heatmap rollups
// are kept so long-range heatmaps survive raw-event retention.
func (s *Store) PruneEvents(ctx context.Context, cutoff int64) (int64, error) {
	res, err := s.w.ExecContext(ctx, `DELETE FROM events WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
	_, err := s.w.ExecContext(ctx, `INSERT INTO sites (id, name, domain, created_at) VALUES (?,?,?,?)`,
		site.ID, site.Name, site.Domain, site.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY KEY") {
			return Site{}, ErrExists
		}
		return Site{}, err
	}
	return site, nil
}

func (s *Store) GetSite(ctx context.Context, id string) (Site, error) {
	var site Site
	err := s.r.QueryRowContext(ctx, `SELECT id, name, domain, created_at FROM sites WHERE id = ?`, id).
		Scan(&site.ID, &site.Name, &site.Domain, &site.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	return site, err
}

func (s *Store) ListSites(ctx context.Context) ([]Site, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id, name, domain, created_at FROM sites ORDER BY created_at, id`)
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
	res, err := s.w.ExecContext(ctx, `UPDATE sites SET name = ?, domain = ? WHERE id = ?`, site.Name, site.Domain, site.ID)
	if err != nil {
		return Site{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Site{}, ErrNotFound
	}
	return s.GetSite(ctx, site.ID)
}

func (s *Store) DeleteSite(ctx context.Context, id string) error {
	res, err := s.w.ExecContext(ctx, `DELETE FROM sites WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type SiteStat struct {
	Pageviews   int64
	LastEventAt int64
}

func (s *Store) SiteStats(ctx context.Context) (map[string]SiteStat, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT site_id, COUNT(*), MAX(ts) FROM events WHERE type = 'pageview' GROUP BY site_id`)
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
	_, err := s.w.ExecContext(ctx, `INSERT INTO screenshots (site_id, page_url, content_type, data, width, height, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (site_id, page_url) DO UPDATE SET content_type = excluded.content_type, data = excluded.data,
			width = excluded.width, height = excluded.height, updated_at = excluded.updated_at`,
		siteID, pageURL, shot.ContentType, shot.Data, shot.Width, shot.Height, time.Now().UnixMilli())
	return err
}

func (s *Store) GetScreenshot(ctx context.Context, siteID, pageURL string) (Screenshot, error) {
	var shot Screenshot
	err := s.r.QueryRowContext(ctx, `SELECT content_type, data, width, height, updated_at FROM screenshots WHERE site_id = ? AND page_url = ?`,
		siteID, pageURL).Scan(&shot.ContentType, &shot.Data, &shot.Width, &shot.Height, &shot.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
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
	err := s.r.QueryRowContext(ctx, `SELECT width, height, updated_at FROM screenshots WHERE site_id = ? AND page_url = ?`,
		siteID, pageURL).Scan(&m.Width, &m.Height, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) DeleteScreenshot(ctx context.Context, siteID, pageURL string) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM screenshots WHERE site_id = ? AND page_url = ?`, siteID, pageURL)
	return err
}

func (s *Store) screenshotSet(ctx context.Context, siteID string) (map[string]bool, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT page_url FROM screenshots WHERE site_id = ?`, siteID)
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
