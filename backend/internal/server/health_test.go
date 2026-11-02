package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A client that disconnects must not cancel the checks, poison the cache, or block other callers.
func TestReadinessSurvivesClientCancel(t *testing.T) {
	release := make(chan struct{})
	var runs atomic.Int32
	var ctxErr atomic.Value
	h := router(Check{"slow", func(ctx context.Context) error {
		runs.Add(1)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			ctxErr.Store(ctx.Err())
			return ctx.Err()
		}
	}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil).WithContext(ctx))
	if d := time.Since(start); d > time.Second {
		t.Fatalf("cancelled request blocked for %s", d)
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("cancelled request: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	close(release)

	// The next caller joins the same run and gets its real result, not a cached 503.
	if rec := get(h, "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("after cancel: %d %s", rec.Code, rec.Body)
	}
	if v := ctxErr.Load(); v != nil {
		t.Fatalf("client cancellation reached the check: %v", v)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("check ran %d times", n)
	}
}

func TestCancelledCheckIsNotCached(t *testing.T) {
	var runs atomic.Int32
	h := router(Check{"x", func(context.Context) error {
		if runs.Add(1) == 1 {
			return context.Canceled
		}
		return nil
	}})
	if rec := get(h, "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("first: %d", rec.Code)
	}
	if rec := get(h, "/readyz"); rec.Code != http.StatusOK {
		t.Fatalf("cancelled outcome was cached: %d", rec.Code)
	}
}

func TestRealFailureIsCached(t *testing.T) {
	var runs atomic.Int32
	h := router(Check{"x", func(context.Context) error { runs.Add(1); return errors.New("down") }})
	get(h, "/readyz")
	get(h, "/readyz")
	if n := runs.Load(); n != 1 {
		t.Fatalf("check ran %d times", n)
	}
}

// A hanging dependency must cost one timeout overall, however many checks there are.
func TestChecksRunWithinOneTimeout(t *testing.T) {
	hang := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	health := NewHealth(quiet, Check{"a", hang}, Check{"b", hang}, Check{"c", hang}, Check{"d", hang})
	health.timeout = 100 * time.Millisecond
	start := time.Now()
	_, ready, err := health.run(context.Background())
	if err != nil || ready {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Fatalf("took %s", d)
	}
}

func TestPanickingCheckFailsWithoutCrashing(t *testing.T) {
	h := router(Check{"x", func(context.Context) error { panic("boom") }})
	if rec := get(h, "/readyz"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestProbeMethodNotAllowedIsProblemJSON(t *testing.T) {
	for _, p := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		router().ServeHTTP(rec, httptest.NewRequest("POST", p, nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Allow") != "GET" {
			t.Fatalf("%s: Allow %q", p, rec.Header().Get("Allow"))
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["status"] != float64(405) {
			t.Fatalf("%s: %s", p, rec.Body)
		}
	}
}
