package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestRequestIDGeneratedAndSanitised(t *testing.T) {
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, RequestID(r.Context()))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if id := rec.Header().Get(RequestIDHeader); !strings.HasPrefix(id, "req_") || rec.Body.String() != id {
		t.Fatalf("bad generated id %q", id)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(RequestIDHeader, "evil\r\nX: y")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(rec.Header().Get(RequestIDHeader), "evil") {
		t.Fatal("unsafe request id accepted")
	}

	req.Header.Set(RequestIDHeader, "client-id-12345")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get(RequestIDHeader) != "client-id-12345" {
		t.Fatal("valid client id not kept")
	}
}

func TestProblemShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteProblem(rec, httptest.NewRequest("GET", "/", nil), 404, "nope")
	var p Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Header().Get("Content-Type") != "application/problem+json" || p.Status != 404 || p.Title != "Not Found" {
		t.Fatalf("got %s %+v", rec.Header(), p)
	}
}

func TestRecovererHidesPanic(t *testing.T) {
	h := Recoverer(quiet)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret detail") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "secret detail") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestMaxBody(t *testing.T) {
	h := MaxBody(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			WriteProblem(w, r, 413, "too large")
			return
		}
		w.WriteHeader(204)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 11))))
	if rec.Code != 413 {
		t.Fatalf("declared oversize: got %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/", io.MultiReader(strings.NewReader(strings.Repeat("x", 11))))
	req.ContentLength = -1
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Fatalf("undeclared oversize: got %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for _, h := range []string{"X-Content-Type-Options", "Content-Security-Policy", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
}
