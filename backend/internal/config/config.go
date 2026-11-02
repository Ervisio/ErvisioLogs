// Package config loads and validates the service configuration from the environment.
//
// Every variable is prefixed with ERVISIO_LOGS_. Secrets may be supplied through a
// companion <NAME>_FILE variable (Docker/Kubernetes secrets) instead of the value itself.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const prefix = "ERVISIO_LOGS_"

// Env is the deployment mode. Production enforces stricter validation.
type Env string

const (
	EnvDevelopment Env = "development"
	EnvProduction  Env = "production"
)

// Postgres holds PostgreSQL metadata store settings.
type Postgres struct {
	DSN string
}

// ClickHouse holds ClickHouse hot-store settings (native protocol).
type ClickHouse struct {
	Addr     string
	Database string
	User     string
	Password string
	TLS      bool
}

// Config is the validated service configuration.
type Config struct {
	Env             Env
	HTTPAddr        string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	// MaxBodyBytes is the global request body limit; ingestion routes will refine it later.
	MaxBodyBytes int64
	Postgres     Postgres
	ClickHouse   ClickHouse
}

// Load reads the configuration using os.Getenv / os.ReadFile and validates it.
func Load() (Config, error) { return load(os.Getenv, os.ReadFile) }

func load(getenv func(string) string, readFile func(string) ([]byte, error)) (Config, error) {
	var errs []error
	get := func(name, def string) string {
		if v := strings.TrimSpace(getenv(prefix + name)); v != "" {
			return v
		}
		return def
	}
	// secret resolves NAME or NAME_FILE; setting both is an error.
	secret := func(name string) string {
		v, f := getenv(prefix+name), strings.TrimSpace(getenv(prefix+name+"_FILE"))
		if v != "" && f != "" {
			errs = append(errs, fmt.Errorf("%s%s and %s%s_FILE are mutually exclusive", prefix, name, prefix, name))
			return ""
		}
		if f == "" {
			return v
		}
		b, err := readFile(f)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s%s_FILE: %w", prefix, name, err))
			return ""
		}
		return strings.TrimRight(string(b), "\r\n")
	}

	c := Config{
		Env:      Env(get("ENV", string(EnvDevelopment))),
		HTTPAddr: get("HTTP_ADDR", ":8080"),
		ClickHouse: ClickHouse{
			Addr:     get("CLICKHOUSE_ADDR", "clickhouse:9000"),
			Database: get("CLICKHOUSE_DATABASE", "ervisio_logs"),
			User:     get("CLICKHOUSE_USER", "ervisio_logs"),
			Password: secret("CLICKHOUSE_PASSWORD"),
		},
		Postgres: Postgres{DSN: secret("POSTGRES_DSN")},
	}

	if err := c.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("%sLOG_LEVEL: %w", prefix, err))
	}
	var err error
	if c.ShutdownTimeout, err = time.ParseDuration(get("SHUTDOWN_TIMEOUT", "20s")); err != nil || c.ShutdownTimeout <= 0 {
		errs = append(errs, fmt.Errorf("%sSHUTDOWN_TIMEOUT must be a positive duration", prefix))
	}
	if c.MaxBodyBytes, err = strconv.ParseInt(get("MAX_BODY_BYTES", "1048576"), 10, 64); err != nil || c.MaxBodyBytes <= 0 {
		errs = append(errs, fmt.Errorf("%sMAX_BODY_BYTES must be a positive integer", prefix))
	}
	if v := get("CLICKHOUSE_TLS", "false"); v != "" {
		if c.ClickHouse.TLS, err = strconv.ParseBool(v); err != nil {
			errs = append(errs, fmt.Errorf("%sCLICKHOUSE_TLS must be a boolean", prefix))
		}
	}

	errs = append(errs, c.validate()...)
	return c, errors.Join(errs...)
}

func (c Config) validate() []error {
	var errs []error
	if c.Env != EnvDevelopment && c.Env != EnvProduction {
		errs = append(errs, fmt.Errorf("%sENV must be %q or %q", prefix, EnvDevelopment, EnvProduction))
	}
	if c.Postgres.DSN == "" {
		errs = append(errs, fmt.Errorf("%sPOSTGRES_DSN (or _FILE) is required", prefix))
	} else if u, err := url.Parse(c.Postgres.DSN); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		errs = append(errs, fmt.Errorf("%sPOSTGRES_DSN must be a postgres:// URL", prefix))
	}
	if c.ClickHouse.Password == "" {
		errs = append(errs, fmt.Errorf("%sCLICKHOUSE_PASSWORD (or _FILE) is required: no default credentials", prefix))
	}
	if !strings.Contains(c.ClickHouse.Addr, ":") {
		errs = append(errs, fmt.Errorf("%sCLICKHOUSE_ADDR must be host:port", prefix))
	}
	if c.Env == EnvProduction {
		if u, err := url.Parse(c.Postgres.DSN); err == nil && u.Query().Get("sslmode") == "disable" {
			errs = append(errs, fmt.Errorf("%sPOSTGRES_DSN: sslmode=disable is refused in production", prefix))
		}
		if looksLikePlaceholder(c.ClickHouse.Password) || looksLikePlaceholder(c.Postgres.DSN) {
			errs = append(errs, errors.New("placeholder credentials (change_me) are refused in production"))
		}
	}
	return errs
}

func looksLikePlaceholder(s string) bool {
	return strings.Contains(strings.ToLower(s), "change_me")
}

// Redacted returns a log-safe summary of the configuration (no secrets).
func (c Config) Redacted() map[string]any {
	host := ""
	if u, err := url.Parse(c.Postgres.DSN); err == nil {
		host = u.Host
	}
	return map[string]any{
		"env": c.Env, "http_addr": c.HTTPAddr, "log_level": c.LogLevel.String(),
		"postgres_host": host, "clickhouse_addr": c.ClickHouse.Addr,
		"clickhouse_database": c.ClickHouse.Database, "clickhouse_tls": c.ClickHouse.TLS,
	}
}
