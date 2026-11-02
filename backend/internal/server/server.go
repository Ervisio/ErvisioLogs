// Package server wires the HTTP router and middleware.
package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
)

// Options configure the router.
type Options struct {
	Log          *slog.Logger
	Health       *Health
	UI           http.Handler // served for non-API paths; may be nil
	MaxBodyBytes int64
}

// NewRouter builds the HTTP handler. API routes live under /api/v1; probes are at the root.
func NewRouter(o Options) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestIDMiddleware, httpx.AccessLog(o.Log), httpx.Recoverer(o.Log), httpx.SecurityHeaders, httpx.MaxBody(o.MaxBodyBytes))

	r.Get("/healthz", o.Health.Live)
	r.Get("/readyz", o.Health.Ready)

	r.Route("/api/v1", func(api chi.Router) {
		// Feature routes are added by later phases. Unknown API paths answer with a problem document.
		api.NotFound(func(w http.ResponseWriter, req *http.Request) {
			httpx.WriteProblem(w, req, http.StatusNotFound, "not found")
		})
		api.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
			httpx.WriteProblem(w, req, http.StatusMethodNotAllowed, "method not allowed")
		})
	})

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if o.UI == nil || strings.HasPrefix(req.URL.Path, "/api/") {
			httpx.WriteProblem(w, req, http.StatusNotFound, "not found")
			return
		}
		o.UI.ServeHTTP(w, req)
	})
	return r
}

// NewHTTPServer returns an http.Server with defensive timeouts (slowloris protection).
// WriteTimeout is deliberately unset: SSE tail streams (P2) are long-lived.
func NewHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
}
