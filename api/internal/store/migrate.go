package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations are embedded SQL files named NNNN_description.sql. Version N
// upgrades a database from N-1 to N; each runs in its own transaction and is
// recorded in schema_migrations.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations(fsys fs.FS) ([]migration, error) {
	paths, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, p := range paths {
		base := strings.TrimSuffix(path.Base(p), ".sql")
		num, name, ok := strings.Cut(base, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("migration %s: name must be NNNN_description.sql", p)
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: name, sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("migrations must be numbered 1..n without gaps; found %04d at position %d", m.version, i+1)
		}
	}
	return out, nil
}

// migrateLock is the advisory lock key that serialises concurrent migrators
// (several API instances, the importer) on one database.
const migrateLock = 0x63616c6f72 // "calor"

// SchemaVersion is the schema version this binary migrates to.
func SchemaVersion() int {
	ms, _ := loadMigrations(migrationFiles)
	return len(ms)
}

func (s *Store) migrate(ctx context.Context) error {
	return migrate(ctx, s.w, migrationFiles)
}

func migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) error {
	ms, err := loadMigrations(fsys)
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLock); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrateLock)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    integer PRIMARY KEY,
		name       text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	var cur int
	if err := conn.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&cur); err != nil {
		return err
	}
	if cur > len(ms) {
		return fmt.Errorf("database schema is at version %d, newer than this binary (%d)", cur, len(ms))
	}
	for _, m := range ms[cur:] {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		// Multi-statement files need the simple query protocol.
		if _, err := tx.Conn().PgConn().Exec(ctx, m.sql).ReadAll(); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migrate to v%d (%s): %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.version, m.name); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
