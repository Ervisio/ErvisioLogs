// Package settings resolves the runtime settings (ADR 0018). A value comes from, in order: the
// environment variable that backs the key, the stored value of the project, of its workspace, of the
// instance, and last the default of the configuration. Secrets are never settings.
package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

const (
	OpenRegistration    = "auth.open_registration"
	SessionTTL          = "auth.session_ttl"
	LogLevel            = "server.log_level"
	IngestMaxFutureSkew = "ingest.max_future_skew"
	IngestMaxPastAge    = "ingest.max_past_age"
	AutoCreateStreams   = "ingest.auto_create_streams"
	MaxStreams          = "ingest.max_streams"
	ArchiveInterval     = "archive.interval"
	ArchiveGrace        = "archive.grace"
	ArchiveLateInterval = "archive.late_interval"
	AlertsInterval      = "alerts.interval"
	AlertsHistory       = "alerts.history_retention"
	WebhookAllowPrivate = "alerts.webhook_allow_private"
	ExportMaxRows       = "export.max_rows"
	SMTPHost            = "email.smtp_host"
	SMTPPort            = "email.smtp_port"
	SMTPSecurity        = "email.smtp_security"
	SMTPUsername        = "email.smtp_username"
	SMTPPasswordFile    = "email.smtp_password_file"
	MailFrom            = "email.from"
)

type Kind string

const (
	Bool     Kind = "bool"
	Int      Kind = "int"
	Duration Kind = "duration" // whole seconds on the wire and in the database
	Choice   Kind = "choice"
	String   Kind = "string"
)

// Def describes a setting. Values are bool, int64 (Int and Duration) or string (Choice, String).
type Def struct {
	Key      string
	Kind     Kind
	Env      string
	Default  any
	Min, Max int64
	Options  []string
	// Check validates a non-empty String value; Max bounds its length.
	Check func(string) error
	// PerProject settings can also be stored for a workspace and for a project.
	PerProject bool
}

const day = 24 * 60 * 60

func defs(c config.Config) []Def {
	secs := func(d time.Duration) int64 { return int64(d / time.Second) }
	return []Def{
		{Key: OpenRegistration, Kind: Bool, Env: "OPEN_REGISTRATION", Default: c.Auth.OpenRegistration},
		{Key: SessionTTL, Kind: Duration, Env: "SESSION_TTL", Default: secs(c.Auth.SessionTTL), Min: 300, Max: 366 * day},
		{Key: LogLevel, Kind: Choice, Env: "LOG_LEVEL", Default: strings.ToLower(c.LogLevel.String()),
			Options: []string{"debug", "info", "warn", "error"}},
		{Key: IngestMaxFutureSkew, Kind: Duration, Env: "INGEST_MAX_FUTURE_SKEW", Default: secs(c.Ingest.MaxFutureSkew), Max: day},
		{Key: IngestMaxPastAge, Kind: Duration, Env: "INGEST_MAX_PAST_AGE", Default: secs(c.Ingest.MaxPastAge), Min: 3600, Max: 366 * day},
		{Key: AutoCreateStreams, Kind: Bool, Default: true, PerProject: true},
		// 0 means no limit.
		{Key: MaxStreams, Kind: Int, Default: int64(20), Max: 10000, PerProject: true},
		{Key: ArchiveInterval, Kind: Duration, Env: "ARCHIVE_INTERVAL", Default: secs(c.Archive.Interval), Min: 60, Max: day},
		{Key: ArchiveGrace, Kind: Duration, Env: "ARCHIVE_GRACE", Default: secs(c.Archive.Grace), Max: 7 * day},
		{Key: ArchiveLateInterval, Kind: Duration, Env: "ARCHIVE_LATE_INTERVAL", Default: secs(c.Archive.LateInterval), Max: 7 * day},
		{Key: AlertsInterval, Kind: Duration, Env: "ALERTS_INTERVAL", Default: secs(c.Alerts.Interval), Min: 5, Max: 3600},
		{Key: AlertsHistory, Kind: Duration, Env: "ALERTS_HISTORY_RETENTION", Default: secs(c.Alerts.HistoryRetention), Min: 3600, Max: 3660 * day},
		{Key: WebhookAllowPrivate, Kind: Bool, Env: "WEBHOOK_ALLOW_PRIVATE", Default: c.Alerts.WebhookAllowPrivate},
		{Key: ExportMaxRows, Kind: Int, Env: "EXPORT_MAX_ROWS", Default: int64(c.ExportMaxRows), Min: 1, Max: 50_000_000},
		// An empty host turns email off.
		{Key: SMTPHost, Kind: String, Env: "SMTP_HOST", Default: c.Mail.Host, Max: 253, Check: checkHost},
		{Key: SMTPPort, Kind: Int, Env: "SMTP_PORT", Default: int64(c.Mail.Port), Min: 1, Max: 65535},
		{Key: SMTPSecurity, Kind: Choice, Env: "SMTP_SECURITY", Default: c.Mail.Security, Options: []string{"starttls", "tls", "none"}},
		{Key: SMTPUsername, Kind: String, Env: "SMTP_USERNAME", Default: c.Mail.Username, Max: 256},
		// The password is a file in the secrets directory, unless ERVISIO_LOGS_SMTP_PASSWORD is set.
		{Key: SMTPPasswordFile, Kind: String, Default: "", Max: 100, Check: checkFileName},
		{Key: MailFrom, Kind: String, Env: "SMTP_FROM", Default: c.Mail.From, Max: 320, Check: checkAddress},
	}
}

func checkHost(v string) error {
	if strings.ContainsAny(v, " /:@") {
		return errors.New("must be a host name or address, without port")
	}
	return nil
}

func checkFileName(v string) error {
	if strings.HasPrefix(v, ".") || strings.ContainsAny(v, "/\\") {
		return errors.New("must be a file name in the secrets directory")
	}
	return nil
}

func checkAddress(v string) error {
	if _, err := mail.ParseAddress(v); err != nil {
		return errors.New("must be an address such as Ervisio Logs <logs@example.com>")
	}
	return nil
}

// Scope is where a value applies: the zero Scope is the instance, a Scope without Project is a
// workspace.
type Scope struct{ Workspace, Project uuid.UUID }

func (s Scope) level() string {
	switch {
	case s.Project != uuid.Nil:
		return "project"
	case s.Workspace != uuid.Nil:
		return "workspace"
	}
	return "instance"
}

func (s Scope) parent() Scope {
	if s.Project != uuid.Nil {
		return Scope{Workspace: s.Workspace}
	}
	return Scope{}
}

func (s Scope) ids() (ws, project *uuid.UUID) {
	if s.Workspace != uuid.Nil {
		ws = &s.Workspace
	}
	if s.Project != uuid.Nil {
		project = &s.Project
	}
	return ws, project
}

var (
	ErrUnknown = errors.New("unknown setting")
	ErrLocked  = errors.New("set by an environment variable on the server")
)

// Invalid is a value the setting does not accept. Its text is safe to show.
type Invalid string

func (e Invalid) Error() string { return string(e) }

type Store interface {
	ListSettings(ctx context.Context) ([]store.Setting, error)
	PutSetting(ctx context.Context, st store.Setting, by *uuid.UUID) error
	DeleteSetting(ctx context.Context, workspaceID, projectID *uuid.UUID, key string) error
}

type Service struct {
	store  Store
	defs   []Def
	locked map[string]bool
	values atomic.Pointer[map[Scope]map[string]any]
	// OnLoad runs after every load, for code that applies a setting outside the getters.
	OnLoad func()
}

// New returns a Service holding the defaults of c. Call Load to read the stored values; a nil
// store keeps the defaults.
func New(st Store, c config.Config) *Service {
	s := &Service{store: st, defs: defs(c), locked: map[string]bool{}}
	for _, d := range s.defs {
		if d.Env != "" && c.FromEnv["ERVISIO_LOGS_"+d.Env] {
			s.locked[d.Key] = true
		}
	}
	s.values.Store(&map[Scope]map[string]any{})
	return s
}

func (s *Service) def(key string) (Def, bool) {
	i := slices.IndexFunc(s.defs, func(d Def) bool { return d.Key == key })
	if i < 0 {
		return Def{}, false
	}
	return s.defs[i], true
}

// Load reads the stored values. Rows with a key this version does not know, or with a value it no
// longer accepts, are left out: an upgrade must not fail because of an old row.
func (s *Service) Load(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	rows, err := s.store.ListSettings(ctx)
	if err != nil {
		return err
	}
	vals := map[Scope]map[string]any{}
	for _, r := range rows {
		d, ok := s.def(r.Key)
		if !ok {
			continue
		}
		v, err := d.parse(r.Value)
		if err != nil {
			continue
		}
		var sc Scope
		if r.WorkspaceID != nil {
			sc.Workspace = *r.WorkspaceID
		}
		if r.ProjectID != nil {
			sc.Project = *r.ProjectID
		}
		if vals[sc] == nil {
			vals[sc] = map[string]any{}
		}
		vals[sc][r.Key] = v
	}
	s.values.Store(&vals)
	if s.OnLoad != nil {
		s.OnLoad()
	}
	return nil
}

// Run reloads the values every interval, so changes made through another instance arrive here.
func (s *Service) Run(ctx context.Context, every time.Duration, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.Load(ctx); err != nil && ctx.Err() == nil {
			log.Warn("reloading settings failed", "error", err)
		}
	}
}

func (s *Service) resolve(d Def, sc Scope) (any, string) {
	if s.locked[d.Key] {
		return d.Default, "env"
	}
	vals := *s.values.Load()
	for {
		if v, ok := vals[sc][d.Key]; ok {
			return v, sc.level()
		}
		if sc == (Scope{}) {
			return d.Default, "default"
		}
		sc = sc.parent()
	}
}

func (s *Service) get(key string, sc Scope) any {
	d, ok := s.def(key)
	if !ok {
		panic("settings: unknown key " + key)
	}
	v, _ := s.resolve(d, sc)
	return v
}

func (s *Service) Bool(key string) bool { return s.get(key, Scope{}).(bool) }

func (s *Service) Int(key string) int64 { return s.get(key, Scope{}).(int64) }

func (s *Service) String(key string) string { return s.get(key, Scope{}).(string) }

func (s *Service) Duration(key string) time.Duration {
	return time.Duration(s.get(key, Scope{}).(int64)) * time.Second
}

func (s *Service) ProjectBool(ws, project uuid.UUID, key string) bool {
	return s.get(key, Scope{ws, project}).(bool)
}

func (s *Service) ProjectInt(ws, project uuid.UUID, key string) int64 {
	return s.get(key, Scope{ws, project}).(int64)
}

// Entry is a setting as the API shows it at one scope. Inherited is the value the scope would get
// without its own.
type Entry struct {
	Key       string   `json:"key"`
	Type      Kind     `json:"type"`
	Value     any      `json:"value"`
	Source    string   `json:"source"`
	Inherited any      `json:"inherited"`
	Locked    bool     `json:"locked"`
	Env       string   `json:"env,omitempty"`
	Min       *int64   `json:"min,omitempty"`
	Max       *int64   `json:"max,omitempty"`
	Options   []string `json:"options,omitempty"`
}

// Describe lists the settings that can be stored at sc.
func (s *Service) Describe(sc Scope) []Entry {
	out := []Entry{}
	for _, d := range s.defs {
		if e, ok := s.entry(d, sc); ok {
			out = append(out, e)
		}
	}
	return out
}

// Entry describes one setting at sc.
func (s *Service) Entry(sc Scope, key string) (Entry, error) {
	d, ok := s.def(key)
	if !ok {
		return Entry{}, ErrUnknown
	}
	e, ok := s.entry(d, sc)
	if !ok {
		return Entry{}, ErrUnknown
	}
	return e, nil
}

func (s *Service) entry(d Def, sc Scope) (Entry, bool) {
	if sc != (Scope{}) && !d.PerProject {
		return Entry{}, false
	}
	e := Entry{Key: d.Key, Type: d.Kind, Locked: s.locked[d.Key], Options: d.Options}
	e.Value, e.Source = s.resolve(d, sc)
	e.Inherited = d.Default
	if sc != (Scope{}) {
		e.Inherited, _ = s.resolve(d, sc.parent())
	}
	if d.Env != "" {
		e.Env = "ERVISIO_LOGS_" + d.Env
	}
	switch d.Kind {
	case Int, Duration:
		e.Min, e.Max = &d.Min, &d.Max
	case String:
		e.Max = &d.Max
	}
	return e, true
}

// Set stores the value of key at sc and reloads.
func (s *Service) Set(ctx context.Context, sc Scope, key string, raw json.RawMessage, by *uuid.UUID) (Entry, error) {
	e, err := s.Entry(sc, key)
	if err != nil {
		return e, err
	}
	if e.Locked {
		return e, ErrLocked
	}
	d, _ := s.def(key)
	v, err := d.parse(raw)
	if err != nil {
		return e, err
	}
	canonical, _ := json.Marshal(v)
	ws, project := sc.ids()
	if err := s.store.PutSetting(ctx, store.Setting{WorkspaceID: ws, ProjectID: project, Key: key, Value: canonical}, by); err != nil {
		return e, err
	}
	if err := s.Load(ctx); err != nil {
		return e, err
	}
	return s.Entry(sc, key)
}

// Reset removes the value of key at sc, so the scope inherits again. It works on a locked key too,
// to clear a value stored before the variable was set.
func (s *Service) Reset(ctx context.Context, sc Scope, key string) (Entry, error) {
	if _, err := s.Entry(sc, key); err != nil {
		return Entry{}, err
	}
	ws, project := sc.ids()
	if err := s.store.DeleteSetting(ctx, ws, project, key); err != nil {
		return Entry{}, err
	}
	if err := s.Load(ctx); err != nil {
		return Entry{}, err
	}
	return s.Entry(sc, key)
}

func (d Def) parse(raw json.RawMessage) (any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, Invalid("value is required")
	}
	switch d.Kind {
	case Bool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return nil, Invalid(d.Key + " must be true or false")
		}
		return b, nil
	case Int, Duration:
		var n int64
		if json.Unmarshal(raw, &n) != nil || n < d.Min || n > d.Max {
			unit := "a whole number"
			if d.Kind == Duration {
				unit = "a number of seconds"
			}
			return nil, Invalid(fmt.Sprintf("%s must be %s from %d to %d", d.Key, unit, d.Min, d.Max))
		}
		return n, nil
	case Choice:
		var c string
		if json.Unmarshal(raw, &c) != nil || !slices.Contains(d.Options, c) {
			return nil, Invalid(d.Key + " must be one of " + strings.Join(d.Options, ", "))
		}
		return c, nil
	case String:
		var v string
		if json.Unmarshal(raw, &v) != nil || int64(len(v)) > d.Max || strings.ContainsFunc(v, unicode.IsControl) {
			return nil, Invalid(fmt.Sprintf("%s must be text of at most %d characters", d.Key, d.Max))
		}
		v = strings.TrimSpace(v)
		if v != "" && d.Check != nil {
			if err := d.Check(v); err != nil {
				return nil, Invalid(d.Key + " " + err.Error())
			}
		}
		return v, nil
	}
	return nil, Invalid("unsupported setting")
}
