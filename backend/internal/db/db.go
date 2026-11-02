// Package db opens connections to the metadata (PostgreSQL) and hot (ClickHouse) stores.
package db

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
)

// OpenPostgres creates a lazily-connecting pool (connections are established on first use).
func OpenPostgres(ctx context.Context, c config.Postgres) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(c.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres dsn: %w", err)
	}
	pc.MaxConns = 10
	pc.MaxConnLifetime = time.Hour
	return pgxpool.NewWithConfig(ctx, pc)
}

// OpenClickHouse creates a native-protocol connection pool.
func OpenClickHouse(c config.ClickHouse) (driver.Conn, error) {
	opts := &clickhouse.Options{
		Addr: []string{c.Addr},
		Auth: clickhouse.Auth{Database: c.Database, Username: c.User, Password: c.Password},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout:  5 * time.Second,
		ReadTimeout:  60 * time.Second,
		MaxOpenConns: 10,
	}
	if c.TLS {
		opts.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return clickhouse.Open(opts)
}
