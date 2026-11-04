//go:build integration

package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// socketClient serves the API on a unix socket wrapped by Socket, as main does, and returns a client
// dialing it. It has no cookies and no CSRF token.
func (e *env) socketClient() *client {
	// t.TempDir can exceed the unix socket path limit on macOS.
	dir, err := os.MkdirTemp("", "evl")
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "admin.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		e.t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(Socket(e.srv.Config.Handler))
	_ = srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	e.t.Cleanup(srv.Close)
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", path)
	}}
	return &client{t: e.t, base: "http://unix", http: &http.Client{Transport: tr, Timeout: 10 * time.Second}}
}

// noLogAccess checks that the caller cannot read, export or tail the project's logs, nor use the
// log-reading features built on them.
func noLogAccess(t *testing.T, c *client, tok, project string) {
	t.Helper()
	hdr := map[string]string{}
	if tok != "" {
		hdr["Authorization"] = "Bearer " + tok
	}
	for _, rq := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/logs/search", map[string]any{"project_id": project}},
		{"POST", "/logs/export", map[string]any{"project_id": project}},
		{"GET", "/logs/tail?project_id=" + project, nil},
		{"GET", "/queries?project_id=" + project, nil},
		{"GET", "/alerts?project_id=" + project, nil},
	} {
		if r := c.do(rq.method, rq.path, rq.body, hdr); r.Status != http.StatusUnauthorized {
			t.Errorf("%s %s: %d %s", rq.method, rq.path, r.Status, r.Body)
		}
	}
}

func auditHas(t *testing.T, c *client, ws, action, actor string) {
	t.Helper()
	for _, it := range expect(t, c.get("/workspaces/"+ws+"/audit?limit=200"), 200).JSON()["items"].([]any) {
		ev := it.(map[string]any)
		if ev["action"] == action && ev["actor_name"] == actor && ev["actor_user_id"] == nil {
			return
		}
	}
	t.Errorf("no %s by %q in the audit of %s", action, actor, ws)
}

func TestAdminSocket(t *testing.T) {
	e := newEnv(t, nil, nil, func(a *API) { a.Logs = &Logs{} })
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	carol := e.newUser(admin, "carol")
	aliceID := id(expect(t, alice.get("/auth/me"), 200), "user", "id")
	sock := e.socketClient()

	expect(t, sock.get("/admin/users"), 200)
	expect(t, sock.get("/admin/settings"), 200)
	expect(t, sock.post("/workspaces", map[string]any{"name": "Plugin"}), 422)
	ws := id(expect(t, sock.post("/workspaces", map[string]any{"name": "Plugin", "owner_id": aliceID}), 201), "id")
	if !has(expect(t, alice.get("/workspaces"), 200).Body, ws) {
		t.Fatal("the owner does not see the workspace")
	}
	expect(t, sock.get("/workspaces/"+ws), 200)
	expect(t, sock.post("/workspaces/"+ws+"/members", map[string]any{"email": "carol@example.com", "role": "viewer"}), 201)
	proj := id(expect(t, sock.post("/projects", map[string]any{"workspace_id": ws, "name": "Game"}), 201), "id")

	// Tokens it issues may only ingest.
	expect(t, sock.post("/tokens", map[string]any{"project_id": proj, "name": "read", "scopes": []string{"logs:read"}}), 403)
	expect(t, sock.post("/tokens", map[string]any{"project_id": proj, "name": "both", "scopes": []string{"logs:ingest", "logs:export"}}), 403)
	ingest := expect(t, sock.post("/tokens", map[string]any{"project_id": proj, "name": "ingest", "scopes": []string{"logs:ingest"}}), 201).JSON()["token"].(string)
	cur := expect(t, carol.bearer("GET", "/tokens/current", ingest), 200).JSON()
	if sc, _ := cur["scopes"].([]any); len(sc) != 1 || sc[0] != "logs:ingest" {
		t.Fatalf("scopes %v", cur["scopes"])
	}

	auditHas(t, sock, ws, "workspace.create", "plugin")
	auditHas(t, alice, ws, "member.add", "plugin")

	// It is not a user and holds no log access.
	expect(t, sock.get("/auth/me"), 401)
	expect(t, sock.get("/admin/tokens"), 401)
	expect(t, sock.post("/admin/tokens", map[string]any{"name": "x", "scopes": []string{"admin:read"},
		"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)}), 401)
	noLogAccess(t, sock, "", proj)

	// Membership rules are unchanged for users.
	other := id(expect(t, sock.post("/workspaces", map[string]any{"name": "Other", "owner_id": aliceID}), 201), "id")
	expect(t, carol.get("/workspaces/"+other), 404)
	expect(t, sock.delete("/workspaces/"+other, map[string]any{"confirm": "other"}), 204)
}

func TestAdminTokens(t *testing.T) {
	e := newEnv(t, nil, nil, func(a *API) { a.Logs = &Logs{} })
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	carol := e.newUser(admin, "carol")
	aliceID := id(expect(t, alice.get("/auth/me"), 200), "user", "id")
	in := func(d time.Duration) string { return time.Now().Add(d).Format(time.RFC3339) }
	create := func(c *client, name string, scopes []string, exp string) resp {
		return c.post("/admin/tokens", map[string]any{"name": name, "scopes": scopes, "expires_at": exp})
	}

	for _, bad := range []map[string]any{
		{"name": "x", "scopes": []string{"admin:read"}},
		{"name": "x", "scopes": []string{"admin:read"}, "expires_at": in(400 * 24 * time.Hour)},
		{"name": "x", "scopes": []string{"admin:read"}, "expires_at": in(-time.Minute)},
		{"name": "x", "scopes": []string{"logs:read"}, "expires_at": in(time.Hour)},
		{"name": "x", "scopes": []string{"admin:read", "admin:read"}, "expires_at": in(time.Hour)},
		{"name": "x", "scopes": []string{}, "expires_at": in(time.Hour)},
		{"name": "", "scopes": []string{"admin:read"}, "expires_at": in(time.Hour)},
	} {
		if r := admin.post("/admin/tokens", bad); r.Status != 422 {
			t.Errorf("%v: %d %s", bad, r.Status, r.Body)
		}
	}
	expect(t, create(alice, "x", []string{"admin:read"}, in(time.Hour)), 403)

	created := expect(t, create(admin, "reader", []string{"admin:read"}, in(24*time.Hour)), 201)
	reader := created.JSON()["token"].(string)
	if created.Header.Get("Cache-Control") != "no-store" || id(created, "admin_token", "prefix") == "" {
		t.Fatalf("create: %v %s", created.Header, created.Body)
	}
	readerID := id(created, "admin_token", "id")
	list := expect(t, admin.get("/admin/tokens"), 200)
	if !has(list.Body, readerID) || has(list.Body, reader) {
		t.Fatalf("list: %s", list.Body)
	}
	if !has(expect(t, admin.get("/admin/audit"), 200).Body, "admin_token.create") {
		t.Error("creation not audited")
	}

	// admin:read reads everything it may manage and changes nothing.
	expect(t, alice.bearer("GET", "/admin/users", reader), 200)
	expect(t, alice.bearer("GET", "/workspaces", reader), 200)
	expect(t, alice.bearerJSON("PATCH", "/admin/users/"+aliceID, reader, map[string]any{"status": "disabled"}), 403)
	expect(t, alice.bearerJSON("POST", "/workspaces", reader, map[string]any{"name": "No", "owner_id": aliceID}), 403)
	// A bearer token cannot manage admin tokens, whatever it is.
	expect(t, alice.bearer("GET", "/admin/tokens", reader), 401)
	expect(t, alice.do("POST", "/admin/tokens", map[string]any{"name": "x", "scopes": []string{"admin:read"}, "expires_at": in(time.Hour)},
		map[string]string{"Authorization": "Bearer " + reader}), 401)
	// It is never sent as a cookie.
	expect(t, e.newClient().do("GET", "/admin/users", nil, map[string]string{"Cookie": SessionCookie + "=" + reader}), 401)

	tenancy := expect(t, create(admin, "provisioner", []string{"admin:tenancy"}, in(time.Hour)), 201).JSON()["token"].(string)
	expect(t, alice.bearer("GET", "/admin/users", tenancy), 403)
	expect(t, alice.post("/workspaces", map[string]any{"name": "Mine", "owner_id": aliceID}), 422)
	ws := id(expect(t, alice.bearerJSON("POST", "/workspaces", tenancy, map[string]any{"name": "Provisioned", "owner_id": aliceID}), 201), "id")
	expect(t, alice.get("/workspaces/"+ws), 200)
	expect(t, carol.get("/workspaces/"+ws), 404)
	proj := id(expect(t, alice.bearerJSON("POST", "/projects", tenancy, map[string]any{"workspace_id": ws, "name": "Game"}), 201), "id")
	expect(t, alice.bearerJSON("POST", "/tokens", tenancy, map[string]any{"project_id": proj, "name": "r", "scopes": []string{"logs:read"}}), 403)
	auditHas(t, alice, ws, "workspace.create", "provisioner")
	auditHas(t, alice, ws, "project.create", "provisioner")
	noLogAccess(t, alice, tenancy, proj)
	noLogAccess(t, alice, reader, proj)

	// Revocation, expiry and the creator's status all end it at once.
	expect(t, admin.delete("/admin/tokens/"+readerID, nil), 204)
	expect(t, admin.delete("/admin/tokens/"+readerID, nil), 204)
	expect(t, alice.bearer("GET", "/admin/users", reader), 401)
	if !has(expect(t, admin.get("/admin/tokens"), 200).Body, `"revoked_at":"`) {
		t.Error("revocation not listed")
	}

	expiring := expect(t, create(admin, "expiring", []string{"admin:read"}, in(time.Hour)), 201).JSON()["token"].(string)
	expect(t, alice.bearer("GET", "/admin/users", expiring), 200)
	if err := e.exec(`UPDATE admin_tokens SET expires_at = now() - interval '1 second' WHERE name = 'expiring'`); err != nil {
		t.Fatal(err)
	}
	expect(t, alice.bearer("GET", "/admin/users", expiring), 401)

	bob := e.newUser(admin, "bob")
	bobID := id(expect(t, bob.get("/auth/me"), 200), "user", "id")
	expect(t, admin.patch("/admin/users/"+bobID, map[string]any{"is_instance_admin": true}), 204)
	bobs := expect(t, create(bob, "bobs", []string{"admin:read"}, in(time.Hour)), 201).JSON()["token"].(string)
	expect(t, alice.bearer("GET", "/admin/users", bobs), 200)
	expect(t, admin.patch("/admin/users/"+bobID, map[string]any{"is_instance_admin": false}), 204)
	expect(t, alice.bearer("GET", "/admin/users", bobs), 401)
	expect(t, admin.patch("/admin/users/"+bobID, map[string]any{"is_instance_admin": true}), 204)
	expect(t, alice.bearer("GET", "/admin/users", bobs), 200)
	expect(t, admin.patch("/admin/users/"+bobID, map[string]any{"status": "disabled"}), 204)
	expect(t, alice.bearer("GET", "/admin/users", bobs), 401)
	expect(t, admin.patch("/admin/users/"+bobID, map[string]any{"status": "active"}), 204)
	expect(t, alice.bearer("GET", "/admin/users", bobs), 200)
	expect(t, admin.delete("/admin/users/"+bobID, nil), 204)
	expect(t, alice.bearer("GET", "/admin/users", bobs), 401)
}

func TestAdminTokenCORS(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	aliceID := id(expect(t, e.newUser(admin, "alice").get("/auth/me"), 200), "user", "id")
	tok := expect(t, admin.post("/admin/tokens", map[string]any{"name": "plugin page", "scopes": []string{"admin:tenancy", "admin:users"},
		"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)}), 201).JSON()["token"].(string)
	page := e.newClient()
	evil := "https://plugin.example"

	pre := expect(t, page.do("OPTIONS", "/admin/users", nil, map[string]string{"Origin": evil, "Access-Control-Request-Method": "PATCH"}), 204)
	if pre.Header.Get("Access-Control-Allow-Origin") != "*" || pre.Header.Get("Access-Control-Allow-Headers") != "Authorization, Content-Type" ||
		pre.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("preflight %v", pre.Header)
	}
	got := expect(t, page.do("GET", "/admin/users", nil, map[string]string{"Origin": evil, "Authorization": "Bearer " + tok}), 200)
	if got.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("GET %v", got.Header)
	}
	expect(t, page.do("POST", "/workspaces", map[string]any{"name": "Remote", "owner_id": aliceID},
		map[string]string{"Origin": evil, "Authorization": "Bearer " + tok}), 201)

	// Cookie requests keep the Origin and CSRF checks; auth and log routes get no CORS headers.
	expect(t, admin.do("POST", "/workspaces", map[string]any{"name": "Cross"}, map[string]string{"Origin": evil}), 403)
	expect(t, admin.do("POST", "/workspaces", map[string]any{"name": "NoCSRF"}, map[string]string{CSRFHeader: ""}), 403)
	expect(t, admin.post("/workspaces", map[string]any{"name": "Same"}), 201)
	if r := page.do("OPTIONS", "/auth/login", nil, map[string]string{"Origin": evil, "Access-Control-Request-Method": "POST"}); r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("auth preflight %d %v", r.Status, r.Header)
	}
}
