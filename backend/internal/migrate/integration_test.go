//go:build integration

package migrate_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/db"
	"github.com/Ervisio/ErvisioLogs/backend/internal/migrate"
	"github.com/Ervisio/ErvisioLogs/backend/migrations"
)

func isLocal(hostPort string) bool {
	return strings.HasPrefix(hostPort, "127.0.0.1:") || strings.HasPrefix(hostPort, "localhost:")
}

// Runs against real PostgreSQL and ClickHouse (see `make test-integration`).
func TestMigrationsAgainstRealStores(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("integration configuration missing or invalid (use `make test-integration`): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// This test destroys both schemas: never let it near anything but a throwaway local stack.
	dsnHost := cfg.Postgres.DSN[strings.LastIndex(cfg.Postgres.DSN, "@")+1:]
	if !isLocal(dsnHost) || !isLocal(cfg.ClickHouse.Addr) {
		t.Fatal("refusing to reset non-local databases")
	}

	pool, err := db.OpenPostgres(ctx, cfg.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ch, err := db.OpenClickHouse(cfg.ClickHouse)
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()

	// A wipe rather than Down: the database may hold an older copy of a migration (or may just have been
	// reset by internal/api's integration tests), and Down would stop at the checksum guard.
	resetPostgres := func() error {
		_, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`)
		return err
	}
	resetClickHouse := func() error {
		rows, err := ch.Query(ctx, "SELECT name FROM system.tables WHERE database = currentDatabase()")
		if err != nil {
			return err
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			names = append(names, n)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, n := range names {
			if err := ch.Exec(ctx, "DROP TABLE IF EXISTS `"+n+"` SYNC"); err != nil {
				return err
			}
		}
		return nil
	}

	targets := map[string]struct {
		drv   migrate.Driver
		dir   string
		reset func() error
	}{
		"postgres":   {migrate.Postgres{Pool: pool}, migrations.PostgresDir, resetPostgres},
		"clickhouse": {migrate.ClickHouse{Conn: ch}, migrations.ClickHouseDir, resetClickHouse},
	}
	for name, tg := range targets {
		t.Run(name, func(t *testing.T) {
			ms, err := migrate.Load(migrations.FS, tg.dir)
			if err != nil {
				t.Fatal(err)
			}
			// Start from a clean slate, whatever state the database was left in.
			if err := tg.reset(); err != nil {
				t.Fatalf("reset: %v", err)
			}
			if n, err := migrate.Up(ctx, tg.drv, ms); err != nil || n != len(ms) {
				t.Fatalf("up: %d %v", n, err)
			}
			if n, err := migrate.Up(ctx, tg.drv, ms); err != nil || n != 0 {
				t.Fatalf("second up must be a no-op: %d %v", n, err)
			}
			st, err := migrate.Verify(ctx, tg.drv, ms)
			if err != nil || len(st.Pending) != 0 {
				t.Fatalf("verify: %+v %v", st, err)
			}
			// Up -> down -> up proves the down scripts are real.
			if n, err := migrate.Down(ctx, tg.drv, ms, len(ms)); err != nil || n != len(ms) {
				t.Fatalf("down: %d %v", n, err)
			}
			if n, err := migrate.Up(ctx, tg.drv, ms); err != nil || n != len(ms) {
				t.Fatalf("re-up: %d %v", n, err)
			}
		})
	}
}

// Two concurrent ClickHouse runners may both record a migration; the service must still become ready
// and a later down must clear every copy.
func TestClickHouseDuplicateRecordsAreTolerated(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("integration configuration missing or invalid (use `make test-integration`): %v", err)
	}
	if !isLocal(cfg.ClickHouse.Addr) {
		t.Fatal("refusing to touch a non-local ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ch, err := db.OpenClickHouse(cfg.ClickHouse)
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	ms, err := migrate.Load(migrations.FS, migrations.ClickHouseDir)
	if err != nil {
		t.Fatal(err)
	}
	drv := migrate.ClickHouse{Conn: ch}
	if _, err := migrate.Up(ctx, drv, ms); err != nil {
		t.Fatal(err)
	}
	if err := ch.Exec(ctx, "INSERT INTO schema_migrations(version,name,checksum) VALUES(?,?,?)", uint32(ms[0].Version), ms[0].Name, ms[0].Checksum()); err != nil {
		t.Fatal(err)
	}
	st, err := migrate.Verify(ctx, drv, ms)
	if err != nil || len(st.Applied) != len(ms) || len(st.Pending) != 0 {
		t.Fatalf("verify with duplicate: %+v %v", st, err)
	}
	if n, err := migrate.Down(ctx, drv, ms, len(ms)); err != nil || n != len(ms) {
		t.Fatalf("down: %d %v", n, err)
	}
	if st, err := migrate.Verify(ctx, drv, ms); err != nil || len(st.Applied) != 0 {
		t.Fatalf("down must clear duplicates: %+v %v", st, err)
	}
	if _, err := migrate.Up(ctx, drv, ms); err != nil {
		t.Fatal(err)
	}
}

// A second runner must give up instead of waiting forever for the advisory lock.
func TestPostgresLockTimesOut(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("integration configuration missing or invalid (use `make test-integration`): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := db.OpenPostgres(ctx, cfg.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	release, err := migrate.Postgres{Pool: pool}.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := (migrate.Postgres{Pool: pool, LockTimeout: 500 * time.Millisecond}).Lock(ctx); err == nil {
		t.Fatal("second lock must time out")
	}
}
