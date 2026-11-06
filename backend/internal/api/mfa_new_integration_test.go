//go:build integration

package api

import (
	"context"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/google/uuid"
)

func TestTOTPEnrollmentLoginRecoveryAndReplay(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	uid := uuid.MustParse(id(expect(t, admin.get("/auth/me"), 200), "user", "id"))
	second := e.newClient()
	expect(t, second.adopt(second.post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"})), 200)
	setup := expect(t, admin.post("/auth/totp/setup", map[string]string{"password": "admin-password-1"}), 200).JSON()
	secret := setup["secret"].(string)
	var stored []byte
	if err := e.queryRow(`SELECT secret FROM user_totp WHERE user_id=$1`, uid).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) == secret {
		t.Fatal("plaintext TOTP secret persisted")
	}
	now := time.Now().UTC()
	code, err := auth.TOTPCode(secret, now.Unix()/30)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := expect(t, admin.post("/auth/totp/confirm", map[string]string{"code": code}), 200).JSON()
	codes := confirmed["recovery_codes"].([]any)
	if len(codes) != 10 {
		t.Fatal("missing recovery codes")
	}
	expect(t, second.get("/auth/me"), 401)
	expect(t, admin.post("/auth/totp/setup", map[string]string{"password": "admin-password-1"}), 409)
	user := e.newClient()
	challenge := expect(t, user.post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}), 202).JSON()["challenge"].(string)
	expect(t, user.get("/auth/me"), 401)
	expect(t, user.post("/auth/mfa/verify", map[string]string{"challenge": challenge, "code": code}), 401)
	expect(t, user.adopt(user.post("/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[0].(string)})), 200)
	expect(t, user.post("/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[1].(string)}), 401)
	fresh := e.newClient()
	challenge = expect(t, fresh.post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}), 202).JSON()["challenge"].(string)
	expect(t, fresh.post("/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[0].(string)}), 401)
	expect(t, fresh.adopt(fresh.post("/auth/mfa/verify", map[string]string{"challenge": challenge, "code": codes[1].(string)})), 200)
	expect(t, fresh.delete("/auth/totp", map[string]string{"password": "admin-password-1", "code": codes[2].(string)}), 204)
	expect(t, e.newClient().post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}), 200)
	var count int
	if err := e.queryRow(`SELECT count(*) FROM user_recovery_codes WHERE user_id=$1`, uid).Scan(&count); err != nil || count != 0 {
		t.Fatalf("recovery codes remain: %d %v", count, err)
	}
}
func TestMFAChallengeAttemptsExpiryAndDisabledAccount(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	uid := uuid.MustParse(id(expect(t, admin.get("/auth/me"), 200), "user", "id"))
	secret, _ := auth.NewTOTPSecret()
	sealed, err := auth.Seal(e.api.secret, []byte(secret), "totp:"+uid.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.exec(`INSERT INTO user_totp(user_id,secret,enabled) VALUES($1,$2,true)`, uid, sealed); err != nil {
		t.Fatal(err)
	}
	fresh := e.newClient()
	challenge := expect(t, fresh.post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}), 202).JSON()["challenge"].(string)
	for i := 0; i < 5; i++ {
		expect(t, fresh.post("/auth/mfa/verify", map[string]string{"challenge": challenge, "code": "invalid"}), 401)
	}
	code, _ := auth.TOTPCode(secret, time.Now().Unix()/30)
	expect(t, fresh.post("/auth/mfa/verify", map[string]string{"challenge": challenge, "code": code}), 401)
	challenge = expect(t, fresh.post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}), 202).JSON()["challenge"].(string)
	if err := e.exec(`UPDATE auth_challenges SET expires_at=now()-interval '1 second' WHERE user_id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	expect(t, fresh.post("/auth/mfa/verify", map[string]string{"challenge": challenge, "code": code}), 401)
	challenge = expect(t, fresh.post("/auth/login", map[string]string{"login": "admin", "password": "admin-password-1"}), 202).JSON()["challenge"].(string)
	if err := e.exec(`UPDATE users SET status='disabled' WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	expect(t, fresh.post("/auth/mfa/verify", map[string]string{"challenge": challenge, "code": code}), 401)
	expect(t, fresh.get("/auth/me"), 401)
	if _, err := e.store.TOTP(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
}
func TestTokenRotationRevokesOldAndRetainsExpiry(t *testing.T) {
	e := newEnv(t, nil, nil)
	admin := e.setupAdmin()
	ws := id(expect(t, admin.post("/workspaces", map[string]string{"name": "Rotation"}), 201), "id")
	project := id(expect(t, admin.post("/projects", map[string]string{"workspace_id": ws, "name": "Service"}), 201), "id")
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	old := expect(t, admin.post("/tokens", map[string]any{"project_id": project, "name": "Collector", "scopes": []string{"logs:ingest"}, "expires_at": expires}), 201)
	rotated := expect(t, admin.post("/tokens/"+id(old, "api_token", "id")+"/rotate", nil), 201)
	expect(t, e.newClient().do("GET", "/tokens/current", nil, map[string]string{"Authorization": "Bearer " + id(old, "token")}), 401)
	expect(t, e.newClient().do("GET", "/tokens/current", nil, map[string]string{"Authorization": "Bearer " + id(rotated, "token")}), 200)
	expect(t, admin.post("/tokens/"+id(old, "api_token", "id")+"/rotate", nil), 404)
	if got := rotated.JSON()["api_token"].(map[string]any)["expires_at"]; got != expires.Format(time.RFC3339) {
		t.Fatalf("expiry changed: %v", got)
	}
}
