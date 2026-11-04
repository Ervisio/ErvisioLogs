// Package config loads and validates the service configuration from the environment.
//
// Every variable is prefixed with ERVISIO_LOGS_. Secrets may be supplied through a
// companion <NAME>_FILE variable (Docker/Kubernetes secrets) instead of the value itself.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
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

// ClickHouse holds ClickHouse hot-store settings. Queries use the native protocol; the archiver
// exports Parquet over HTTPURL, since the native protocol has no output formats.
type ClickHouse struct {
	Addr     string
	HTTPURL  string
	Database string
	User     string
	Password string
	TLS      bool
}

// Archive holds the local side of the archive tier (ADR 0008). Provider settings live in the
// database; their secrets are files in SecretsDir.
type Archive struct {
	// CacheDir holds staged exports and downloaded segments. ClickHouse must see the same
	// directory, read-only, at user_files/<ClickHousePath>.
	CacheDir       string
	ClickHousePath string
	SecretsDir     string
	// Interval between archiver passes. Zero disables the archiver in this process.
	Interval time.Duration
	// Grace is how long after a UTC day ends before it is archived.
	Grace time.Duration
	// LateInterval is the shortest time between two generations of one stream-day.
	LateInterval time.Duration
}

// OAuthProvider holds one external identity provider's credentials. Empty ClientID means disabled.
type OAuthProvider struct {
	ClientID     string
	ClientSecret string
	// Issuer is the OIDC issuer URL (Cognito only; Google's is fixed, Discord is plain OAuth2).
	Issuer string
}

func (p OAuthProvider) Enabled() bool { return p.ClientID != "" }

// Auth holds authentication settings.
type Auth struct {
	// PublicURL is the externally visible base URL (OAuth redirect URIs, cookie Secure flag).
	PublicURL string
	// SecretKey keys the HMAC used to store API tokens. At least 32 characters.
	SecretKey string
	// OpenRegistration lets anyone create an account. Off by default: after the first admin
	// exists, accounts are created by an instance admin.
	OpenRegistration bool
	// SetupCode authorises creating the first (instance admin) account. Generated and logged
	// at start-up when unset and no user exists.
	SetupCode  string
	SessionTTL time.Duration
	// TrustProxy takes the client IP from the last X-Forwarded-For entry. Enable only behind a
	// reverse proxy that sets that header itself.
	TrustProxy bool
	Google     OAuthProvider
	Cognito    OAuthProvider
	Discord    OAuthProvider
}

// Mail holds the SMTP server for outgoing email (invitations, password reset, verification). The
// values here are the defaults of the email.* runtime settings; Password is the only secret and is
// never a setting. Empty Host means email is off.
type Mail struct {
	Host     string
	Port     int
	Security string // starttls, tls or none
	Username string
	Password string
	From     string
}

// Config is the validated service configuration.
type Config struct {
	Env      Env
	HTTPAddr string
	// AdminSocket is the path of the local admin unix socket (ADR 0019); empty disables it.
	AdminSocket     string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	// MaxBodyBytes is the global request body limit; ingestion routes will refine it later.
	MaxBodyBytes int64
	Postgres     Postgres
	ClickHouse   ClickHouse
	Auth         Auth
	Ingest       Ingest
	Archive      Archive
	Alerts       Alerts
	Mail         Mail
	// ExportMaxRows caps the rows of one export (ADR 0010).
	ExportMaxRows int
	// FromEnv holds the variables that were set, by full name. A runtime setting backed by one of
	// them cannot be changed from the API (ADR 0018).
	FromEnv map[string]bool
}

// Alerts configures the alert evaluator and webhook delivery (ADR 0010).
type Alerts struct {
	// Interval between evaluator passes; 0 leaves evaluation to another process.
	Interval time.Duration
	// WebhookAllowPrivate lets webhooks reach loopback, private and link-local addresses and plain
	// http. Off by default: a rule author could otherwise probe the server's own network.
	WebhookAllowPrivate bool
	// HistoryRetention bounds how long alert state changes are kept.
	HistoryRetention time.Duration
}

// Ingest holds the durable spool (WAL) and ingest limits.
type Ingest struct {
	// SpoolDir holds the WAL. Give it its own volume: its budget is independent of the databases.
	SpoolDir string
	// SpoolMaxBytes is the WAL disk budget. When full, ingest answers 503.
	SpoolMaxBytes int64
	// MaxFutureSkew and MaxPastAge bound event timestamps (provisional until D007).
	MaxFutureSkew time.Duration
	MaxPastAge    time.Duration
	// MetricsToken, when set, protects /metrics with a bearer token. Unset: /metrics is served only
	// in development.
	MetricsToken string
}

// Load reads the configuration using os.Getenv / os.ReadFile and validates it.
func Load() (Config, error) { return load(os.Getenv, os.ReadFile) }

func load(getenv func(string) string, readFile func(string) ([]byte, error)) (Config, error) {
	var errs []error
	fromEnv := map[string]bool{}
	get := func(name, def string) string {
		if v := strings.TrimSpace(getenv(prefix + name)); v != "" {
			fromEnv[prefix+name] = true
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
		FromEnv:     fromEnv,
		Env:         Env(get("ENV", string(EnvDevelopment))),
		HTTPAddr:    get("HTTP_ADDR", ":8080"),
		AdminSocket: get("ADMIN_SOCKET", ""),
		ClickHouse: ClickHouse{
			Addr:     get("CLICKHOUSE_ADDR", "clickhouse:9000"),
			Database: get("CLICKHOUSE_DATABASE", "ervisio_logs"),
			User:     get("CLICKHOUSE_USER", "ervisio_logs"),
			Password: secret("CLICKHOUSE_PASSWORD"),
		},
		Postgres: Postgres{DSN: secret("POSTGRES_DSN")},
		Auth: Auth{
			PublicURL: strings.TrimRight(get("PUBLIC_URL", "http://localhost:8080"), "/"),
			SecretKey: secret("SECRET_KEY"),
			SetupCode: secret("SETUP_CODE"),
			Google:    OAuthProvider{ClientID: get("GOOGLE_CLIENT_ID", ""), ClientSecret: secret("GOOGLE_CLIENT_SECRET")},
			Cognito: OAuthProvider{
				ClientID: get("COGNITO_CLIENT_ID", ""), ClientSecret: secret("COGNITO_CLIENT_SECRET"),
				Issuer: strings.TrimRight(get("COGNITO_ISSUER", ""), "/"),
			},
			Discord: OAuthProvider{ClientID: get("DISCORD_CLIENT_ID", ""), ClientSecret: secret("DISCORD_CLIENT_SECRET")},
		},
	}
	c.Ingest.SpoolDir = get("SPOOL_DIR", "/var/lib/ervisio-logs/spool")
	c.ClickHouse.HTTPURL = strings.TrimRight(get("CLICKHOUSE_HTTP_URL", ""), "/")
	c.Archive.CacheDir = get("ARCHIVE_CACHE_DIR", "/var/lib/ervisio-logs/cache")
	c.Archive.ClickHousePath = strings.Trim(get("ARCHIVE_CLICKHOUSE_PATH", "ervisio-archive"), "/")
	c.Archive.SecretsDir = get("ARCHIVE_SECRETS_DIR", "")
	c.Ingest.MetricsToken = secret("METRICS_TOKEN")
	c.Mail = Mail{Host: get("SMTP_HOST", ""), Security: get("SMTP_SECURITY", "starttls"),
		Username: get("SMTP_USERNAME", ""), Password: secret("SMTP_PASSWORD"), From: get("SMTP_FROM", "")}
	var perr error
	if c.Ingest.SpoolMaxBytes, perr = strconv.ParseInt(get("SPOOL_MAX_BYTES", "1073741824"), 10, 64); perr != nil || c.Ingest.SpoolMaxBytes < 1<<20 {
		errs = append(errs, fmt.Errorf("%sSPOOL_MAX_BYTES must be an integer of at least 1048576", prefix))
	}
	if c.Ingest.MaxFutureSkew, perr = time.ParseDuration(get("INGEST_MAX_FUTURE_SKEW", "5m")); perr != nil || c.Ingest.MaxFutureSkew < 0 {
		errs = append(errs, fmt.Errorf("%sINGEST_MAX_FUTURE_SKEW must be a non-negative duration", prefix))
	}
	if c.Ingest.MaxPastAge, perr = time.ParseDuration(get("INGEST_MAX_PAST_AGE", "168h")); perr != nil || c.Ingest.MaxPastAge <= 0 {
		errs = append(errs, fmt.Errorf("%sINGEST_MAX_PAST_AGE must be a positive duration", prefix))
	}
	for _, d := range []struct {
		name, def string
		dst       *time.Duration
		min       time.Duration
	}{
		{"ARCHIVE_INTERVAL", "5m", &c.Archive.Interval, 0},
		{"ARCHIVE_GRACE", "1h", &c.Archive.Grace, 0},
		{"ARCHIVE_LATE_INTERVAL", "6h", &c.Archive.LateInterval, 0},
		{"ALERTS_INTERVAL", "30s", &c.Alerts.Interval, 0},
		{"ALERTS_HISTORY_RETENTION", "720h", &c.Alerts.HistoryRetention, time.Hour},
	} {
		if *d.dst, perr = time.ParseDuration(get(d.name, d.def)); perr != nil || *d.dst < d.min {
			errs = append(errs, fmt.Errorf("%s%s must be a non-negative duration", prefix, d.name))
		}
	}
	if c.Alerts.WebhookAllowPrivate, perr = strconv.ParseBool(get("WEBHOOK_ALLOW_PRIVATE", "false")); perr != nil {
		errs = append(errs, fmt.Errorf("%sWEBHOOK_ALLOW_PRIVATE must be a boolean", prefix))
	}
	if c.ExportMaxRows, perr = strconv.Atoi(get("EXPORT_MAX_ROWS", "1000000")); perr != nil || c.ExportMaxRows < 1 || c.ExportMaxRows > 50000000 {
		errs = append(errs, fmt.Errorf("%sEXPORT_MAX_ROWS must be an integer between 1 and 50000000", prefix))
	}
	if c.Mail.Port, perr = strconv.Atoi(get("SMTP_PORT", "587")); perr != nil || c.Mail.Port < 1 || c.Mail.Port > 65535 {
		errs = append(errs, fmt.Errorf("%sSMTP_PORT must be a port number", prefix))
	}
	if c.Mail.Security != "starttls" && c.Mail.Security != "tls" && c.Mail.Security != "none" {
		errs = append(errs, fmt.Errorf("%sSMTP_SECURITY must be starttls, tls or none", prefix))
	}
	if c.Auth.OpenRegistration, perr = strconv.ParseBool(get("OPEN_REGISTRATION", "false")); perr != nil {
		errs = append(errs, fmt.Errorf("%sOPEN_REGISTRATION must be a boolean", prefix))
	}
	if c.Auth.TrustProxy, perr = strconv.ParseBool(get("TRUST_PROXY", "false")); perr != nil {
		errs = append(errs, fmt.Errorf("%sTRUST_PROXY must be a boolean", prefix))
	}
	if c.Auth.SessionTTL, perr = time.ParseDuration(get("SESSION_TTL", "336h")); perr != nil || c.Auth.SessionTTL <= 0 {
		errs = append(errs, fmt.Errorf("%sSESSION_TTL must be a positive duration", prefix))
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

	if c.ClickHouse.HTTPURL == "" {
		// Same host as the native address, on the default HTTP(S) port.
		host, _, _ := strings.Cut(c.ClickHouse.Addr, ":")
		if c.ClickHouse.TLS {
			c.ClickHouse.HTTPURL = "https://" + host + ":8443"
		} else {
			c.ClickHouse.HTTPURL = "http://" + host + ":8123"
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
	if u, err := url.Parse(c.ClickHouse.HTTPURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		errs = append(errs, fmt.Errorf("%sCLICKHOUSE_HTTP_URL must be an http(s)://host:port URL without credentials", prefix))
	}
	if c.Archive.ClickHousePath == "" || strings.Contains(c.Archive.ClickHousePath, "..") || strings.ContainsAny(c.Archive.ClickHousePath, "'\\") {
		errs = append(errs, fmt.Errorf("%sARCHIVE_CLICKHOUSE_PATH must be a plain relative path", prefix))
	}
	if c.Mail.From != "" {
		if _, err := mail.ParseAddress(c.Mail.From); err != nil {
			errs = append(errs, fmt.Errorf("%sSMTP_FROM must be an address such as Ervisio Logs <logs@example.com>", prefix))
		}
	}
	errs = append(errs, c.validateAuth()...)
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

func (c Config) validateAuth() []error {
	var errs []error
	a := c.Auth
	if len(a.SecretKey) < 32 {
		errs = append(errs, fmt.Errorf("%sSECRET_KEY (or _FILE) is required and must be at least 32 characters", prefix))
	}
	u, err := url.Parse(a.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("%sPUBLIC_URL must be an http(s) URL", prefix))
	} else if c.Env == EnvProduction && u.Scheme != "https" {
		errs = append(errs, fmt.Errorf("%sPUBLIC_URL must be https in production", prefix))
	}
	for name, p := range map[string]OAuthProvider{"GOOGLE": a.Google, "COGNITO": a.Cognito, "DISCORD": a.Discord} {
		if p.Enabled() && p.ClientSecret == "" {
			errs = append(errs, fmt.Errorf("%s%s_CLIENT_SECRET is required when %s_CLIENT_ID is set", prefix, name, name))
		}
	}
	if a.Cognito.Enabled() {
		if iu, err := url.Parse(a.Cognito.Issuer); err != nil || iu.Scheme != "https" || iu.Host == "" {
			errs = append(errs, fmt.Errorf("%sCOGNITO_ISSUER must be the https pool issuer URL", prefix))
		}
	}
	if c.Env == EnvProduction && looksLikePlaceholder(a.SecretKey) {
		errs = append(errs, errors.New("placeholder SECRET_KEY refused in production"))
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
		"env": c.Env, "http_addr": c.HTTPAddr, "admin_socket": c.AdminSocket, "log_level": c.LogLevel.String(),
		"public_url": c.Auth.PublicURL, "open_registration": c.Auth.OpenRegistration,
		"oauth_google": c.Auth.Google.Enabled(), "oauth_cognito": c.Auth.Cognito.Enabled(), "oauth_discord": c.Auth.Discord.Enabled(),
		"postgres_host": host, "clickhouse_addr": c.ClickHouse.Addr,
		"clickhouse_database": c.ClickHouse.Database, "clickhouse_tls": c.ClickHouse.TLS,
		"spool_dir": c.Ingest.SpoolDir, "spool_max_bytes": c.Ingest.SpoolMaxBytes,
		"metrics_protected": c.Ingest.MetricsToken != "",
		"alerts_interval":   c.Alerts.Interval.String(), "webhook_allow_private": c.Alerts.WebhookAllowPrivate,
		"export_max_rows": c.ExportMaxRows, "smtp_host": c.Mail.Host,
	}
}
