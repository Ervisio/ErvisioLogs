// Command ervisio-logs is the Ervisio Logs service: HTTP server and migration tool.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/db"
	"github.com/Ervisio/ErvisioLogs/backend/internal/migrate"
	"github.com/Ervisio/ErvisioLogs/backend/internal/server"
	"github.com/Ervisio/ErvisioLogs/backend/internal/version"
	"github.com/Ervisio/ErvisioLogs/backend/internal/webui"
	"github.com/Ervisio/ErvisioLogs/backend/migrations"
)

const usage = `usage: ervisio-logs <command>

commands:
  serve                 run the HTTP server
  migrate up            apply pending PostgreSQL and ClickHouse migrations
  migrate down [-n N]   revert the last N migrations on both stores (default 1)
  migrate status        show applied and pending migrations
  version               print version information
  config-check          validate the environment configuration and exit
  healthcheck           probe the local /healthz (for container HEALTHCHECK)`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return errors.New("missing command")
	}
	switch args[0] {
	case "version":
		fmt.Printf("ervisio-logs %s (%s)\n", version.Version, version.Commit)
		return nil
	case "serve":
		return withConfig(serve)
	case "healthcheck":
		return healthcheck()
	case "config-check":
		return withConfig(func(_ context.Context, c config.Config, _ *slog.Logger) error {
			fmt.Println("configuration OK")
			return nil
		})
	case "migrate":
		if len(args) < 2 {
			return errors.New("migrate: missing subcommand (up|down|status)")
		}
		sub, rest := args[1], args[2:]
		return withConfig(func(ctx context.Context, c config.Config, log *slog.Logger) error {
			return runMigrate(ctx, c, sub, rest)
		})
	default:
		fmt.Fprintln(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// healthcheck probes the local liveness endpoint; the distroless image has no curl.
func healthcheck() error {
	addr := os.Getenv("ERVISIO_LOGS_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}

func withConfig(fn func(context.Context, config.Config, *slog.Logger) error) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return fn(ctx, cfg, log)
}

func loadAll() (pg, ch []migrate.Migration, err error) {
	if pg, err = migrate.Load(migrations.FS, migrations.PostgresDir); err != nil {
		return
	}
	ch, err = migrate.Load(migrations.FS, migrations.ClickHouseDir)
	return
}

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	log.Info("starting", "version", version.Version, "commit", version.Commit, "config", cfg.Redacted())

	pgMigs, chMigs, err := loadAll()
	if err != nil {
		return err
	}
	pool, err := db.OpenPostgres(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()
	ch, err := db.OpenClickHouse(cfg.ClickHouse)
	if err != nil {
		return err
	}
	defer ch.Close()

	pgDrv, chDrv := migrate.Postgres{Pool: pool}, migrate.ClickHouse{Conn: ch}
	// Migration preflight: the server never migrates by itself; it reports not-ready until the
	// one-shot `migrate up` job has brought both schemas to the version this binary expects.
	migrated := func(d migrate.Driver, ms []migrate.Migration) func(context.Context) error {
		return func(ctx context.Context) error {
			st, err := migrate.Verify(ctx, d, ms)
			if err != nil {
				return err
			}
			if len(st.Pending) > 0 {
				return fmt.Errorf("%d migrations pending", len(st.Pending))
			}
			return nil
		}
	}
	health := server.NewHealth(log,
		server.Check{Name: "postgres", Fn: pool.Ping},
		server.Check{Name: "clickhouse", Fn: ch.Ping},
		server.Check{Name: "migrations_postgres", Fn: migrated(pgDrv, pgMigs)},
		server.Check{Name: "migrations_clickhouse", Fn: migrated(chDrv, chMigs)},
	)

	srv := server.NewHTTPServer(cfg.HTTPAddr, server.NewRouter(server.Options{
		Log: log, Health: health, UI: webui.Handler(), MaxBodyBytes: cfg.MaxBodyBytes,
	}))

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down", "timeout", cfg.ShutdownTimeout.String())
	sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runMigrate(ctx context.Context, cfg config.Config, sub string, args []string) error {
	fs := flag.NewFlagSet("migrate "+sub, flag.ContinueOnError)
	steps := fs.Int("n", 1, "number of migrations to revert (down only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pgMigs, chMigs, err := loadAll()
	if err != nil {
		return err
	}
	pool, err := db.OpenPostgres(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()
	ch, err := db.OpenClickHouse(cfg.ClickHouse)
	if err != nil {
		return err
	}
	defer ch.Close()

	targets := []struct {
		name string
		drv  migrate.Driver
		ms   []migrate.Migration
	}{
		{"postgres", migrate.Postgres{Pool: pool}, pgMigs},
		{"clickhouse", migrate.ClickHouse{Conn: ch}, chMigs},
	}
	for _, t := range targets {
		switch sub {
		case "up":
			n, err := migrate.Up(ctx, t.drv, t.ms)
			if err != nil {
				return fmt.Errorf("%s: %w", t.name, err)
			}
			fmt.Printf("%s: applied %d migration(s)\n", t.name, n)
		case "down":
			n, err := migrate.Down(ctx, t.drv, t.ms, *steps)
			if err != nil {
				return fmt.Errorf("%s: %w", t.name, err)
			}
			fmt.Printf("%s: reverted %d migration(s)\n", t.name, n)
		case "status":
			st, err := migrate.Check(ctx, t.drv, t.ms)
			if err != nil {
				return fmt.Errorf("%s: %w", t.name, err)
			}
			fmt.Printf("%s: %d applied, %d pending\n", t.name, len(st.Applied), len(st.Pending))
		default:
			return fmt.Errorf("unknown migrate subcommand %q", sub)
		}
	}
	return nil
}
