//go:build integration

package api

import "testing"

func webauthnChallenge(t *testing.T, r resp) string {
	t.Helper()
	options := r.JSON()["options"].(map[string]any)
	return options["publicKey"].(map[string]any)["challenge"].(string)
}
func TestPasskeyRegistrationLoginOriginAndReplay(t *testing.T) {
	e := newEnv(t, nil, nil, func(a *API) { a.origin = "http://localhost" })
	admin := e.setupAdmin()
	v := newVirtualPasskey(t)
	start := expect(t, admin.post("/auth/passkeys/register/start", map[string]string{"password": "admin-password-1", "name": "Hardware key"}), 200)
	credential := v.create(t, webauthnChallenge(t, start), "http://localhost")
	body := map[string]any{"challenge": id(start, "challenge"), "credential": credential}
	other := e.newClient()
	expect(t, other.post("/auth/passkeys/register/finish", body), 401)
	saved := expect(t, admin.post("/auth/passkeys/register/finish", body), 201)
	expect(t, admin.post("/auth/passkeys/register/finish", body), 401)
	fresh := e.newClient()
	start = expect(t, fresh.post("/auth/passkeys/login/start", map[string]string{"login": "admin"}), 200)
	body = map[string]any{"challenge": id(start, "challenge"), "credential": v.assertion(t, webauthnChallenge(t, start), "https://attacker.example")}
	expect(t, fresh.post("/auth/passkeys/login/finish", body), 401)
	body["credential"] = v.assertion(t, webauthnChallenge(t, start), "http://localhost")
	expect(t, other.post("/auth/passkeys/login/finish", body), 401)
	expect(t, fresh.adopt(fresh.post("/auth/passkeys/login/finish", body)), 200)
	expect(t, fresh.post("/auth/passkeys/login/finish", body), 401)
	expect(t, fresh.get("/auth/me"), 200)
	expect(t, admin.delete("/auth/passkeys/"+id(saved, "id"), map[string]string{"password": "admin-password-1"}), 204)
	expect(t, e.newClient().post("/auth/passkeys/login/start", map[string]string{"login": "admin"}), 401)
}
