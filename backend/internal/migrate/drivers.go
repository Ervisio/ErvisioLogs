package migrate

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// advisoryLockKey serialises PostgreSQL migration runners ("ervlogs" as an int64 tag).
const advisoryLockKey int64 = 0x6572766c6f6773

// Postgres migrates a PostgreSQL database; each migration runs in one transaction.
type Postgres struct{ Pool *pgxpool.Pool }

func (p Postgres) Lock(ctx context.Context) (func(), error) {
	conn, err := p.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		conn.Release()
		return nil, err
	}
	return func() {
		// Use a fresh context: the caller's may already be cancelled.
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey)
		conn.Release()
	}, nil
}

func (p Postgres) Init(ctx context.Context) error {
	_, err := p.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version int PRIMARY KEY, name text NOT NULL, checksum text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now())`)
	return err
}

func (p Postgres) Applied(ctx context.Context) ([]Record, error) {
	rows, err := p.Pool.Query(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.Version, &r.Name, &r.Checksum); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p Postgres) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p Postgres) Apply(ctx context.Context, m Migration) error {
	return p.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, m.Up); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO schema_migrations(version,name,checksum) VALUES($1,$2,$3)", m.Version, m.Name, m.Checksum())
		return err
	})
}

func (p Postgres) Revert(ctx context.Context, m Migration) error {
	return p.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, m.Down); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version=$1", m.Version)
		return err
	})
}

// ClickHouse migrates a ClickHouse database. DDL is not transactional: statements must be
// idempotent (IF NOT EXISTS / IF EXISTS) so a half-applied migration can be re-run.
type ClickHouse struct{ Conn driver.Conn }

// Lock is a no-op: ClickHouse has no advisory locks. Run migrations from a single one-shot job.
func (ClickHouse) Lock(context.Context) (func(), error) { return func() {}, nil }

func (c ClickHouse) Init(ctx context.Context) error {
	return c.Conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version UInt32, name String, checksum String, applied_at DateTime DEFAULT now())
		ENGINE = MergeTree ORDER BY version`)
}

func (c ClickHouse) Applied(ctx context.Context) ([]Record, error) {
	rows, err := c.Conn.Query(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var v uint32
		var r Record
		if err := rows.Scan(&v, &r.Name, &r.Checksum); err != nil {
			return nil, err
		}
		r.Version = int(v)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (c ClickHouse) exec(ctx context.Context, script string) error {
	for _, s := range SplitStatements(script) {
		if err := c.Conn.Exec(ctx, s); err != nil {
			return fmt.Errorf("%w (statement: %.80s)", err, s)
		}
	}
	return nil
}

func (c ClickHouse) Apply(ctx context.Context, m Migration) error {
	if err := c.exec(ctx, m.Up); err != nil {
		return err
	}
	return c.Conn.Exec(ctx, "INSERT INTO schema_migrations(version,name,checksum) VALUES(?,?,?)", uint32(m.Version), m.Name, m.Checksum())
}

func (c ClickHouse) Revert(ctx context.Context, m Migration) error {
	if err := c.exec(ctx, m.Down); err != nil {
		return err
	}
	return c.Conn.Exec(ctx, "ALTER TABLE schema_migrations DELETE WHERE version = ? SETTINGS mutations_sync = 2", uint32(m.Version))
}
