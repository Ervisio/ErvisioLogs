//go:build integration

package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
)

// ---------- OAuth browser binding and identities ----------

func TestOAuthCallbackIsBoundToTheBrowser(t *testing.T) {
	e := newIDPEnv(t, true)
	e.setupAdmin()
	claims := map[string]any{"sub": "s1", "email": "csrf@example.com", "email_verified": true}
	victim := e.newClient()
	state, nonce, ch := e.start(victim, "google")
	code := e.idp.issue(fakeGrant{claims: claims, nonce: nonce, challenge: ch})

	// The URL prepared by one browser does not work in another, with no cookie or with a wrong one.
	other := e.newClient()
	if loc := e.callback(other, "google", state, code); loc != "/login?error=invalid_state" {
		t.Fatalf("no cookie: %q", loc)
	}
	expect(t, other.get("/auth/me"), 401)
	if loc := e.callbackWith("wrong", "google", state, code); loc != "/login?error=invalid_state" {
		t.Fatalf("wrong cookie: %q", loc)
	}
	// Those attempts did not consume the state: the browser that started the flow still completes it.
	if loc := e.callback(victim, "google", state, code); loc != "/" {
		t.Fatalf("right browser: %q", loc)
	}
	expect(t, victim.get("/auth/me"), 200)
	if binding(victim) != "" {
		t.Fatal("the OAuth cookie survives the callback")
	}

	// A newer flow in the same browser supersedes the older one.
	b := e.newClient()
	old, oldNonce, oldCh := e.start(b, "google")
	e.start(b, "google")
	if loc := e.callback(b, "google", old, e.idp.issue(fakeGrant{claims: claims, nonce: oldNonce, challenge: oldCh})); loc != "/login?error=invalid_state" {
		t.Fatalf("old flow: %q", loc)
	}
}

func TestOAuthCookieAttributes(t *testing.T) {
	check := func(t *testing.T, e *idpEnv, secure bool) {
		t.Helper()
		r := expect(t, e.newClient().get("/auth/oauth/google/start"), 302)
		sc := r.Header.Get("Set-Cookie")
		for _, want := range []string{oauthStateCookie + "=", "HttpOnly", "SameSite=Lax", "Path=/api/v1/auth/oauth", "Max-Age=600"} {
			if !strings.Contains(sc, want) {
				t.Errorf("cookie lacks %s: %s", want, sc)
			}
		}
		if strings.Contains(sc, "Secure") != secure {
			t.Errorf("Secure should be %v: %s", secure, sc)
		}
		loc := r.Header.Get("Location")
		value := strings.TrimPrefix(strings.SplitN(sc, ";", 2)[0], oauthStateCookie+"=")
		if value == "" || strings.Contains(loc, value) {
			t.Errorf("the cookie must hold a digest, not the state: %s", sc)
		}
		cleared := e.newClient().get("/auth/oauth/google/callback?error=denied").Header.Get("Set-Cookie")
		if !strings.Contains(cleared, "Max-Age=0") || !strings.Contains(cleared, "Path=/api/v1/auth/oauth") {
			t.Errorf("callback does not clear the cookie: %s", cleared)
		}
	}
	t.Run("http", func(t *testing.T) { check(t, newIDPEnv(t, true), false) })
	t.Run("https", func(t *testing.T) { check(t, newIDPEnv(t, true, func(a *API) { a.secure = true }), true) })
}

func TestIdentityConflictAndDisabledLinking(t *testing.T) {
	e := newIDPEnv(t, true)
	admin := e.setupAdmin()
	first := e.newClient()
	if loc := e.oidcLogin(first, "google", map[string]any{"sub": "sub-a", "email": "two@example.com", "email_verified": true}); loc != "/" {
		t.Fatalf("first: %q", loc)
	}
	uid := id(expect(t, first.get("/auth/me"), 200), "user", "id")

	// Same provider, another subject, same address: refused, whatever the provider's email says.
	c := e.newClient()
	if loc := e.oidcLogin(c, "google", map[string]any{"sub": "sub-b", "email": "two@example.com", "email_verified": true}); loc != "/login?error=identity_conflict" {
		t.Fatalf("conflict: %q", loc)
	}
	expect(t, c.get("/auth/me"), 401)
	if n := e.identities("two@example.com"); n != 1 {
		t.Fatalf("identities: %d", n)
	}
	// The schema refuses it too.
	if err := e.exec(`INSERT INTO identities (user_id, provider, subject) VALUES ($1, 'google', 'sub-c')`, uid); err == nil {
		t.Fatal("no UNIQUE (user_id, provider)")
	}
	// A different provider is not a conflict, and the first identity still signs in.
	if loc := e.oidcLogin(e.newClient(), "cognito", map[string]any{"sub": "sub-d", "email": "two@example.com", "email_verified": true}); loc != "/" {
		t.Fatalf("other provider: %q", loc)
	}
	if loc := e.oidcLogin(e.newClient(), "google", map[string]any{"sub": "sub-a", "email": "two@example.com", "email_verified": true}); loc != "/" {
		t.Fatalf("first identity: %q", loc)
	}

	// A disabled account gets no new identity.
	u := expect(t, admin.post("/admin/users", map[string]string{"email": "off@example.com", "username": "offline"}), 201)
	expect(t, admin.patch("/admin/users/"+id(u, "id"), map[string]string{"status": "disabled"}), 204)
	if loc := e.oidcLogin(e.newClient(), "google", map[string]any{"sub": "sub-off", "email": "off@example.com", "email_verified": true}); loc != "/login?error=account_disabled" {
		t.Fatalf("disabled: %q", loc)
	}
	if n := e.identities("off@example.com"); n != 0 {
		t.Fatalf("a disabled account was linked: %d", n)
	}
}

func TestVerifiedEmailLinksAcrossProviders(t *testing.T) {
	e := newIDPEnv(t, true)
	e.setupAdmin()
	c := e.newClient()
	if loc := e.oidcLogin(c, "google", map[string]any{"sub": "g1", "email": "both@example.com", "email_verified": true}); loc != "/" {
		t.Fatalf("google: %q", loc)
	}
	me := expect(t, c.get("/auth/me"), 200).JSON()["user"].(map[string]any)
	if me["email_verified"] != true {
		t.Fatalf("OAuth account not verified: %v", me)
	}
	d := e.newClient()
	state, _, ch := e.start(d, "discord")
	code := e.discord.issue(fakeGrant{challenge: ch}, map[string]any{"id": "d1", "username": "both", "email": "both@example.com", "verified": true})
	if loc := e.callback(d, "discord", state, code); loc != "/" {
		t.Fatalf("discord: %q", loc)
	}
	if got := id(expect(t, d.get("/auth/me"), 200), "user", "id"); got != me["id"] {
		t.Fatalf("another account: %s, want %v", got, me["id"])
	}
	if n := e.identities("both@example.com"); n != 2 {
		t.Fatalf("identities: %d", n)
	}
}

func TestCognitoStringEmailVerified(t *testing.T) {
	e := newIDPEnv(t, true)
	e.setupAdmin()
	if loc := e.oidcLogin(e.newClient(), "cognito", map[string]any{"sub": "c1", "email": "no@example.com", "email_verified": "false"}); loc != "/login?error=email_unverified" {
		t.Fatalf("false: %q", loc)
	}
	c := e.newClient()
	if loc := e.oidcLogin(c, "cognito", map[string]any{"sub": "c2", "email": "yes@example.com", "email_verified": "true"}); loc != "/" {
		t.Fatalf("true: %q", loc)
	}
	expect(t, c.get("/auth/me"), 200)
}

// ---------- API tokens follow their creator ----------

func TestTokenFollowsCreator(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	owner, dev := e.newUser(admin, "owner1"), e.newUser(admin, "devvy")
	devID := id(expect(t, dev.get("/auth/me"), 200), "user", "id")
	ws := id(expect(t, owner.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	proj := id(expect(t, owner.post("/projects", map[string]string{"workspace_id": ws, "name": "P"}), 201), "id")
	expect(t, owner.post("/workspaces/"+ws+"/members", map[string]string{"email": "devvy@example.com", "role": "developer"}), 201)
	mint := func(scopes ...string) string {
		return expect(t, dev.post("/tokens", map[string]any{"project_id": proj, "name": "t", "scopes": scopes}), 201).JSON()["token"].(string)
	}
	both, ingest := mint("logs:ingest", "logs:read"), mint("logs:ingest")
	anon := e.newClient()
	scopes := func(tok string) string {
		return fmt.Sprint(expect(t, anon.bearer("GET", "/tokens/current", tok), 200).JSON()["scopes"])
	}
	role := func(r string) {
		expect(t, owner.patch("/workspaces/"+ws+"/members/"+devID, map[string]string{"role": r}), 204)
	}
	if got := scopes(both); got != "[logs:ingest logs:read]" {
		t.Fatalf("scopes: %s", got)
	}

	// Demotion narrows at once, and a token left with nothing stops working.
	role("viewer")
	if got := scopes(both); got != "[logs:read]" {
		t.Fatalf("demoted: %s", got)
	}
	expect(t, anon.bearer("GET", "/tokens/current", ingest), 401)
	role("developer")
	if got := scopes(both); got != "[logs:ingest logs:read]" {
		t.Fatalf("promoted: %s", got)
	}
	scopes(ingest)

	// Removal: refused while out, nothing destroyed.
	expect(t, owner.delete("/workspaces/"+ws+"/members/"+devID, nil), 204)
	expect(t, anon.bearer("GET", "/tokens/current", both), 401)
	expect(t, owner.post("/workspaces/"+ws+"/members", map[string]string{"email": "devvy@example.com", "role": "developer"}), 201)
	scopes(both)

	// Disabling the creator.
	expect(t, admin.patch("/admin/users/"+devID, map[string]string{"status": "disabled"}), 204)
	expect(t, anon.bearer("GET", "/tokens/current", both), 401)
	expect(t, admin.patch("/admin/users/"+devID, map[string]string{"status": "active"}), 204)
	scopes(both)

	var revoked int
	_ = e.queryRow(`SELECT count(*) FROM api_tokens WHERE revoked_at IS NOT NULL`).Scan(&revoked)
	if revoked != 0 {
		t.Fatalf("%d tokens were revoked", revoked)
	}
}

// ---------- login limits ----------

func trustProxy(a *API) { a.TrustProxy = true }

func loginFrom(c *client, ip, login, password string) resp {
	return c.do("POST", "/auth/login", map[string]string{"login": login, "password": password}, map[string]string{"X-Forwarded-For": ip})
}

func TestLoginLimitCannotLockOutFromAnotherIP(t *testing.T) {
	e := newEnv(t, nil, nil, trustProxy)
	e.setupAdmin()
	c := e.newClient()
	for i := 0; i < 10; i++ {
		expect(t, loginFrom(c, "198.51.100.7", "admin", "wrong-wrong-1"), 401)
	}
	expect(t, loginFrom(c, "198.51.100.7", "admin", "admin-password-1"), 429)
	expect(t, loginFrom(e.newClient(), "203.0.113.9", "admin", "admin-password-1"), 200)
}

func TestLoginCountersAreCanonical(t *testing.T) {
	e := newEnv(t, nil, nil, trustProxy)
	e.setupAdmin()
	c := e.newClient()
	// Email and username of one account share a counter.
	for i := 0; i < 5; i++ {
		expect(t, loginFrom(c, "198.51.100.1", "admin", "wrong-wrong-1"), 401)
		expect(t, loginFrom(c, "198.51.100.1", "ADMIN@example.com", "wrong-wrong-1"), 401)
	}
	expect(t, loginFrom(c, "198.51.100.1", "admin@example.com", "wrong-wrong-1"), 429)
	// An unknown login has one counter whatever its case.
	for i := 0; i < 5; i++ {
		expect(t, loginFrom(c, "198.51.100.2", "Ghost", "wrong-wrong-1"), 401)
		expect(t, loginFrom(c, "198.51.100.2", "ghost", "wrong-wrong-1"), 401)
	}
	expect(t, loginFrom(c, "198.51.100.2", "GHOST", "wrong-wrong-1"), 429)
}

func TestLoginGlobalAccountLimit(t *testing.T) {
	e := newEnv(t, nil, nil, trustProxy)
	admin := e.setupAdmin()
	expect(t, admin.post("/admin/users", map[string]string{"email": "vic@example.com", "username": "victim", "password": "victim-password-1"}), 201)
	c := e.newClient()
	// One IP feeds the global counter only up to its own limit, however long it keeps trying.
	for i := 0; i < 25; i++ {
		want := 401
		if i >= 10 {
			want = 429
		}
		expect(t, loginFrom(c, "192.0.2.200", "victim", "wrong-wrong-1"), want)
	}
	for ip := 1; ip <= 9; ip++ {
		for i := 0; i < 10; i++ {
			expect(t, loginFrom(c, fmt.Sprintf("192.0.2.%d", ip), "victim", "wrong-wrong-1"), 401)
		}
	}
	// A distributed attack reached the threshold: even the right password from a new IP waits.
	expect(t, loginFrom(c, "203.0.113.50", "victim", "victim-password-1"), 429)
	// Other accounts are unaffected.
	expect(t, loginFrom(e.newClient(), "203.0.113.50", "admin", "admin-password-1"), 200)
}

func TestLoginLimitAggregatesIPv6(t *testing.T) {
	e := newEnv(t, nil, nil, trustProxy)
	e.setupAdmin()
	c := e.newClient()
	for i := 1; i <= 10; i++ {
		expect(t, loginFrom(c, fmt.Sprintf("2001:db8:1:2::%x", i), "admin", "wrong-wrong-1"), 401)
	}
	expect(t, loginFrom(c, "2001:db8:1:2:ffff::1", "admin", "wrong-wrong-1"), 429)
	expect(t, loginFrom(c, "2001:db8:1:3::1", "admin", "admin-password-1"), 200)
}

// ---------- audit ----------

func auditCount(e *env, action string) int {
	var n int
	_ = e.queryRow(`SELECT count(*) FROM audit_events WHERE action = $1`, action).Scan(&n)
	return n
}

func TestOAuthCallbackIsRateLimitedWithoutAudit(t *testing.T) {
	e := newIDPEnv(t, true)
	c := e.newClient()
	for i := 0; i < 30; i++ {
		expect(t, c.get("/auth/oauth/google/callback?state=x&code=y"), 303)
	}
	if n := auditCount(e.env, "auth.oauth_failed"); n != 30 {
		t.Fatalf("audit rows: %d", n)
	}
	r := expect(t, c.get("/auth/oauth/google/callback?state=x&code=y"), 429)
	if r.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	if n := auditCount(e.env, "auth.oauth_failed"); n != 30 {
		t.Fatalf("a blocked callback wrote audit: %d", n)
	}
}

func TestBlockedLoginWritesNoAudit(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.setupAdmin()
	c := e.newClient()
	for i := 0; i < 15; i++ {
		c.post("/auth/login", map[string]string{"login": "admin", "password": "wrong-wrong-1"})
	}
	if n := auditCount(e, "auth.login_failed"); n != 10 {
		t.Fatalf("login_failed rows: %d", n)
	}
	var n int
	_ = e.queryRow(`SELECT count(*) FROM audit_events WHERE action LIKE '%throttled%'`).Scan(&n)
	if n != 0 {
		t.Fatalf("throttled rows: %d", n)
	}
}

func TestWorkspaceDeleteAuditFollowsSuccess(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Doomed"}), 201), "id")
	if err := e.exec(`CREATE FUNCTION refuse_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'no'; END $$;
		CREATE TRIGGER refuse BEFORE DELETE ON workspaces FOR EACH ROW EXECUTE FUNCTION refuse_delete()`); err != nil {
		t.Fatal(err)
	}
	expect(t, alice.delete("/workspaces/"+ws, map[string]string{"confirm": "doomed"}), 500)
	if n := auditCount(e, "workspace.delete"); n != 0 {
		t.Fatal("workspace.delete recorded for a deletion that failed")
	}
	if err := e.exec(`DROP TRIGGER refuse ON workspaces`); err != nil {
		t.Fatal(err)
	}
	expect(t, alice.delete("/workspaces/"+ws, map[string]string{"confirm": "doomed"}), 204)
	b := expect(t, admin.get("/admin/audit"), 200).Body
	if !has(b, "workspace.delete") || !has(b, ws) {
		t.Fatalf("instance audit lacks the deletion: %s", b)
	}
}

func TestAdminAudit(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	expect(t, e.newClient().get("/admin/audit"), 401)
	expect(t, alice.get("/admin/audit"), 403)
	expect(t, admin.get("/admin/audit?cursor=garbage"), 400)

	// Failed logins name a useful target and no secret.
	aliceID := id(expect(t, alice.get("/auth/me"), 200), "user", "id")
	c := e.newClient()
	expect(t, c.post("/auth/login", map[string]string{"login": "alice", "password": "secret-typed-password"}), 401)
	expect(t, c.post("/auth/login", map[string]string{"login": "secret-pasted-in-login", "password": "secret-typed-password"}), 401)
	var rows []map[string]any
	for _, ev := range expect(t, admin.get("/admin/audit?limit=200"), 200).JSON()["items"].([]any) {
		if m := ev.(map[string]any); m["action"] == "auth.login_failed" {
			rows = append(rows, m)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("failed logins: %v", rows)
	}
	if rows[0]["target_type"] != "login" || rows[0]["target_id"] == "" || rows[1]["target_type"] != "user" || rows[1]["target_id"] != aliceID {
		t.Fatalf("targets: %v", rows)
	}
	if b := expect(t, admin.get("/admin/audit?limit=200"), 200).Body; has(b, "secret-") {
		t.Fatalf("secret in audit: %s", b)
	}
	walkAudit(t, admin, "/admin/audit")
}

// walkAudit pages through a trail with every page size, including the one equal to its length.
func walkAudit(t *testing.T, c *client, path string) {
	t.Helper()
	all := expect(t, c.get(path+"?limit=200"), 200).JSON()
	total := len(all["items"].([]any))
	if total < 2 || all["next_cursor"] != nil {
		t.Fatalf("trail: %v", all)
	}
	for limit := 1; limit <= total+1; limit++ {
		seen, cursor, pages := map[string]bool{}, "", 0
		for {
			p := path + fmt.Sprintf("?limit=%d", limit)
			if cursor != "" {
				p += "&cursor=" + cursor
			}
			page := expect(t, c.get(p), 200).JSON()
			for _, ev := range page["items"].([]any) {
				k := ev.(map[string]any)["id"].(string)
				if seen[k] {
					t.Fatalf("limit %d: duplicate %s", limit, k)
				}
				seen[k] = true
			}
			pages++
			next, _ := page["next_cursor"].(string)
			if next == "" {
				break
			}
			cursor = next
		}
		if len(seen) != total {
			t.Fatalf("limit %d: %d events, want %d", limit, len(seen), total)
		}
		wantPages := (total + limit - 1) / limit
		if pages != wantPages {
			t.Fatalf("limit %d: %d pages, want %d (no empty last page)", limit, pages, wantPages)
		}
	}
}

func TestAuditPaginationEndsWithoutDuplicates(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	for i := 0; i < 5; i++ {
		expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": fmt.Sprintf("P%d", i)}), 201)
	}
	walkAudit(t, alice, "/workspaces/"+ws+"/audit")
	// Equal timestamps are ordered by id, so a page boundary between them loses nothing.
	if err := e.exec(`UPDATE audit_events SET created_at = now() WHERE workspace_id = $1`, ws); err != nil {
		t.Fatal(err)
	}
	walkAudit(t, alice, "/workspaces/"+ws+"/audit")
}

// ---------- role changes re-checked under the lock ----------

func TestMemberChangesRecheckRolesUnderLock(t *testing.T) {
	const blocked = `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`
	cases := []struct {
		name          string
		change        func(admin *client, path string) resp // what the workspace admin attempts on the target
		actor, target string                                // roles they get while it waits
	}{
		{"patch, caller demoted", func(c *client, p string) resp { return c.patch(p, map[string]string{"role": "analyst"}) }, "viewer", "viewer"},
		{"patch, target promoted", func(c *client, p string) resp { return c.patch(p, map[string]string{"role": "analyst"}) }, "admin", "owner"},
		{"delete, caller demoted", func(c *client, p string) resp { return c.delete(p, nil) }, "viewer", "viewer"},
		{"delete, target promoted", func(c *client, p string) resp { return c.delete(p, nil) }, "admin", "owner"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			admin := e.setupAdmin()
			owner, actor, target := e.newUser(admin, "owner1"), e.newUser(admin, "actor"), e.newUser(admin, "target")
			ws := id(expect(t, owner.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
			expect(t, owner.post("/workspaces/"+ws+"/members", map[string]string{"email": "actor@example.com", "role": "admin"}), 201)
			expect(t, owner.post("/workspaces/"+ws+"/members", map[string]string{"email": "target@example.com", "role": "viewer"}), 201)
			actorID := id(expect(t, actor.get("/auth/me"), 200), "user", "id")
			targetID := id(expect(t, target.get("/auth/me"), 200), "user", "id")

			ctx := context.Background()
			tx, err := e.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, ws); err != nil {
				t.Fatal(err)
			}
			done := make(chan resp, 1)
			go func() { done <- tc.change(actor, "/workspaces/"+ws+"/members/"+targetID) }()
			// The request passed its first access check and now waits for the lock.
			deadline := time.Now().Add(5 * time.Second)
			for {
				var n int
				if err := e.queryRow(blocked).Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the request never waited for the lock")
				}
				time.Sleep(5 * time.Millisecond)
			}
			for uid, role := range map[string]string{actorID: tc.actor, targetID: tc.target} {
				if _, err := tx.Exec(ctx, `UPDATE workspace_members SET role_id = (SELECT id FROM roles WHERE workspace_id = $1 AND name = $2)
					WHERE workspace_id = $1 AND user_id = $3`, ws, role, uid); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			expect(t, <-done, 403)
			var got string
			if err := e.queryRow(`SELECT r.name FROM workspace_members m JOIN roles r ON r.id = m.role_id WHERE m.workspace_id = $1 AND m.user_id = $2`, ws, targetID).Scan(&got); err != nil {
				t.Fatalf("target's membership: %v", err)
			}
			if got != tc.target {
				t.Fatalf("target role %q, want %q", got, tc.target)
			}
		})
	}
}

func TestWorkspaceAdminCannotTouchOwner(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	owner, wsAdmin := e.newUser(admin, "owner1"), e.newUser(admin, "wsadmin")
	ownerID := id(expect(t, owner.get("/auth/me"), 200), "user", "id")
	ws := id(expect(t, owner.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	expect(t, owner.post("/workspaces/"+ws+"/members", map[string]string{"email": "wsadmin@example.com", "role": "admin"}), 201)
	// A second owner, so only permissions can refuse this and not the last-owner rule.
	second := e.newUser(admin, "owner2")
	expect(t, owner.post("/workspaces/"+ws+"/members", map[string]string{"email": "owner2@example.com", "role": "owner"}), 201)
	expect(t, wsAdmin.patch("/workspaces/"+ws+"/members/"+ownerID, map[string]string{"role": "viewer"}), 403)
	expect(t, wsAdmin.delete("/workspaces/"+ws+"/members/"+ownerID, nil), 403)
	expect(t, owner.get("/workspaces/"+ws+"/members"), 200)
	expect(t, second.get("/workspaces/"+ws), 200)
}

func TestLastOwnerConcurrency(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	a, b := e.newUser(admin, "ownera"), e.newUser(admin, "ownerb")
	aID := id(expect(t, a.get("/auth/me"), 200), "user", "id")
	bID := id(expect(t, b.get("/auth/me"), 200), "user", "id")
	demote := map[string]string{"role": "viewer"}
	modes := []struct {
		name         string
		first, other func(ws string) resp
	}{
		{"both leave",
			func(ws string) resp { return a.delete("/workspaces/"+ws+"/members/"+aID, nil) },
			func(ws string) resp { return b.delete("/workspaces/"+ws+"/members/"+bID, nil) }},
		{"remove each other",
			func(ws string) resp { return a.delete("/workspaces/"+ws+"/members/"+bID, nil) },
			func(ws string) resp { return b.delete("/workspaces/"+ws+"/members/"+aID, nil) }},
		{"demote each other",
			func(ws string) resp { return a.patch("/workspaces/"+ws+"/members/"+bID, demote) },
			func(ws string) resp { return b.patch("/workspaces/"+ws+"/members/"+aID, demote) }},
		{"both demote themselves",
			func(ws string) resp { return a.patch("/workspaces/"+ws+"/members/"+aID, demote) },
			func(ws string) resp { return b.patch("/workspaces/"+ws+"/members/"+bID, demote) }},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			for round := 0; round < 8; round++ {
				ws := id(expect(t, a.post("/workspaces", map[string]string{"name": fmt.Sprintf("%s %d", m.name, round)}), 201), "id")
				expect(t, a.post("/workspaces/"+ws+"/members", map[string]string{"email": "ownerb@example.com", "role": "owner"}), 201)
				var wg sync.WaitGroup
				start := make(chan struct{})
				got := make([]int, 2)
				for i, f := range []func(string) resp{m.first, m.other} {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						got[i] = f(ws).Status
					}()
				}
				close(start)
				wg.Wait()
				ok := 0
				for _, s := range got {
					if s == 204 {
						ok++
					} else if s >= 500 {
						t.Fatalf("round %d: statuses %v", round, got)
					}
				}
				var owners int
				if err := e.queryRow(`SELECT count(*) FROM workspace_members m JOIN roles r ON r.id = m.role_id
					WHERE m.workspace_id = $1 AND r.name = 'owner'`, ws).Scan(&owners); err != nil {
					t.Fatal(err)
				}
				if ok != 1 || owners != 1 {
					t.Fatalf("round %d: statuses %v, owners %d", round, got, owners)
				}
				if err := e.exec(`DELETE FROM workspaces WHERE id = $1`, ws); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// ---------- tenants ----------

func TestBearerOfOneTenantOnAnother(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice, mallory := e.newUser(admin, "alice"), e.newUser(admin, "mallory")
	wsA := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Alice Co"}), 201), "id")
	projA := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": wsA, "name": "Game"}), 201), "id")
	wsM := id(expect(t, mallory.post("/workspaces", map[string]string{"name": "Mallory Co"}), 201), "id")
	projM := id(expect(t, mallory.post("/projects", map[string]string{"workspace_id": wsM, "name": "Mine"}), 201), "id")
	tokM := expect(t, mallory.post("/tokens", map[string]any{"project_id": projM, "name": "m", "scopes": []string{"logs:read"}}), 201).JSON()["token"].(string)

	anon := e.newClient()
	for _, p := range []struct{ method, path string }{
		{"GET", "/workspaces/" + wsA}, {"GET", "/workspaces/" + wsA + "/members"}, {"GET", "/workspaces/" + wsA + "/audit"},
		{"GET", "/projects/" + projA}, {"GET", "/projects/" + projA + "/environments"}, {"GET", "/projects/" + projA + "/streams"},
		{"GET", "/tokens?project_id=" + projA}, {"DELETE", "/projects/" + projA}, {"DELETE", "/workspaces/" + wsA},
	} {
		r := anon.bearer(p.method, p.path, tokM)
		if r.Status != 401 || has(r.Body, "Alice Co") || has(r.Body, "Game") {
			t.Errorf("%s %s: %d %s", p.method, p.path, r.Status, r.Body)
		}
	}
	// The token describes only its own project.
	cur := expect(t, anon.bearer("GET", "/tokens/current", tokM), 200)
	if cur.JSON()["project_id"] != projM || has(cur.Body, projA) || has(cur.Body, wsA) {
		t.Fatalf("current: %s", cur.Body)
	}
	expect(t, alice.get("/projects/"+projA), 200)
}

func TestChildResourcesOfAnotherProject(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	one := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "One"}), 201), "id")
	two := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "Two"}), 201), "id")
	for _, kind := range []string{"environments", "streams"} {
		list := func() []any { return expect(t, alice.get("/projects/"+two+"/"+kind), 200).JSON()["items"].([]any) }
		before := list()
		if len(before) == 0 {
			t.Fatalf("%s: project has none", kind)
		}
		victim := before[0].(map[string]any)["id"].(string)
		expect(t, alice.delete("/projects/"+one+"/"+kind+"/"+victim, nil), 404)
		if after := list(); len(after) != len(before) {
			t.Fatalf("%s: deleted through another project", kind)
		}
	}
}

// ---------- overload, housekeeping, hostile text ----------

func TestHasherSaturationAnswers503(t *testing.T) {
	saturable := auth.NewHasherWithQueue(auth.Params{Memory: 8, Time: 1, Threads: 1}, 1, 0)
	e := newEnv(t, func(c *config.Auth) { c.OpenRegistration = true }, nil, func(a *API) { a.Hasher = saturable })
	admin := e.setupAdmin()
	release, err := saturable.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	check := func(r resp) {
		t.Helper()
		expect(t, r, 503)
		if r.Header.Get("Retry-After") == "" {
			t.Fatal("no Retry-After")
		}
	}
	check(e.newClient().post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}))
	check(e.newClient().post("/auth/login", map[string]string{"login": "nobody", "password": "whatever-it-is"}))
	check(e.newClient().post("/auth/register", map[string]string{"email": "n@example.com", "username": "newone", "password": "long-enough-pw"}))
	check(admin.post("/admin/users", map[string]string{"email": "m@example.com", "username": "newtwo", "password": "long-enough-pw"}))
	var n int
	_ = e.queryRow(`SELECT count(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Fatalf("users: %d", n)
	}
	if got := auditCount(e, "auth.login_failed"); got != 0 {
		t.Fatalf("overload recorded as %d failed logins", got)
	}
}

func TestExpiredRowsArePurged(t *testing.T) {
	e := newIDPEnv(t, true)
	admin := e.setupAdmin()
	gone := e.newUser(admin, "gone")
	goneID := id(expect(t, gone.get("/auth/me"), 200), "user", "id")
	e.start(e.newClient(), "google") // stays live
	e.start(e.newClient(), "google") // will expire
	if err := e.exec(`UPDATE oauth_states SET expires_at = now() - interval '1 second' WHERE state_hash = (SELECT state_hash FROM oauth_states LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	if err := e.exec(`UPDATE sessions SET expires_at = now() - interval '1 second' WHERE user_id = $1`, goneID); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		var n int
		if err := e.queryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Within the interval nothing runs; once it has passed the next sign-in cleans up.
	expect(t, e.newClient().post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}), 200)
	if count(`SELECT count(*) FROM sessions WHERE expires_at < now()`) != 1 {
		t.Fatal("purged within the interval")
	}
	e.api.lastPurge.Store(0)
	expect(t, e.newClient().post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}), 200)
	if n := count(`SELECT count(*) FROM sessions WHERE expires_at < now()`); n != 0 {
		t.Fatalf("expired sessions left: %d", n)
	}
	if n := count(`SELECT count(*) FROM oauth_states WHERE expires_at < now()`); n != 0 {
		t.Fatalf("expired states left: %d", n)
	}
	if n := count(`SELECT count(*) FROM oauth_states`); n != 1 {
		t.Fatalf("live states: %d, want 1", n)
	}
	expect(t, admin.get("/auth/me"), 200)
	// The purge also runs when a flow starts.
	if err := e.exec(`UPDATE oauth_states SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	e.api.lastPurge.Store(0)
	e.start(e.newClient(), "google")
	if n := count(`SELECT count(*) FROM oauth_states`); n != 1 {
		t.Fatalf("states after start: %d, want 1", n)
	}
}

func TestNULCharactersAreRefused(t *testing.T) {
	const nul = "a\x00b"
	e := newIDPEnv(t, true)
	admin := e.setupAdmin()
	alice := e.newUser(admin, "alice")
	ws := id(expect(t, alice.post("/workspaces", map[string]string{"name": "Team"}), 201), "id")
	proj := id(expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": "P"}), 201), "id")

	expect(t, e.newClient().post("/auth/login", map[string]string{"login": nul, "password": "whatever-it-is"}), 401)
	expect(t, e.newClient().post("/auth/login", map[string]string{"login": "alice", "password": nul}), 401)
	expect(t, e.newClient().post("/auth/register", map[string]string{"email": nul + "@example.com", "username": "newone", "password": "long-enough-pw"}), 422)
	expect(t, e.newClient().post("/auth/register", map[string]string{"email": "n@example.com", "username": nul + "xx", "password": "long-enough-pw"}), 422)
	expect(t, admin.post("/admin/users", map[string]string{"email": nul + "@example.com", "username": "newtwo"}), 422)
	expect(t, alice.post("/workspaces", map[string]string{"name": nul}), 422)
	expect(t, alice.patch("/workspaces/"+ws, map[string]string{"name": nul}), 422)
	expect(t, alice.post("/projects", map[string]string{"workspace_id": ws, "name": nul}), 422)
	expect(t, alice.patch("/projects/"+proj, map[string]string{"name": nul}), 422)
	expect(t, alice.post("/projects/"+proj+"/environments", map[string]string{"name": nul}), 422)
	expect(t, alice.post("/projects/"+proj+"/streams", map[string]string{"name": nul}), 422)
	expect(t, alice.post("/tokens", map[string]any{"project_id": proj, "name": nul, "scopes": []string{"logs:read"}}), 422)
	expect(t, alice.post("/workspaces/"+ws+"/members", map[string]string{"email": nul + "@example.com", "role": "viewer"}), 422)
	expect(t, e.newClient().get("/auth/oauth/google/callback?state=a%00b&code=c"), 303)

	// Invalid UTF-8 in the User-Agent reaches a text column.
	expect(t, e.newClient().do("POST", "/auth/login", map[string]string{"login": "alice", "password": "alice-password-1"},
		map[string]string{"User-Agent": "bad\xffagent"}), 200)

	// From a provider.
	for _, claims := range []map[string]any{
		{"sub": "n1", "email": nul + "@example.com", "email_verified": true},
		{"sub": nul, "email": "n2@example.com", "email_verified": true},
	} {
		if loc := e.oidcLogin(e.newClient(), "google", claims); loc != "/login?error=exchange_failed" {
			t.Fatalf("redirect %q", loc)
		}
	}
}

func TestSetupCheckFailsClosed(t *testing.T) {
	failing := func(a *API) {
		a.countUsers = func(context.Context) (int, error) { return 0, errors.New("database down") }
	}
	e := newEnv(t, func(c *config.Auth) { c.OpenRegistration = true }, nil, failing)
	c := e.newClient()
	expect(t, c.post("/auth/register", map[string]string{"email": "a@example.com", "username": "alice", "password": "long-enough-pw"}), 500)
	expect(t, c.post("/auth/register", map[string]string{"email": "a@example.com", "username": "alice", "password": "long-enough-pw", "setup_code": e.cfg.SetupCode}), 500)
	expect(t, c.get("/auth/providers"), 500)
	var n int
	_ = e.queryRow(`SELECT count(*) FROM users`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d users created during the failure", n)
	}
}
