//go:build integration

package api

import (
	"testing"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

func TestScopedRetentionIsolationAndPermissions(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	owner := e.newUser(admin, "retowner")
	other := e.newUser(admin, "retother")
	developer := e.newUser(admin, "retdev")
	viewer := e.newUser(admin, "retviewer")
	workspace := id(expect(t, owner.post("/workspaces", map[string]string{"name": "Owner workspace"}), 201), "id")
	otherWorkspace := id(expect(t, other.post("/workspaces", map[string]string{"name": "Other workspace"}), 201), "id")
	project := id(expect(t, owner.post("/projects", map[string]string{"workspace_id": workspace, "name": "Project"}), 201), "id")
	sibling := id(expect(t, owner.post("/projects", map[string]string{"workspace_id": workspace, "name": "Sibling"}), 201), "id")
	for _, member := range []struct{ email, role string }{{"retdev@example.com", "developer"}, {"retviewer@example.com", "viewer"}} {
		expect(t, owner.post("/workspaces/"+workspace+"/members", map[string]string{"email": member.email, "role": member.role}), 201)
	}
	instance := "/admin/retention/instance/" + store.InstanceScope.String()
	expect(t, admin.put(instance, map[string]any{"local_age_hours": 24, "global_age_hours": 720, "cache_max_bytes": 1024 * 1024, "high_pct": 90, "low_pct": 70}), 200)
	for _, path := range []string{"/workspaces/" + workspace + "/retention", "/projects/" + project + "/retention", "/projects/" + sibling + "/retention"} {
		expect(t, owner.put(path, map[string]int{"local_age_hours": 48, "global_age_hours": 960}), 200)
	}
	expect(t, other.put("/workspaces/"+otherWorkspace+"/retention", map[string]int{"local_age_hours": 72}), 200)
	for _, path := range []string{"/workspaces/" + workspace + "/retention", "/projects/" + project + "/retention"} {
		expect(t, other.get(path), 404)
		expect(t, other.put(path, map[string]int{"local_age_hours": 1}), 404)
		expect(t, other.delete(path, nil), 404)
		expect(t, admin.get(path), 404)
		expect(t, viewer.get(path), 200)
		expect(t, viewer.put(path, map[string]int{"local_age_hours": 1}), 403)
		expect(t, viewer.delete(path, nil), 403)
	}
	expect(t, developer.put("/workspaces/"+workspace+"/retention", map[string]int{"local_age_hours": 1}), 403)
	expect(t, developer.put("/projects/"+project+"/retention", map[string]int{"local_age_hours": 36}), 200)
	response := expect(t, viewer.get("/projects/"+project+"/retention"), 200)
	rows := response.JSON()["items"].([]any)
	if len(rows) != 3 {
		t.Fatalf("expected three visible ancestor/own policies, got %s", response.Body)
	}
	for _, row := range rows {
		policy := row.(map[string]any)
		if policy["scope_id"] == sibling || policy["scope_id"] == otherWorkspace {
			t.Fatalf("leaked another scope: %v", policy)
		}
		for _, key := range []string{"cache_max_bytes", "high_pct", "low_pct", "updated_by"} {
			if policy[key] != nil {
				t.Fatalf("leaked %s: %v", key, policy)
			}
		}
	}
	expect(t, owner.delete("/projects/"+project+"/retention", nil), 204)
	expect(t, owner.get("/projects/"+sibling+"/retention"), 200)
	rows = expect(t, owner.get("/workspaces/"+workspace+"/retention"), 200).JSON()["items"].([]any)
	if len(rows) != 2 {
		t.Fatalf("workspace should see only instance and workspace: %v", rows)
	}
}

func TestScopedRetentionValidation(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	owner := e.newUser(admin, "retvalidate")
	workspace := id(expect(t, owner.post("/workspaces", map[string]string{"name": "Validation"}), 201), "id")
	path := "/workspaces/" + workspace + "/retention"
	for _, body := range []map[string]any{
		{"local_age_hours": 0}, {"local_age_hours": 87841}, {"global_age_hours": -1},
		{"local_age_hours": 48, "global_age_hours": 24},
		{"cache_max_bytes": 1024}, {"high_pct": 90},
	} {
		expect(t, owner.put(path, body), 422)
	}
	expect(t, owner.put(path, map[string]any{"local_age_hours": 1.5}), 400)
	expect(t, owner.put(path, map[string]any{"unknown": true}), 400)
	expect(t, owner.put(path, map[string]any{"local_age_hours": nil, "global_age_hours": nil}), 422)
	expect(t, owner.put(path, map[string]any{"local_age_hours": 24, "global_age_hours": nil}), 200)
	expect(t, owner.delete(path, nil), 204)
}
