package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := cors(next)
	for _, tc := range []struct {
		name, method, path string
		preflight          bool
		code               int
		acao               string
	}{
		{"preflight on an admin route", http.MethodOptions, "/admin/users", true, http.StatusNoContent, "*"},
		{"preflight on a tenancy route", http.MethodOptions, "/workspaces/x/members", true, http.StatusNoContent, "*"},
		{"simple request", http.MethodGet, "/projects", false, http.StatusTeapot, "*"},
		{"auth routes stay same-origin", http.MethodOptions, "/auth/login", true, http.StatusTeapot, ""},
		{"log routes stay same-origin", http.MethodPost, "/logs/search", false, http.StatusTeapot, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("Origin", "https://plugin.example")
			if tc.preflight {
				r.Header.Set("Access-Control-Request-Method", http.MethodPost)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != tc.code || rec.Header().Get("Access-Control-Allow-Origin") != tc.acao {
				t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
			}
			if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatal("credentials must never be allowed")
			}
			if tc.code == http.StatusNoContent && rec.Header().Get("Access-Control-Allow-Headers") != "Authorization, Content-Type" {
				t.Fatalf("allow headers %q", rec.Header().Get("Access-Control-Allow-Headers"))
			}
		})
	}
}
