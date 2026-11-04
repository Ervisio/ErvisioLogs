package settings

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

type fakeStore struct{ rows []store.Setting }

func same(a, b *uuid.UUID) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func (f *fakeStore) ListSettings(context.Context) ([]store.Setting, error) { return f.rows, nil }

func (f *fakeStore) PutSetting(ctx context.Context, st store.Setting, _ *uuid.UUID) error {
	_ = f.DeleteSetting(ctx, st.WorkspaceID, st.ProjectID, st.Key)
	f.rows = append(f.rows, st)
	return nil
}

func (f *fakeStore) DeleteSetting(_ context.Context, ws, project *uuid.UUID, key string) error {
	kept := f.rows[:0]
	for _, r := range f.rows {
		if !(r.Key == key && same(r.WorkspaceID, ws) && same(r.ProjectID, project)) {
			kept = append(kept, r)
		}
	}
	f.rows = kept
	return nil
}

func testConfig() config.Config {
	var c config.Config
	c.Auth.SessionTTL = 720 * time.Hour
	c.Alerts.Interval = 30 * time.Second
	c.ExportMaxRows = 100000
	c.FromEnv = map[string]bool{}
	return c
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestDefaultsAndInstanceOverride(t *testing.T) {
	ctx := context.Background()
	s := New(&fakeStore{}, testConfig())
	if got := s.Duration(SessionTTL); got != 720*time.Hour {
		t.Fatalf("default session ttl = %v", got)
	}
	e, err := s.Set(ctx, Scope{}, SessionTTL, raw("3600"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Source != "instance" || s.Duration(SessionTTL) != time.Hour {
		t.Fatalf("after set: %+v, %v", e, s.Duration(SessionTTL))
	}
	e, err = s.Reset(ctx, Scope{}, SessionTTL)
	if err != nil || e.Source != "default" || s.Duration(SessionTTL) != 720*time.Hour {
		t.Fatalf("after reset: %+v, %v", e, err)
	}
}

func TestProjectInheritance(t *testing.T) {
	ctx := context.Background()
	s := New(&fakeStore{}, testConfig())
	ws, p := uuid.New(), uuid.New()
	if s.ProjectInt(ws, p, MaxStreams) != 20 {
		t.Fatal("default max streams")
	}
	if _, err := s.Set(ctx, Scope{}, MaxStreams, raw("5"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set(ctx, Scope{Workspace: ws}, MaxStreams, raw("0"), nil); err != nil {
		t.Fatal(err)
	}
	if got := s.ProjectInt(ws, p, MaxStreams); got != 0 {
		t.Fatalf("project inherits workspace: %d", got)
	}
	if got := s.ProjectInt(uuid.New(), uuid.New(), MaxStreams); got != 5 {
		t.Fatalf("other workspace inherits instance: %d", got)
	}
	e, err := s.Set(ctx, Scope{ws, p}, MaxStreams, raw("3"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Source != "project" || e.Value != int64(3) || e.Inherited != int64(0) {
		t.Fatalf("project entry: %+v", e)
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	c := testConfig()
	c.FromEnv["ERVISIO_LOGS_SESSION_TTL"] = true
	s := New(&fakeStore{}, c)
	var inv Invalid
	cases := []struct {
		name  string
		scope Scope
		key   string
		value string
		want  func(error) bool
	}{
		{"unknown", Scope{}, "nope", "1", func(e error) bool { return errors.Is(e, ErrUnknown) }},
		{"locked", Scope{}, SessionTTL, "3600", func(e error) bool { return errors.Is(e, ErrLocked) }},
		{"instance only", Scope{Workspace: uuid.New()}, ExportMaxRows, "5", func(e error) bool { return errors.Is(e, ErrUnknown) }},
		{"below min", Scope{}, AlertsInterval, "1", func(e error) bool { return errors.As(e, &inv) }},
		{"wrong type", Scope{}, OpenRegistration, `"yes"`, func(e error) bool { return errors.As(e, &inv) }},
		{"bad choice", Scope{}, LogLevel, `"trace"`, func(e error) bool { return errors.As(e, &inv) }},
		{"null", Scope{}, LogLevel, "null", func(e error) bool { return errors.As(e, &inv) }},
	}
	for _, tc := range cases {
		if _, err := s.Set(ctx, tc.scope, tc.key, raw(tc.value), nil); !tc.want(err) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
	if e, _ := s.Entry(Scope{}, SessionTTL); !e.Locked || e.Source != "env" || e.Env != "ERVISIO_LOGS_SESSION_TTL" {
		t.Errorf("locked entry: %+v", e)
	}
}

func TestLoadSkipsStaleRows(t *testing.T) {
	st := &fakeStore{rows: []store.Setting{
		{Key: "removed.key", Value: raw("1")},
		{Key: AlertsInterval, Value: raw("1")},
		{Key: ExportMaxRows, Value: raw("42")},
	}}
	s := New(st, testConfig())
	loaded := 0
	s.OnLoad = func() { loaded++ }
	if err := s.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Duration(AlertsInterval) != 30*time.Second || s.Int(ExportMaxRows) != 42 || loaded != 1 {
		t.Fatalf("interval %v rows %d loaded %d", s.Duration(AlertsInterval), s.Int(ExportMaxRows), loaded)
	}
}

func TestNilStore(t *testing.T) {
	s := New(nil, testConfig())
	if err := s.Load(context.Background()); err != nil || s.Int(ExportMaxRows) != 100000 {
		t.Fatal("nil store keeps defaults")
	}
	if len(s.Describe(Scope{})) != len(s.defs) {
		t.Fatal("describe instance lists every setting")
	}
	if len(s.Describe(Scope{Workspace: uuid.New()})) != 2 {
		t.Fatal("describe workspace lists the per-project settings")
	}
}
