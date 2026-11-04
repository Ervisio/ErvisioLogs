//go:build integration

package api

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
)

func TestFirstRunSetup(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.newClient()

	prov := expect(t, c.get("/auth/providers"), 200).JSON()
	if prov["setup_required"] != true || prov["open_registration"] != false {
		t.Fatalf("providers: %v", prov)
	}
	body := map[string]string{"email": "a@example.com", "username": "alice", "password": "alice-password-1"}

	expect(t, c.post("/auth/register", body), 403) // no setup code
	body["setup_code"] = "wrong"
	expect(t, c.post("/auth/register", body), 403)

	// Concurrent attempts with the right code: exactly one account may be created.
	var wg sync.WaitGroup
	codes := make([]int, 5)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cc := e.newClient()
			codes[i] = cc.post("/auth/register", map[string]string{"email": fmt.Sprintf("u%d@example.com", i),
				"username": fmt.Sprintf("user%d", i), "password": "long-enough-pw", "setup_code": e.cfg.SetupCode}).Status
		}()
	}
	wg.Wait()
	created := 0
	for _, s := range codes {
		if s == 201 {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly one first admin, statuses %v", codes)
	}
	var admins int
	_ = e.queryRow(`SELECT count(*) FROM users WHERE is_instance_admin`).Scan(&admins)
	if admins != 1 {
		t.Fatalf("admins: %d", admins)
	}
	// Setup is over: the code no longer works and registration is closed.
	body["setup_code"] = e.cfg.SetupCode
	expect(t, c.post("/auth/register", body), 403)
}

func TestLocalAuth(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()

	me := expect(t, admin.get("/auth/me"), 200).JSON()
	if u := me["user"].(map[string]any); u["is_instance_admin"] != true || u["email"] != "admin@example.com" {
		t.Fatalf("me: %v", me)
	}
	if has(expect(t, admin.get("/auth/me"), 200).Body, "password") {
		t.Fatal("password material in /auth/me")
	}

	// Login by email and by username; wrong and unknown accounts give the same answer.
	for _, login := range []string{"admin@example.com", "ADMIN"} {
		c := e.newClient()
		expect(t, c.post("/auth/login", map[string]string{"login": login, "password": "admin-password-1"}), 200)
	}
	c := e.newClient()
	wrong := expect(t, c.post("/auth/login", map[string]string{"login": "admin", "password": "nope-nope-nope"}), 401)
	unknown := expect(t, c.post("/auth/login", map[string]string{"login": "ghost", "password": "nope-nope-nope"}), 401)
	if string(wrong.JSON()["detail"].(string)) != unknown.JSON()["detail"].(string) {
		t.Fatal("login errors reveal whether the account exists")
	}

	// Cookie flags.
	r := c.post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"})
	sc := r.Header.Get("Set-Cookie")
	if !strings.Contains(sc, "HttpOnly") || !strings.Contains(sc, "SameSite=Lax") {
		t.Fatalf("cookie: %s", sc)
	}

	// Logout invalidates the server-side session, not just the cookie.
	expect(t, admin.post("/auth/logout", nil), 204)
	expect(t, admin.get("/auth/me"), 401)
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.setupAdmin()
	c := e.newClient()
	var last resp
	for i := 0; i < 12; i++ {
		last = c.post("/auth/login", map[string]string{"login": "admin", "password": "wrong-wrong-1"})
	}
	if last.Status != 429 || last.Header.Get("Retry-After") == "" {
		t.Fatalf("expected 429 with Retry-After, got %d", last.Status)
	}
	// Even the right password is refused while throttled.
	expect(t, c.post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}), 429)
}

func TestRegistrationValidationAndOpenMode(t *testing.T) {
	e := newEnv(t, func(c *config.Auth) { c.OpenRegistration = true }, nil)
	e.setupAdmin()
	c := e.newClient()
	cases := []map[string]string{
		{"email": "bad", "username": "bobby", "password": "long-enough-pw"},
		{"email": "b@example.com", "username": "Bo", "password": "long-enough-pw"},
		{"email": "b@example.com", "username": "bobby", "password": "short"},
	}
	for _, body := range cases {
		expect(t, c.post("/auth/register", body), 422)
	}
	ok := map[string]string{"email": "Bob@Example.com", "username": "Bobby", "password": "long-enough-pw"}
	expect(t, c.adopt(c.post("/auth/register", ok)), 201)
	expect(t, e.newClient().post("/auth/register", ok), 409)
	// Open registration never grants admin.
	if me := expect(t, c.get("/auth/me"), 200).JSON()["user"].(map[string]any); me["is_instance_admin"] != false || me["email"] != "bob@example.com" {
		t.Fatalf("me: %v", me)
	}
	expect(t, c.get("/admin/users"), 403)
}

func TestCSRFAndOrigin(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	ws := map[string]string{"name": "Acme"}

	csrf := admin.csrf
	admin.csrf = ""
	expect(t, admin.post("/workspaces", ws), 403) // cookie but no token
	admin.csrf = "wrong-token"
	expect(t, admin.post("/workspaces", ws), 403)
	admin.csrf = csrf
	r := admin.do("POST", "/workspaces", ws, map[string]string{"Origin": "http://evil.example"})
	expect(t, r, 403)
	expect(t, admin.do("POST", "/workspaces", ws, map[string]string{"Origin": "http://app.test"}), 201)
	// Anonymous cross-origin login is refused too.
	expect(t, e.newClient().do("POST", "/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"},
		map[string]string{"Origin": "http://evil.example"}), 403)
}

func TestDisabledUserLosesSessions(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	bob := e.newUser(admin, "bobby")
	expect(t, bob.get("/auth/me"), 200)
	uid := id(expect(t, bob.get("/auth/me"), 200), "user", "id")
	expect(t, admin.patch("/admin/users/"+uid, map[string]string{"status": "disabled"}), 204)
	expect(t, bob.get("/auth/me"), 401)
	expect(t, e.newClient().post("/auth/login", map[string]string{"login": "bobby", "password": "bobby-password-1"}), 401)
	expect(t, admin.patch("/admin/users/"+id(admin.get("/auth/me"), "user", "id"), map[string]string{"status": "disabled"}), 422)
}

func TestTenantIsolation(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice, mallory := e.newUser(admin, "alice"), e.newUser(admin, "mallory")

	wsA := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Alice Co"}), 201), "id")
	projA := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": wsA, "name": "Game"}), 201), "id")
	tokA := expect(t, alice.post("/tokens", map[string]any{"project_id": projA, "name": "ingest", "scopes": []string{"logs:ingest"}}), 201)
	tokID := id(tokA, "api_token", "id")
	envs := expect(t, alice.get("/projects/"+projA+"/environments"), 200).JSON()["items"].([]any)
	streams := expect(t, alice.get("/projects/"+projA+"/streams"), 200).JSON()["items"].([]any)
	envID, streamID := envs[0].(map[string]any)["id"].(string), streams[0].(map[string]any)["id"].(string)

	wsM := id(expect(t, mallory.post("/workspaces", map[string]string{"name": "Mallory Co"}), 201), "id")
	expect(t, mallory.post("/projects", map[string]string{"workspace_id": wsM, "name": "Mine"}), 201)

	// Every access by Mallory to Alice's resources must look exactly like a missing resource (404).
	probes := []struct{ method, path string }{
		{"GET", "/workspaces/" + wsA}, {"PATCH", "/workspaces/" + wsA}, {"DELETE", "/workspaces/" + wsA},
		{"GET", "/workspaces/" + wsA + "/members"}, {"POST", "/workspaces/" + wsA + "/members"},
		{"GET", "/workspaces/" + wsA + "/audit"},
		{"GET", "/projects?workspace_id=" + wsA}, {"GET", "/projects/" + projA}, {"PATCH", "/projects/" + projA},
		{"DELETE", "/projects/" + projA},
		{"GET", "/projects/" + projA + "/environments"}, {"POST", "/projects/" + projA + "/environments"},
		{"PATCH", "/projects/" + projA + "/environments/" + envID}, {"DELETE", "/projects/" + projA + "/environments/" + envID},
		{"GET", "/projects/" + projA + "/streams"}, {"POST", "/projects/" + projA + "/streams"},
		{"PATCH", "/projects/" + projA + "/streams/" + streamID}, {"DELETE", "/projects/" + projA + "/streams/" + streamID},
		{"GET", "/tokens?project_id=" + projA}, {"DELETE", "/tokens/" + tokID},
	}
	missing := mallory.get("/workspaces/" + "00000000-0000-7000-8000-000000000000")
	for _, p := range probes {
		var body any
		if p.method == "POST" || p.method == "PATCH" || p.method == "DELETE" {
			body = map[string]any{"name": "x", "email": "alice@example.com", "role": "viewer", "confirm": "x"}
		}
		got := mallory.do(p.method, p.path, body, nil)
		if got.Status != 404 {
			t.Errorf("%s %s: status %d, want 404 (%s)", p.method, p.path, got.Status, got.Body)
		}
	}
	if missing.Status != 404 {
		t.Fatalf("missing workspace: %d", missing.Status)
	}
	// Body-supplied identifiers are checked too.
	expect(t, mallory.post("/projects", map[string]any{"workspace_id": wsA, "name": "evil"}), 404)
	expect(t, mallory.post("/tokens", map[string]any{"project_id": projA, "name": "evil", "scopes": []string{"logs:read"}}), 404)

	// Nothing leaked into listings.
	for _, path := range []string{"/workspaces", "/projects?workspace_id=" + wsM} {
		if b := expect(t, mallory.get(path), 200).Body; has(b, wsA) || has(b, projA) {
			t.Fatalf("%s leaks Alice's data: %s", path, b)
		}
	}
	// The instance admin has no automatic access to tenant data.
	expect(t, admin.get("/workspaces/"+wsA), 404)
	expect(t, admin.get("/projects/"+projA), 404)
	// And Alice still has everything.
	expect(t, alice.get("/projects/"+projA), 200)
}

func TestSchemaRejectsCrossWorkspaceReferences(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	wsA := id(expect(t, alice.post("/workspaces", map[string]string{"name": "A"}), 201), "id")
	wsB := id(expect(t, alice.post("/workspaces", map[string]string{"name": "B"}), 201), "id")
	projA := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": wsA, "name": "P"}), 201), "id")
	// Even a buggy caller cannot attach a child of workspace A's project to workspace B.
	err := e.exec(`INSERT INTO streams (id, workspace_id, project_id, name) VALUES (gen_random_uuid(), $1, $2, 'x')`, wsB, projA)
	if err == nil {
		t.Fatal("composite foreign key did not reject a cross-workspace stream")
	}
	err = e.exec(`INSERT INTO api_tokens (id, workspace_id, project_id, name, prefix, hash, scopes) VALUES (gen_random_uuid(), $1, $2, 'x', 'evl_x', '\x01', '{logs:read}')`, wsB, projA)
	if err == nil {
		t.Fatal("composite foreign key did not reject a cross-workspace token")
	}
}

func TestRBAC(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	owner := e.newUser(admin, "owner1")
	roles := map[string]*client{}
	for _, r := range []string{"admin", "developer", "analyst", "viewer"} {
		roles[r] = e.newUser(admin, "u"+r)
	}
	ws := id(expect(t, owner.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	for r := range roles {
		expect(t, owner.post("/workspaces/"+ws+"/members", map[string]string{"email": "u" + r + "@example.com", "role": r}), 201)
	}
	proj := id(expect(t, owner.post("/projects", map[string]string{"workspace_id": ws, "name": "P"}), 201), "id")

	want := []struct {
		role, method, path string
		body               any
		status             int
	}{
		{"viewer", "GET", "/projects/" + proj, nil, 200},
		{"viewer", "POST", "/projects", map[string]any{"workspace_id": ws, "name": "x"}, 403},
		{"viewer", "GET", "/tokens?project_id=" + proj, nil, 403},
		{"viewer", "GET", "/workspaces/" + ws + "/audit", nil, 403},
		{"analyst", "POST", "/tokens", map[string]any{"project_id": proj, "name": "t", "scopes": []string{"logs:read"}}, 403},
		{"developer", "POST", "/projects", map[string]any{"workspace_id": ws, "name": "dev project"}, 201},
		{"developer", "POST", "/tokens", map[string]any{"project_id": proj, "name": "t", "scopes": []string{"logs:ingest"}}, 201},
		{"developer", "POST", "/workspaces/" + ws + "/members", map[string]any{"email": "owner1@example.com", "role": "viewer"}, 403},
		{"developer", "PATCH", "/workspaces/" + ws, map[string]any{"name": "renamed"}, 403},
		{"admin", "POST", "/workspaces/" + ws + "/members", map[string]any{"email": "admin@example.com", "role": "viewer"}, 201},
		{"admin", "POST", "/workspaces/" + ws + "/members", map[string]any{"email": "uviewer@example.com", "role": "owner"}, 403}, // no escalation
		{"admin", "PATCH", "/workspaces/" + ws, map[string]any{"name": "renamed"}, 403},
		{"admin", "GET", "/workspaces/" + ws + "/audit", nil, 200},
	}
	for _, tc := range want {
		got := roles[tc.role].do(tc.method, tc.path, tc.body, nil)
		if got.Status != tc.status {
			t.Errorf("%s %s %s: %d, want %d (%s)", tc.role, tc.method, tc.path, got.Status, tc.status, got.Body)
		}
	}
	expect(t, owner.patch("/workspaces/"+ws, map[string]any{"name": "renamed"}), 200)

	// A token cannot carry a scope its creator lacks, nor an unknown one.
	expect(t, roles["developer"].post("/tokens", map[string]any{"project_id": proj, "name": "t", "scopes": []string{"logs:delete"}}), 422)
	expect(t, roles["developer"].post("/tokens", map[string]any{"project_id": proj, "name": "t", "scopes": []string{}}), 422)
}

func TestStreamAndEnvironmentMetadata(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	owner, viewer := e.newUser(admin, "owner1"), e.newUser(admin, "uviewer")
	ws := id(expect(t, owner.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	expect(t, owner.post("/workspaces/"+ws+"/members", map[string]string{"email": "uviewer@example.com", "role": "viewer"}), 201)
	proj := id(expect(t, owner.post("/projects", map[string]string{"workspace_id": ws, "name": "P"}), 201), "id")

	s := expect(t, owner.post("/projects/"+proj+"/streams", map[string]any{
		"name": "economy", "display_name": " Economia ", "description": "Soldi e transazioni", "color": "#22C55E"}), 201).JSON()
	if s["display_name"] != "Economia" || s["color"] != "#22c55e" || s["description"] != "Soldi e transazioni" {
		t.Fatalf("created stream %v", s)
	}
	sid := s["id"].(string)
	path := "/projects/" + proj + "/streams/" + sid
	expect(t, owner.patch(path, map[string]any{"color": "green"}), 422)
	expect(t, owner.patch(path, map[string]any{"display_name": strings.Repeat("x", 101)}), 422)
	expect(t, owner.patch(path, map[string]any{"name": "renamed"}), 400)
	expect(t, viewer.patch(path, map[string]any{"color": ""}), 403)
	expect(t, owner.patch("/projects/"+proj+"/streams/00000000-0000-7000-8000-000000000000", map[string]any{}), 404)

	// Absent fields stay, empty strings clear.
	s = expect(t, owner.patch(path, map[string]any{"color": ""}), 200).JSON()
	if s["color"] != "" || s["display_name"] != "Economia" || s["name"] != "economy" {
		t.Fatalf("patched stream %v", s)
	}
	items := expect(t, owner.get("/projects/"+proj+"/streams"), 200).JSON()["items"].([]any)
	if last := items[len(items)-1].(map[string]any); last["display_name"] != "Economia" || last["color"] != "" {
		t.Fatalf("listed stream %v", last)
	}

	envs := expect(t, owner.get("/projects/"+proj+"/environments"), 200).JSON()["items"].([]any)
	envPath := "/projects/" + proj + "/environments/" + envs[0].(map[string]any)["id"].(string)
	env := expect(t, owner.patch(envPath, map[string]any{"display_name": "Produzione", "color": "#ef4444"}), 200).JSON()
	if env["name"] != "production" || env["display_name"] != "Produzione" || env["color"] != "#ef4444" || env["description"] != "" {
		t.Fatalf("patched environment %v", env)
	}
	expect(t, owner.post("/projects/"+proj+"/environments", map[string]any{"name": "staging", "description": strings.Repeat("x", 501)}), 422)
}

func TestLastOwnerProtection(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	owner, other := e.newUser(admin, "owner1"), e.newUser(admin, "other")
	ws := id(expect(t, owner.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	ownerID := id(expect(t, owner.get("/auth/me"), 200), "user", "id")
	otherID := id(expect(t, other.get("/auth/me"), 200), "user", "id")

	expect(t, owner.delete("/workspaces/"+ws+"/members/"+ownerID, nil), 409)
	expect(t, owner.patch("/workspaces/"+ws+"/members/"+ownerID, map[string]string{"role": "viewer"}), 409)
	expect(t, owner.post("/workspaces/"+ws+"/members", map[string]string{"email": "other@example.com", "role": "owner"}), 201)
	expect(t, owner.patch("/workspaces/"+ws+"/members/"+ownerID, map[string]string{"role": "viewer"}), 204)
	// "other" is now the only owner and cannot leave.
	expect(t, other.delete("/workspaces/"+ws+"/members/"+otherID, nil), 409)
}

func TestWorkspaceDeleteNeedsConfirmation(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	w := expect(t, alice.post("/workspaces", map[string]string{"name": "Doomed Project"}), 201).JSON()
	if w["slug"] != "doomed-project" {
		t.Fatalf("slug: %v", w["slug"])
	}
	ws := w["id"].(string)
	expect(t, alice.delete("/workspaces/"+ws, map[string]string{"confirm": "nope"}), 422)
	expect(t, alice.delete("/workspaces/"+ws, map[string]string{"confirm": "doomed-project"}), 204)
	expect(t, alice.get("/workspaces/"+ws), 404)
}

func TestAPITokenLifecycle(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "P"}), 201), "id")

	created := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "ci", "scopes": []string{"logs:ingest", "logs:read"}}), 201)
	tok, tokID := created.JSON()["token"].(string), id(created, "api_token", "id")
	if !strings.HasPrefix(tok, "evl_") || created.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("token %q cache %q", tok, created.Header.Get("Cache-Control"))
	}

	// The plaintext appears in the creation response only.
	if b := expect(t, alice.get("/tokens?project_id="+proj), 200).Body; has(b, tok) || !has(b, "evl_") {
		t.Fatalf("listing must show the prefix but never the token: %s", b)
	}
	// And it is not stored: only a keyed hash.
	var stored []byte
	_ = e.queryRow(`SELECT hash FROM api_tokens WHERE id=$1`, tokID).Scan(&stored)
	if len(stored) != 32 || strings.Contains(string(stored), tok) {
		t.Fatal("token stored in the clear or hash malformed")
	}

	anon := e.newClient()
	cur := expect(t, anon.bearer("GET", "/tokens/current", tok), 200).JSON()
	if cur["project_id"] != proj || fmt.Sprint(cur["scopes"]) != "[logs:ingest logs:read]" {
		t.Fatalf("current: %v", cur)
	}
	// A token is not a user: it cannot reach user routes or manage tokens.
	expect(t, anon.bearer("GET", "/workspaces", tok), 401)
	expect(t, anon.bearer("DELETE", "/tokens/"+tokID, tok), 401)
	// Malformed and unknown tokens are refused.
	expect(t, anon.bearer("GET", "/tokens/current", "evl_"+strings.Repeat("A", 43)), 401)
	expect(t, anon.bearer("GET", "/tokens/current", "garbage"), 401)

	// Revocation takes effect immediately and is idempotent.
	expect(t, alice.delete("/tokens/"+tokID, nil), 204)
	expect(t, anon.bearer("GET", "/tokens/current", tok), 401)
	expect(t, alice.delete("/tokens/"+tokID, nil), 204)

	// Expiry.
	t2 := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "short", "scopes": []string{"logs:read"},
		"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)}), 201).JSON()["token"].(string)
	expect(t, anon.bearer("GET", "/tokens/current", t2), 200)
	if err := e.exec(`UPDATE api_tokens SET expires_at = now() - interval '1 second' WHERE prefix = $1`, t2[:10]); err != nil {
		t.Fatal(err)
	}
	expect(t, anon.bearer("GET", "/tokens/current", t2), 401)
	expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "past", "scopes": []string{"logs:read"},
		"expires_at": time.Now().Add(-time.Hour).Format(time.RFC3339)}), 422)

	// Deleting the project kills its tokens. Loss of the creator's standing: see TestTokenFollowsCreator.
	t3 := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "p", "scopes": []string{"logs:read"}}), 201).JSON()["token"].(string)
	expect(t, alice.delete("/projects/"+proj, map[string]string{"confirm": "p"}), 204)
	expect(t, anon.bearer("GET", "/tokens/current", t3), 401)
}

func TestAuditTrail(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "P"}), 201), "id")
	tk := expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": "ci", "scopes": []string{"logs:read"}}), 201)
	expect(t, alice.delete("/tokens/"+id(tk, "api_token", "id"), nil), 204)

	page1 := expect(t, alice.get("/workspaces/"+ws+"/audit?limit=2"), 200).JSON()
	if len(page1["items"].([]any)) != 2 || page1["next_cursor"] == nil {
		t.Fatalf("page1: %v", page1)
	}
	page2 := expect(t, alice.get("/workspaces/"+ws+"/audit?limit=2&cursor="+page1["next_cursor"].(string)), 200).JSON()
	all := fmt.Sprint(page1["items"], page2["items"])
	for _, a := range []string{"token.revoke", "token.create", "project.create", "workspace.create"} {
		if !strings.Contains(all, a) {
			t.Errorf("audit lacks %s: %s", a, all)
		}
	}
	if strings.Contains(all, tk.JSON()["token"].(string)) {
		t.Fatal("token secret in audit log")
	}
	expect(t, alice.get("/workspaces/"+ws+"/audit?cursor=garbage"), 400)
}

func TestStrictJSONInput(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	expect(t, admin.post("/workspaces", map[string]any{"name": "x", "unknown_field": 1}), 400)
	expect(t, admin.do("POST", "/workspaces", nil, nil), 400)
	big := strings.Repeat("a", 70<<10)
	expect(t, admin.post("/workspaces", map[string]any{"name": big}), 413)
	expect(t, admin.get("/workspaces/not-a-uuid"), 404)
}
