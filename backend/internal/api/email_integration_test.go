//go:build integration

package api

import (
	"context"
	"errors"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/mail"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
)

// fakeMail records what the API sends; fail makes every delivery fail.
type fakeMail struct {
	sent chan mail.Message
	fail atomic.Bool
}

func (f *fakeMail) Send(_ context.Context, c mail.Config, m mail.Message) error {
	if f.fail.Load() {
		return errors.New("smtp down")
	}
	if c.Host != "smtp.test" || c.Password != "env-password" {
		return errors.New("unexpected config")
	}
	f.sent <- m
	return nil
}

func (f *fakeMail) next(t *testing.T, to string) string {
	t.Helper()
	select {
	case m := <-f.sent:
		if m.To != to {
			t.Fatalf("mail to %s, want %s", m.To, to)
		}
		tok := linkToken.FindStringSubmatch(m.Body)
		if tok == nil {
			t.Fatalf("no link in %q", m.Body)
		}
		return tok[1]
	case <-time.After(5 * time.Second):
		t.Fatalf("no mail to %s", to)
		return ""
	}
}

func (f *fakeMail) none(t *testing.T) {
	t.Helper()
	select {
	case m := <-f.sent:
		t.Fatalf("unexpected mail to %s", m.To)
	case <-time.After(200 * time.Millisecond):
	}
}

var linkToken = regexp.MustCompile(`#token=([A-Za-z0-9_-]+)`)

func newMailEnv(t *testing.T) (*env, *fakeMail, *client) {
	fm := &fakeMail{sent: make(chan mail.Message, 10)}
	e := newEnv(t, nil, nil, func(a *API) { a.Mail = &Mail{Sender: fm, Password: "env-password"} })
	return e, fm, e.setupAdmin()
}

func enableMail(t *testing.T, admin *client) {
	expect(t, admin.put("/admin/settings/"+settings.SMTPHost, map[string]any{"value": "smtp.test"}), 200)
	expect(t, admin.put("/admin/settings/"+settings.MailFrom, map[string]any{"value": "Logs <logs@example.com>"}), 200)
}

func TestPasswordResetAndVerification(t *testing.T) {
	e, fm, admin := newMailEnv(t)
	alice := e.newUser(admin, "alice")
	anon := e.newClient()

	expect(t, anon.post("/auth/password/forgot", map[string]string{"email": "alice@example.com"}), 409)
	expect(t, admin.put("/admin/settings/"+settings.SMTPHost, map[string]any{"value": "smtp.test:25"}), 422)
	enableMail(t, admin)
	if expect(t, anon.get("/auth/providers"), 200).JSON()["email"] != true {
		t.Fatal("email not reported as enabled")
	}

	// Registration sends a verification link; it works once.
	expect(t, admin.put("/admin/settings/"+settings.OpenRegistration, map[string]any{"value": true}), 200)
	carol := e.newClient()
	expect(t, carol.adopt(carol.post("/auth/register", map[string]string{
		"email": "carol@example.com", "username": "carol", "password": "carol-password-1"})), 201)
	tok := fm.next(t, "carol@example.com")
	expect(t, carol.post("/auth/email/verification", nil), 202)
	tok2 := fm.next(t, "carol@example.com")
	expect(t, anon.post("/auth/email/verify", map[string]string{"token": tok}), 410) // replaced
	expect(t, anon.post("/auth/email/verify", map[string]string{"token": tok2}), 204)
	expect(t, anon.post("/auth/email/verify", map[string]string{"token": tok2}), 410)
	if me := expect(t, carol.get("/auth/me"), 200).JSON()["user"].(map[string]any); me["email_verified"] != true {
		t.Fatalf("not verified: %v", me)
	}
	expect(t, carol.post("/auth/email/verification", nil), 409)

	// Unknown addresses get the same answer and no mail.
	expect(t, anon.post("/auth/password/forgot", map[string]string{"email": "nobody@example.com"}), 202)
	fm.none(t)

	expect(t, anon.post("/auth/password/forgot", map[string]string{"email": "Alice@Example.com"}), 202)
	tok = fm.next(t, "alice@example.com")
	expect(t, anon.post("/auth/password/reset", map[string]string{"token": tok, "password": "short"}), 422)
	expect(t, anon.post("/auth/password/reset", map[string]string{"token": "nope", "password": "alice-new-password-2"}), 410)
	expect(t, anon.post("/auth/password/reset", map[string]string{"token": tok, "password": "alice-new-password-2"}), 204)
	expect(t, anon.post("/auth/password/reset", map[string]string{"token": tok, "password": "alice-new-password-3"}), 410)
	expect(t, alice.get("/auth/me"), 401)
	alice = e.newClient()
	expect(t, alice.post("/auth/login", map[string]string{"login": "alice", "password": "alice-password-1"}), 401)
	me := expect(t, alice.adopt(alice.post("/auth/login", map[string]string{"login": "alice", "password": "alice-new-password-2"})), 200).JSON()
	if me["user"].(map[string]any)["email_verified"] != true {
		t.Fatalf("a used reset link should verify the address: %v", me)
	}

	// Three requests an hour per address.
	expect(t, anon.post("/auth/password/forgot", map[string]string{"email": "alice@example.com"}), 202)
	expect(t, anon.post("/auth/password/forgot", map[string]string{"email": "alice@example.com"}), 202)
	expect(t, anon.post("/auth/password/forgot", map[string]string{"email": "alice@example.com"}), 429)
}

func TestInvitations(t *testing.T) {
	e, fm, admin := newMailEnv(t)
	alice, bob := e.newUser(admin, "alice"), e.newUser(admin, "bob")
	anon := e.newClient()
	ws := id(expect(t, admin.post("/workspaces", map[string]string{"name": "Server"}), 201), "id")
	path := "/workspaces/" + ws + "/invitations"

	// Without email the inviter gets the link; accepting it does not verify the address.
	r := expect(t, admin.post(path, map[string]string{"email": "New@Example.com", "role": "developer"}), 201).JSON()
	if r["emailed"] != false || r["url"] == nil {
		t.Fatalf("invitation %v", r)
	}
	tok := linkToken.FindStringSubmatch(r["url"].(string))[1]
	p := expect(t, anon.post("/invitations/preview", map[string]string{"token": tok}), 200).JSON()
	if p["email"] != "new@example.com" || p["has_account"] != false || p["workspace_name"] != "Server" {
		t.Fatalf("preview %v", p)
	}
	newbie := e.newClient()
	expect(t, newbie.post("/invitations/register", map[string]string{"token": tok, "username": "bob", "password": "newbie-password-1"}), 409)
	me := expect(t, newbie.adopt(newbie.post("/invitations/register", map[string]string{
		"token": tok, "username": "newbie", "password": "newbie-password-1"})), 201).JSON()
	if u := me["user"].(map[string]any); u["email"] != "new@example.com" || u["email_verified"] != false {
		t.Fatalf("registered %v", u)
	}
	expect(t, newbie.get("/workspaces/"+ws), 200)
	expect(t, anon.post("/invitations/preview", map[string]string{"token": tok}), 410)

	// With email the link goes to the address only, and accepting it verifies the address.
	enableMail(t, admin)
	r = expect(t, admin.post(path, map[string]string{"email": "alice@example.com", "role": "viewer"}), 201).JSON()
	if r["emailed"] != true || r["url"] != nil {
		t.Fatalf("emailed invitation %v", r)
	}
	aliceTok := fm.next(t, "alice@example.com")
	r = expect(t, admin.post(path, map[string]string{"email": "bob@example.com", "role": "analyst"}), 201).JSON()
	bobTok := fm.next(t, "bob@example.com")
	expect(t, alice.post("/invitations/accept", map[string]string{"token": bobTok}), 403)
	expect(t, anon.post("/invitations/accept", map[string]string{"token": aliceTok}), 401)
	if w := expect(t, alice.post("/invitations/accept", map[string]string{"token": aliceTok}), 200).JSON(); w["role"] != "viewer" {
		t.Fatalf("joined %v", w)
	}
	if u := expect(t, alice.get("/auth/me"), 200).JSON()["user"].(map[string]any); u["email_verified"] != true {
		t.Fatalf("alice not verified: %v", u)
	}

	// A member's address cannot be invited; a viewer cannot invite.
	expect(t, admin.post(path, map[string]string{"email": "alice@example.com", "role": "viewer"}), 409)
	expect(t, alice.post(path, map[string]string{"email": "x@example.com", "role": "viewer"}), 403)
	expect(t, admin.post(path, map[string]string{"email": "not-an-email", "role": "viewer"}), 422)

	// Listing and revoking; a revoked link is gone.
	items := expect(t, admin.get(path), 200).JSON()["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["email"] != "bob@example.com" {
		t.Fatalf("invitations %v", items)
	}
	expect(t, alice.get(path), 403)
	expect(t, admin.delete(path+"/"+r["id"].(string), nil), 204)
	expect(t, bob.post("/invitations/accept", map[string]string{"token": bobTok}), 410)

	// A failed delivery hands the link to the inviter instead.
	fm.fail.Store(true)
	r = expect(t, admin.post(path, map[string]string{"email": "bob@example.com", "role": "analyst"}), 201).JSON()
	if r["emailed"] != false || r["url"] == nil || r["mail_error"] == nil {
		t.Fatalf("failed delivery %v", r)
	}
	expect(t, bob.post("/invitations/accept", map[string]string{"token": linkToken.FindStringSubmatch(r["url"].(string))[1]}), 200)
	if u := expect(t, bob.get("/auth/me"), 200).JSON()["user"].(map[string]any); u["email_verified"] != false {
		t.Fatalf("bob verified by a shared link: %v", u)
	}
}
