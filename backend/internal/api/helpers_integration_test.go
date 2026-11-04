//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/db"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/migrate"
	"github.com/Ervisio/ErvisioLogs/backend/internal/oauth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/Ervisio/ErvisioLogs/backend/migrations"
)

const testSecret = "0123456789abcdef0123456789abcdef-test"

type env struct {
	t        *testing.T
	api      *API
	store    *store.Store
	pool     *pgxpool.Pool
	srv      *httptest.Server
	cfg      config.Auth
	exec     func(q string, args ...any) error
	queryRow func(q string, args ...any) interface{ Scan(...any) error }
}

// newEnv migrates a clean PostgreSQL schema and starts the real router on an httptest server. tweak
// changes the API before it starts serving.
func newEnv(t *testing.T, mutate func(*config.Auth), providers map[string]oauth.Provider, tweak ...func(*API)) *env {
	t.Helper()
	dsn := os.Getenv("ERVISIO_LOGS_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ERVISIO_LOGS_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	pool, err := db.OpenPostgres(ctx, config.Postgres{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ms, err := migrate.Load(migrations.FS, migrations.PostgresDir)
	if err != nil {
		t.Fatal(err)
	}
	drv := migrate.Postgres{Pool: pool}
	// Throwaway database only: wipe the schema so a changed migration cannot trip the checksum guard.
	if !strings.Contains(dsn, "@127.0.0.1:") && !strings.Contains(dsn, "@localhost:") {
		t.Fatal("refusing to reset a non-local database")
	}
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Up(ctx, drv, ms); err != nil {
		t.Fatal(err)
	}

	e := &env{t: t, pool: pool}
	e.cfg = config.Auth{PublicURL: "http://app.test", SecretKey: testSecret, SetupCode: "setup-code-123", SessionTTL: time.Hour}
	if mutate != nil {
		mutate(&e.cfg)
	}
	e.store = store.New(pool)
	fast := auth.NewHasher(auth.Params{Memory: 8, Time: 1, Threads: 1}, 4)
	sets := settings.New(e.store, config.Config{Auth: e.cfg, ExportMaxRows: 1_000_000, FromEnv: map[string]bool{}})
	if err := sets.Load(ctx); err != nil {
		t.Fatal(err)
	}
	e.api = New(e.store, fast, e.cfg, sets, providers, slog.New(slog.NewTextHandler(testWriter{t}, nil)))
	for _, f := range tweak {
		f(e.api)
	}
	root := chi.NewRouter()
	root.Use(httpx.RequestIDMiddleware)
	root.Route("/api/v1", e.api.Routes)
	e.api.OTLPRoutes(root)
	e.srv = httptest.NewServer(root)
	t.Cleanup(e.srv.Close)
	e.exec = func(q string, args ...any) error { _, err := pool.Exec(ctx, q, args...); return err }
	e.queryRow = func(q string, args ...any) interface{ Scan(...any) error } { return pool.QueryRow(ctx, q, args...) }
	return e
}

// client is a browser-like HTTP client: it keeps cookies and sends the CSRF token it learned.
type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func (e *env) newClient() *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: e.t, base: e.srv.URL, http: &http.Client{Jar: jar, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

type resp struct {
	Status int
	Body   []byte
	Header http.Header
}

func (r resp) JSON() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.Body, &m)
	return m
}

func (c *client) do(method, path string, body any, hdr map[string]string) resp {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+"/api/v1"+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" && isUnsafe(method) {
		req.Header.Set(CSRFHeader, c.csrf)
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{res.StatusCode, b, res.Header}
}

func (c *client) get(path string) resp           { return c.do("GET", path, nil, nil) }
func (c *client) post(path string, b any) resp   { return c.do("POST", path, b, nil) }
func (c *client) patch(path string, b any) resp  { return c.do("PATCH", path, b, nil) }
func (c *client) delete(path string, b any) resp { return c.do("DELETE", path, b, nil) }
func (c *client) bearer(method, path, tok string) resp {
	return c.do(method, path, nil, map[string]string{"Authorization": "Bearer " + tok})
}

// adopt stores the CSRF token returned by login/register/me.
func (c *client) adopt(r resp) resp {
	if t, ok := r.JSON()["csrf_token"].(string); ok {
		c.csrf = t
	}
	return r
}

func expect(t *testing.T, r resp, status int) resp {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status %d, want %d: %s", r.Status, status, r.Body)
	}
	return r
}

// setupAdmin creates the first account with the setup code and returns its logged-in client.
func (e *env) setupAdmin() *client {
	c := e.newClient()
	expect(e.t, c.adopt(c.post("/auth/register", map[string]string{
		"email": "admin@example.com", "username": "admin", "password": "admin-password-1", "setup_code": e.cfg.SetupCode})), 201)
	return c
}

// newUser makes an account through the admin API and returns its logged-in client.
func (e *env) newUser(admin *client, name string) *client {
	expect(e.t, admin.post("/admin/users", map[string]string{
		"email": name + "@example.com", "username": name, "password": name + "-password-1"}), 201)
	c := e.newClient()
	expect(e.t, c.adopt(c.post("/auth/login", map[string]string{"login": name, "password": name + "-password-1"})), 200)
	return c
}

func id(r resp, keys ...string) string {
	m := r.JSON()
	for _, k := range keys[:len(keys)-1] {
		m, _ = m[k].(map[string]any)
	}
	s, _ := m[keys[len(keys)-1]].(string)
	return s
}

func has(b []byte, s string) bool { return strings.Contains(string(b), s) }

// testWriter routes application logs into the test output (shown on failure or with -v).
type testWriter struct{ t *testing.T }

func (w testWriter) Write(b []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(b)))
	return len(b), nil
}
