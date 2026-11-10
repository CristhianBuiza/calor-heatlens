package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// events and event_components are range-partitioned by UTC month on ts. Both
// get a partition for the same months, named <table>_yYYYYmMM. There is no
// default partition: the writer creates a month's partitions before the first
// insert into it, and the hourly maintenance creates next month's ahead of
// time so ingest normally never waits on DDL.
var partitionedTables = []string{"event_components", "events"}

// month counts UTC months since year 0: year*12 + (month-1).
type month int

func monthOf(tsMs int64) month {
	t := time.UnixMilli(tsMs).UTC()
	return month(t.Year()*12 + int(t.Month()) - 1)
}

func (m month) start() int64 {
	return time.Date(int(m)/12, time.Month(int(m)%12+1), 1, 0, 0, 0, 0, time.UTC).UnixMilli()
}

func (m month) suffix() string { return fmt.Sprintf("_y%04dm%02d", int(m)/12, int(m)%12+1) }

var partitionName = regexp.MustCompile(`^(events|event_components)_y(\d{4,})m(\d{2})$`)

func parsePartition(name string) (table string, m month, ok bool) {
	p := partitionName.FindStringSubmatch(name)
	if p == nil {
		return "", 0, false
	}
	y, _ := strconv.Atoi(p[2])
	mo, _ := strconv.Atoi(p[3])
	if mo < 1 || mo > 12 {
		return "", 0, false
	}
	return p[1], month(y*12 + mo - 1), true
}

// partitions remembers the months whose partitions exist for both tables.
type partitions struct {
	mu   sync.Mutex
	have map[month]bool
}

type partitionInfo struct {
	name, table string
	m           month
	attached    bool // a live partition of its table
	pending     bool // DETACH CONCURRENTLY was interrupted
}

// listPartitions returns the monthly tables in the events table's schema,
// including ones a crashed retention run left detached but not dropped.
func listPartitions(ctx context.Context, pool *pgxpool.Pool) ([]partitionInfo, error) {
	rows, err := pool.Query(ctx, `SELECT c.relname::text, c.relispartition, COALESCE(i.inhdetachpending, false)
		FROM pg_class c LEFT JOIN pg_inherits i ON i.inhrelid = c.oid
		WHERE c.relnamespace = (SELECT relnamespace FROM pg_class WHERE oid = 'events'::regclass)
			AND c.relkind = 'r' AND c.relname ~ '^(events|event_components)_y[0-9]+m[0-9]{2}$'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []partitionInfo
	for rows.Next() {
		var p partitionInfo
		if err := rows.Scan(&p.name, &p.attached, &p.pending); err != nil {
			return nil, err
		}
		var ok bool
		if p.table, p.m, ok = parsePartition(p.name); ok {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (p *partitions) load(ctx context.Context, pool *pgxpool.Pool) error {
	list, err := listPartitions(ctx, pool)
	if err != nil {
		return err
	}
	n := map[month]int{}
	for _, pi := range list {
		if pi.attached && !pi.pending {
			n[pi.m]++
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.have = map[month]bool{}
	for m, c := range n {
		p.have[m] = c == len(partitionedTables)
	}
	return nil
}

func (p *partitions) missing(tss []int64) []month {
	p.mu.Lock()
	defer p.mu.Unlock()
	var need []month
	var last month = -1
	for _, ts := range tss {
		m := monthOf(ts)
		if m == last || p.have[m] {
			continue
		}
		last = m
		dup := false
		for _, n := range need {
			dup = dup || n == m
		}
		if !dup {
			need = append(need, m)
		}
	}
	return need
}

func (p *partitions) set(m month, ok bool) {
	p.mu.Lock()
	if ok {
		p.have[m] = true
	} else {
		delete(p.have, m)
	}
	p.mu.Unlock()
}

// EnsurePartitions creates the monthly partitions that rows with these
// timestamps (unix ms) need.
func (s *Store) EnsurePartitions(ctx context.Context, tss ...int64) error {
	for _, m := range s.parts.missing(tss) {
		for _, table := range partitionedTables {
			q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%d) TO (%d)`,
				pgx.Identifier{table + m.suffix()}.Sanitize(), table, m.start(), (m + 1).start())
			if _, err := s.w.Exec(ctx, q); err != nil && !isDuplicateObject(err) {
				return fmt.Errorf("create partition %s%s: %w", table, m.suffix(), err)
			}
		}
		s.parts.set(m, true)
	}
	return nil
}

// EnsureUpcomingPartitions creates the partitions for now's month and the next.
func (s *Store) EnsureUpcomingPartitions(ctx context.Context, now time.Time) error {
	cur := monthOf(now.UnixMilli())
	return s.EnsurePartitions(ctx, cur.start(), (cur + 1).start())
}

// isDuplicateObject reports a lost race with another process creating the
// same partition.
func isDuplicateObject(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && (pe.Code == "42P07" || pe.Code == "23505")
}

// dropPartitionsBefore removes the months that end at or before cutoff and
// returns how many events they held. Partitions are detached CONCURRENTLY
// (no lock that blocks ingest or reads) and then dropped.
func (s *Store) dropPartitionsBefore(ctx context.Context, cutoff int64) (int64, error) {
	list, err := listPartitions(ctx, s.w)
	if err != nil {
		return 0, err
	}
	var dropped int64
	for _, p := range list {
		if (p.m + 1).start() > cutoff {
			continue
		}
		s.parts.set(p.m, false)
		ident := pgx.Identifier{p.name}.Sanitize()
		if p.table == "events" {
			var n int64
			if err := s.w.QueryRow(ctx, `SELECT count(*) FROM `+ident).Scan(&n); err != nil {
				return dropped, err
			}
			dropped += n
		}
		if p.attached {
			mode := "CONCURRENTLY"
			if p.pending {
				mode = "FINALIZE"
			}
			if _, err := s.w.Exec(ctx, `ALTER TABLE `+p.table+` DETACH PARTITION `+ident+` `+mode); err != nil {
				return dropped, fmt.Errorf("detach %s: %w", p.name, err)
			}
		}
		if _, err := s.w.Exec(ctx, `DROP TABLE IF EXISTS `+ident); err != nil {
			return dropped, fmt.Errorf("drop %s: %w", p.name, err)
		}
	}
	return dropped, nil
}
