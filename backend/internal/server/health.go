package server

import (
	"context"
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
type Health struct {
	checks  []Check
	log     *slog.Logger
	ttl     time.Duration
	timeout time.Duration

	mu     sync.Mutex
	at     time.Time
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
	checks, ready := h.run(r.Context())
	status, code := "ready", http.StatusOK
	if !ready {
		status, code = "not_ready", http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, code, readyResponse{Status: status, Checks: checks})
}

func (h *Health) run(ctx context.Context) (map[string]string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.result != nil && time.Since(h.at) < h.ttl {
		return h.result, h.ready
	}
	res, ready := make(map[string]string, len(h.checks)), true
	for _, c := range h.checks {
		cctx, cancel := context.WithTimeout(ctx, h.timeout)
		err := c.Fn(cctx)
		cancel()
		if err != nil {
			ready = false
			res[c.Name] = "fail"
			h.log.Warn("readiness check failed", "check", c.Name, "error", err)
		} else {
			res[c.Name] = "ok"
		}
	}
	h.result, h.ready, h.at = res, ready, time.Now()
	return res, ready
}
