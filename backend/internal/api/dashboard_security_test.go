package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

func TestPanelSQLScopesAndValidatesField(t *testing.T) {
	q := &query{where: []string{"workspace_id = ?", "project_id = ?"}, args: []any{"workspace", "project"}}
	for _, typ := range []string{"count", "timeseries", "top"} {
		sql, args, err := panelSQL(store.DashboardPanel{Type: typ, Field: "attributes.actor.id", IntervalSeconds: 3600, Limit: 10}, "events", q)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sql, "workspace_id = ? AND project_id = ?") {
			t.Fatal("missing tenant scope")
		}
		if len(args) < 2 || args[0] != "workspace" || args[1] != "project" {
			t.Fatal("lost tenant args")
		}
	}
	for _, field := range []string{"attributes.x`)", "source_host", "attributes.a;DROP TABLE users", "attributes.", "attributes.a..b"} {
		if _, err := panelField(field); err == nil {
			t.Errorf("accepted %q", field)
		}
	}
}
func TestPasskeyCeremonyRequiresBrowserBinding(t *testing.T) {
	a := &API{}
	w := httptest.NewRecorder()
	a.ceremonyCookie(w, "unguessable-challenge")
	req := httptest.NewRequest("POST", "/api/v1/auth/passkeys/login/finish", nil)
	if a.ceremonyBound(req, "unguessable-challenge") {
		t.Fatal("accepted without browser binding")
	}
	req.AddCookie(w.Result().Cookies()[0])
	if !a.ceremonyBound(req, "unguessable-challenge") {
		t.Fatal("binding rejected")
	}
	if a.ceremonyBound(req, "another-challenge") {
		t.Fatal("different binding accepted")
	}
}
