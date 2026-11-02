package migrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// advisoryLockKey serialises PostgreSQL migration runners ("ervlogs" as an int64 tag).
const advisoryLockKey int64 = 0x6572766c6f6773

// defaultLockTimeout bounds how long a runner waits for another one to finish.
const defaultLockTimeout = 2 * time.Minute

const lockPollInterval = 250 * time.Millisecond

// Postgres migrates a PostgreSQL database; each migration runs in one transaction.
type Postgres struct {
	Pool *pgxpool.Pool
	// LockTimeout bounds the wait for the advisory lock; zero means defaultLockTimeout.
	LockTimeout time.Duration
}

func (p Postgres) Lock(ctx context.Context) (func(), error) {
	timeout := p.LockTimeout
	if timeout <= 0 {
		timeout = defaultLockTimeout
	}
	conn, err := p.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	// pg_try_advisory_lock instead of pg_advisory_lock: the wait stays bounded and cancellable and
	// no statement sits blocked on the server.
	err = waitForLock(ctx, timeout, lockPollInterval, func(ctx context.Context) (bool, error) {
		var got bool
		err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&got)
		return got, err
	})
	if err != nil {
		conn.Release()
		return nil, err
	}
	return func() {
		// Use a fresh context: the caller's may already be cancelled.
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(uctx, "SELECT pg_advisory_unlock($1)", advisoryLockKey); err != nil {
			// The lock is tied to the session: closing it is the only way not to return a pooled
			// connection that still holds the lock.
			_ = conn.Conn().Close(uctx)
		}
		conn.Release()
	}, nil
}

// waitForLock polls try until it succeeds, fails, ctx ends or timeout elapses.
func waitForLock(ctx context.Context, timeout, interval time.Duration, try func(context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		got, err := try(ctx)
		if err == nil && got {
			return nil
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return err
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("migrate: another runner still holds the migration lock after %s", timeout)
			}
			return ctx.Err()
		case <-tick.C:
		}
	}
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
// idempotent (IF NOT EXISTS / IF EXISTS) so a half-applied migration can be re-run, and so two
// concurrent `up` runners converge on the same schema.
type ClickHouse struct{ Conn driver.Conn }

// Lock is a no-op: ClickHouse has no advisory locks. Concurrent `up` runners are safe because scripts
// are idempotent and Verify tolerates the duplicate records they leave behind. Never run `down`
// concurrently with anything else.
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
