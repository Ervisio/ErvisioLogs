//go:build integration

package api

import (
	"testing"
)

func TestAdminUserManagement(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice, bob := e.newUser(admin, "alice"), e.newUser(admin, "bob")
	adminID := id(expect(t, admin.get("/auth/me"), 200), "user", "id")
	aliceID := id(expect(t, alice.get("/auth/me"), 200), "user", "id")
	bobID := id(expect(t, bob.get("/auth/me"), 200), "user", "id")

	// Search and pagination.
	expect(t, alice.get("/admin/users"), 403)
	page := expect(t, admin.get("/admin/users?limit=2"), 200).JSON()
	if len(page["items"].([]any)) != 2 || page["next_cursor"] == nil {
		t.Fatalf("first page %v", page)
	}
	page = expect(t, admin.get("/admin/users?limit=2&cursor="+page["next_cursor"].(string)), 200).JSON()
	if items := page["items"].([]any); len(items) != 1 || items[0].(map[string]any)["username"] != "bob" || page["next_cursor"] != nil {
		t.Fatalf("last page %v", page)
	}
	if items := expect(t, admin.get("/admin/users?q=ALI"), 200).JSON()["items"].([]any); len(items) != 1 {
		t.Fatalf("search %v", items)
	}
	if items := expect(t, admin.get("/admin/users?q=%25"), 200).JSON()["items"].([]any); len(items) != 0 {
		t.Fatalf("LIKE wildcard not escaped: %v", items)
	}
	expect(t, admin.get("/admin/users?cursor=nope"), 400)
	expect(t, admin.get("/admin/users/"+aliceID), 200)

	// Promote alice; the last active admin cannot be demoted, disabled or deleted.
	expect(t, admin.patch("/admin/users/"+adminID, map[string]any{"is_instance_admin": false}), 422)
	expect(t, admin.patch("/admin/users/"+aliceID, map[string]any{}), 422)
	expect(t, admin.patch("/admin/users/"+aliceID, map[string]any{"is_instance_admin": true}), 204)
	expect(t, alice.get("/admin/users"), 200)
	expect(t, alice.patch("/admin/users/"+adminID, map[string]any{"is_instance_admin": false}), 204)
	expect(t, admin.get("/admin/users"), 403)
	expect(t, admin.patch("/admin/users/"+aliceID, map[string]any{"status": "disabled"}), 403)
	// alice is now the only admin, and nobody changes their own standing.
	expect(t, alice.patch("/admin/users/"+aliceID, map[string]any{"status": "disabled"}), 422)

	// Password reset revokes every session of the target.
	expect(t, alice.put("/admin/users/"+bobID+"/password", map[string]string{"password": "short"}), 422)
	expect(t, alice.put("/admin/users/"+bobID+"/password", map[string]string{"password": "bob-new-password-2"}), 204)
	expect(t, bob.get("/auth/me"), 401)
	bob = e.newClient()
	expect(t, bob.post("/auth/login", map[string]string{"login": "bob", "password": "bob-password-1"}), 401)
	expect(t, bob.adopt(bob.post("/auth/login", map[string]string{"login": "bob", "password": "bob-new-password-2"})), 200)

	// Sessions: list and revoke one, then all.
	sessions := expect(t, alice.get("/admin/users/"+bobID+"/sessions"), 200).JSON()["items"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("sessions %v", sessions)
	}
	s := sessions[0].(map[string]any)
	if s["current"] != false || s["ip"] == "" || s["last_seen_at"] == nil {
		t.Fatalf("session %v", s)
	}
	expect(t, alice.delete("/admin/users/"+aliceID+"/sessions/"+s["id"].(string), nil), 404)
	expect(t, alice.delete("/admin/users/"+bobID+"/sessions/"+s["id"].(string), nil), 204)
	expect(t, bob.get("/auth/me"), 401)
	expect(t, bob.adopt(bob.post("/auth/login", map[string]string{"login": "bob", "password": "bob-new-password-2"})), 200)
	if n := expect(t, alice.delete("/admin/users/"+bobID+"/sessions", nil), 200).JSON()["revoked"]; n != float64(1) {
		t.Fatalf("revoked %v", n)
	}
	expect(t, bob.get("/auth/me"), 401)

	// Delete: refused for the only owner of a workspace and for oneself.
	bob = e.newClient()
	expect(t, bob.adopt(bob.post("/auth/login", map[string]string{"login": "bob", "password": "bob-new-password-2"})), 200)
	ws := id(expect(t, bob.post("/workspaces", map[string]string{"name": "Bob"}), 201), "id")
	expect(t, alice.delete("/admin/users/"+aliceID, nil), 422)
	expect(t, alice.delete("/admin/users/"+bobID, nil), 409)
	expect(t, bob.delete("/workspaces/"+ws, map[string]string{"confirm": "bob"}), 204)
	expect(t, alice.delete("/admin/users/"+bobID, nil), 204)
	expect(t, alice.get("/admin/users/"+bobID), 404)
	expect(t, bob.get("/auth/me"), 401)
}

func TestAccountSelfService(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	other := e.newClient()
	expect(t, other.adopt(other.post("/auth/login", map[string]string{"login": "alice", "password": "alice-password-1"})), 200)

	sessions := expect(t, alice.get("/auth/sessions"), 200).JSON()["items"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("sessions %v", sessions)
	}
	current := 0
	for _, s := range sessions {
		if s.(map[string]any)["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("exactly one current session expected: %v", sessions)
	}

	// Changing the password needs the current one and keeps only this session.
	expect(t, alice.put("/auth/password", map[string]string{"current_password": "wrong", "new_password": "alice-new-password-2"}), 403)
	expect(t, alice.put("/auth/password", map[string]string{"current_password": "alice-password-1", "new_password": "alice"}), 422)
	expect(t, alice.put("/auth/password", map[string]string{"current_password": "alice-password-1", "new_password": "alice-new-password-2"}), 204)
	expect(t, alice.get("/auth/me"), 200)
	expect(t, other.get("/auth/me"), 401)

	// Revoke the others, then the current one, which also drops the cookie.
	expect(t, other.adopt(other.post("/auth/login", map[string]string{"login": "alice", "password": "alice-new-password-2"})), 200)
	if n := expect(t, alice.delete("/auth/sessions", nil), 200).JSON()["revoked"]; n != float64(1) {
		t.Fatalf("revoked %v", n)
	}
	expect(t, other.get("/auth/me"), 401)
	sessions = expect(t, alice.get("/auth/sessions"), 200).JSON()["items"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("sessions after revoke %v", sessions)
	}
	expect(t, admin.delete("/auth/sessions/"+sessions[0].(map[string]any)["id"].(string), nil), 404)
	r := expect(t, alice.delete("/auth/sessions/"+sessions[0].(map[string]any)["id"].(string), nil), 204)
	if !has([]byte(r.Header.Get("Set-Cookie")), "Max-Age=0") {
		t.Fatalf("cookie not cleared: %q", r.Header.Get("Set-Cookie"))
	}
	expect(t, alice.get("/auth/me"), 401)
	expect(t, alice.post("/auth/logout", nil), 204)
}
