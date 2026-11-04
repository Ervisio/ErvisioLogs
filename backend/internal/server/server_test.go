package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	apipkg "github.com/Ervisio/ErvisioLogs/backend/internal/api"
	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"gopkg.in/yaml.v3"
	"os"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func router(checks ...Check) http.Handler {
	return NewRouter(Options{
		Log: quiet, Health: NewHealth(quiet, checks...), MaxBodyBytes: 1024,
		UI: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ui") }),
	})
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestLiveness(t *testing.T) {
	// Liveness must not depend on dependencies.
	rec := get(router(Check{"db", func(context.Context) error { return errors.New("down") }}), "/healthz")
	if rec.Code != 200 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestReadiness(t *testing.T) {
	ok := Check{"postgres", func(context.Context) error { return nil }}
	bad := Check{"clickhouse", func(context.Context) error { return errors.New("dial tcp 10.0.0.5:9000: password=hunter2") }}

	rec := get(router(ok), "/readyz")
	if rec.Code != 200 {
		t.Fatalf("ready: got %d", rec.Code)
	}

	rec = get(router(ok, bad), "/readyz")
	if rec.Code != 503 {
		t.Fatalf("not ready: got %d", rec.Code)
	}
	var body readyResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Checks["postgres"] != "ok" || body.Checks["clickhouse"] != "fail" {
		t.Fatalf("got %+v", body)
	}
	if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Fatal("error details leaked to client")
	}
}

func TestReadinessIsCached(t *testing.T) {
	n := 0
	h := router(Check{"x", func(context.Context) error { n++; return nil }})
	get(h, "/readyz")
	get(h, "/readyz")
	if n != 1 {
		t.Fatalf("check ran %d times", n)
	}
}

func TestUnknownAPIPathIsProblemJSON(t *testing.T) {
	rec := get(router(), "/api/v1/nope")
	if rec.Code != 404 || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing request id")
	}
}

func TestUIFallbackNotUsedForAPI(t *testing.T) {
	if b := get(router(), "/dashboard").Body.String(); b != "ui" {
		t.Fatalf("got %q", b)
	}
	if rec := get(router(), "/api/other"); rec.Code != 404 || rec.Body.String() == "ui" {
		t.Fatalf("api fallthrough: %d", rec.Code)
	}
}

func TestSecurityHeadersApplied(t *testing.T) {
	if get(router(), "/healthz").Header().Get("Content-Security-Policy") == "" {
		t.Fatal("no CSP")
	}
}

// TestOpenAPIMatchesRouter keeps api/openapi.yaml and the real routes in sync, in both directions.
func TestOpenAPIMatchesRouter(t *testing.T) {
	raw, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.1") {
		t.Fatalf("openapi version %q, want 3.1.x", doc.OpenAPI)
	}
	documented := map[string]bool{}
	for p, ops := range doc.Paths {
		for m := range ops {
			documented[strings.ToUpper(m)+" "+p] = true
		}
	}
	implemented := map[string]bool{}
	// The contract test needs the feature routes; handlers are never called, so no store is required.
	full := NewRouter(Options{Log: quiet, Health: NewHealth(quiet), MaxBodyBytes: 1024,
		API: apipkg.New(nil, nil, config.Auth{PublicURL: "http://localhost:8080", SecretKey: strings.Repeat("k", 32)}, nil, nil, quiet)})
	err = chi.Walk(full.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		implemented[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for k := range documented {
		if !implemented[k] {
			t.Errorf("documented but not implemented: %s", k)
		}
	}
	for k := range implemented {
		if !documented[k] {
			t.Errorf("implemented but not documented: %s", k)
		}
	}
}
