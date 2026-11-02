// Package httpx holds HTTP primitives shared by all handlers: RFC 7807 errors and middleware.
package httpx

import (
	"encoding/json"
	"net/http"
)

// Problem is an RFC 7807 problem document extended with the request id.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// WriteProblem writes an application/problem+json response. Detail must be safe to show to clients.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	p := Problem{
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		RequestID: RequestID(r.Context()),
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
