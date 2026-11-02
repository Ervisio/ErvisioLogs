//go:build integration

package migrate_test

import (
	"context"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/db"
	"github.com/Ervisio/ErvisioLogs/backend/internal/migrate"
	"github.com/Ervisio/ErvisioLogs/backend/migrations"
)

// Runs against real PostgreSQL and ClickHouse (see `make test-integration`).
func TestMigrationsAgainstRealStores(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("no integration configuration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

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

	targets := map[string]struct {
		drv migrate.Driver
		dir string
	}{
		"postgres":   {migrate.Postgres{Pool: pool}, migrations.PostgresDir},
		"clickhouse": {migrate.ClickHouse{Conn: ch}, migrations.ClickHouseDir},
	}
	for name, tg := range targets {
		t.Run(name, func(t *testing.T) {
			ms, err := migrate.Load(migrations.FS, tg.dir)
			if err != nil {
				t.Fatal(err)
			}
			// Start from a clean slate, whatever state the database was left in.
			if _, err := migrate.Down(ctx, tg.drv, ms, len(ms)); err != nil {
				t.Fatalf("down: %v", err)
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
