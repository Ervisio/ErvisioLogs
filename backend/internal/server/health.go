package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/version"
)

// Check is one named readiness probe.
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
}

// Health serves liveness and readiness. Readiness results are cached briefly so that probes
// (and anyone hammering the endpoint) cannot turn into a load source on the databases.
//
// Checks run in the background on a context independent from any request, and concurrent requests
// share one run: a client that disconnects affects neither the result nor the other waiters.
type Health struct {
	checks  []Check
	log     *slog.Logger
	ttl     time.Duration
	timeout time.Duration

	mu       sync.Mutex // guards the fields below; never held while checks run
	at       time.Time
	result   map[string]string
	ready    bool
	inflight *healthRun
}

// healthRun is one execution of the checks; result and ready are valid once done is closed.
type healthRun struct {
	done   chan struct{}
	result map[string]string
	ready  bool
}

func NewHealth(log *slog.Logger, checks ...Check) *Health {
	return &Health{checks: checks, log: log, ttl: 2 * time.Second, timeout: 3 * time.Second}
}

// Live reports that the process is up. It never touches dependencies.
func (h *Health) Live(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
}

type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Ready reports whether all dependencies answer and migrations are current.
// Responses carry only ok/fail per check; error details go to the server log, never to clients.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	checks, ready, err := h.run(r.Context())
	if err != nil {
		// The caller gave up before the checks finished: this says nothing about the dependencies.
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "readiness check abandoned")
		return
	}
	status, code := "ready", http.StatusOK
	if !ready {
		status, code = "not_ready", http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, code, readyResponse{Status: status, Checks: checks})
}

// run returns the cached result, or waits for a background run until ctx ends.
func (h *Health) run(ctx context.Context) (map[string]string, bool, error) {
	h.mu.Lock()
	if h.result != nil && time.Since(h.at) < h.ttl {
		defer h.mu.Unlock()
		return h.result, h.ready, nil
	}
	run := h.inflight
	if run == nil {
		run = &healthRun{done: make(chan struct{})}
		h.inflight = run
		go h.execute(run)
	}
	h.mu.Unlock()

	select {
	case <-run.done:
		return run.result, run.ready, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

// execute runs all checks in parallel, so a run lasts at most one timeout whatever their number.
func (h *Health) execute(run *healthRun) {
	type outcome struct {
		name string
		err  error
	}
	outcomes := make(chan outcome, len(h.checks))
	for _, c := range h.checks {
		go func() {
			outcomes <- outcome{c.Name, h.probe(c)}
		}()
	}
	res, ready, cacheable := make(map[string]string, len(h.checks)), true, true
	for range h.checks {
		o := <-outcomes
		if o.err == nil {
			res[o.name] = "ok"
			continue
		}
		ready = false
		res[o.name] = "fail"
		h.log.Warn("readiness check failed", "check", o.name, "error", o.err)
		// A cancelled check did not measure the dependency. A deadline did: it stays cacheable.
		if errors.Is(o.err, context.Canceled) {
			cacheable = false
		}
	}
	run.result, run.ready = res, ready

	h.mu.Lock()
	if cacheable {
		h.result, h.ready, h.at = res, ready, time.Now()
	} else {
		h.result = nil
	}
	h.inflight = nil
	h.mu.Unlock()
	close(run.done)
}

// probe runs one check with its own timeout; a panic counts as a failure, not a crash, because
// this goroutine is outside the HTTP recoverer.
func (h *Health) probe(c Check) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic: %v", rec)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	return c.Fn(ctx)
}
