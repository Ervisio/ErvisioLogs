//go:build integration

package api

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/oauth"
)

// fakeIDP is a minimal OpenID Connect issuer: discovery, JWKS and a token endpoint that enforces
// single-use codes and PKCE, like a real provider.
type fakeIDP struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	other *rsa.PrivateKey // a key the JWKS does not publish, to forge tokens
	mu    sync.Mutex
	codes map[string]fakeGrant
}

type fakeGrant struct {
	claims    map[string]any
	nonce     string
	challenge string
	forge     bool
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	f := &fakeIDP{codes: map[string]fakeGrant{}}
	f.key, _ = rsa.GenerateKey(rand.Reader, 2048)
	f.other, _ = rsa.GenerateKey(rand.Reader, 2048)
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		g, ok := f.codes[r.Form.Get("code")]
		delete(f.codes, r.Form.Get("code")) // single use
		f.mu.Unlock()
		if !ok || oauth.Challenge(r.Form.Get("code_verifier")) != g.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		key := f.key
		if g.forge {
			key = f.other
		}
		sig, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		claims := map[string]any{"iss": f.srv.URL, "aud": "client-id", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": g.nonce}
		for k, v := range g.claims {
			claims[k] = v
		}
		tok, _ := jwt.Signed(sig).Claims(claims).Serialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": tok, "expires_in": 3600})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIDP) issue(g fakeGrant) string {
	code := "code-" + time.Now().Format("150405.000000000")
	f.mu.Lock()
	f.codes[code] = g
	f.mu.Unlock()
	return code
}

// fakeDiscord serves the OAuth2 token endpoint and /users/@me.
type fakeDiscord struct {
	srv   *httptest.Server
	mu    sync.Mutex
	codes map[string]fakeGrant
}

func newFakeDiscord(t *testing.T) *fakeDiscord {
	t.Helper()
	d := &fakeDiscord{codes: map[string]fakeGrant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		d.mu.Lock()
		g, ok := d.codes[r.Form.Get("code")]
		delete(d.codes, r.Form.Get("code"))
		d.mu.Unlock()
		if !ok || oauth.Challenge(r.Form.Get("code_verifier")) != g.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-" + r.Form.Get("code"), "token_type": "Bearer"})
	})
	mux.HandleFunc("/api/users/@me", func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer tok-")
		d.mu.Lock()
		defer d.mu.Unlock()
		g, ok := d.codes["profile:"+code]
		if !ok {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(g.claims)
	})
	d.srv = httptest.NewServer(mux)
	t.Cleanup(d.srv.Close)
	return d
}

func (d *fakeDiscord) issue(g fakeGrant, profile map[string]any) string {
	code := "dcode-" + time.Now().Format("150405.000000000")
	d.mu.Lock()
	d.codes[code] = g
	d.codes["profile:"+code] = fakeGrant{claims: profile}
	d.mu.Unlock()
	return code
}

type idpEnv struct {
	*env
	idp     *fakeIDP
	discord *fakeDiscord
}

func newIDPEnv(t *testing.T, open bool, tweak ...func(*API)) *idpEnv {
	idp, dc := newFakeIDP(t), newFakeDiscord(t)
	redirect := func(p string) string { return oauth.RedirectURL("http://app.test", p) }
	providers := map[string]oauth.Provider{
		"google":  oauth.NewOIDC("google", idp.srv.URL, "client-id", "secret", redirect("google")),
		"cognito": oauth.NewCognito(idp.srv.URL, "client-id", "secret", redirect("cognito")),
		"discord": oauth.NewDiscordWith("client-id", "secret", redirect("discord"),
			oauth.DiscordEndpoints{Auth: dc.srv.URL + "/authorize", Token: dc.srv.URL + "/token", API: dc.srv.URL + "/api"}),
	}
	e := newEnv(t, func(c *config.Auth) { c.OpenRegistration = open }, providers, tweak...)
	return &idpEnv{env: e, idp: idp, discord: dc}
}

// start begins the flow and returns the state, nonce and PKCE challenge the app put in the redirect.
func (e *idpEnv) start(c *client, provider string) (state, nonce, challenge string) {
	r := c.get("/auth/oauth/" + provider + "/start")
	expect(e.t, r, 302)
	u, err := url.Parse(r.Header.Get("Location"))
	if err != nil {
		e.t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" {
		e.t.Fatalf("authorization URL lacks state/PKCE: %s", u)
	}
	if provider != "discord" && q.Get("nonce") == "" {
		e.t.Fatal("OIDC authorization URL lacks nonce")
	}
	if q.Get("response_type") != "code" || q.Get("client_id") != "client-id" {
		e.t.Fatalf("bad authorization URL: %s", u)
	}
	return q.Get("state"), q.Get("nonce"), q.Get("code_challenge")
}

func (e *idpEnv) callback(c *client, provider, state, code string) string {
	r := c.get("/auth/oauth/" + provider + "/callback?state=" + url.QueryEscape(state) + "&code=" + url.QueryEscape(code))
	expect(e.t, r, 303)
	return r.Header.Get("Location")
}

// oidcLogin runs a complete OIDC sign-in and returns where the app redirected.
func (e *idpEnv) oidcLogin(c *client, provider string, claims map[string]any) string {
	state, nonce, ch := e.start(c, provider)
	return e.callback(c, provider, state, e.idp.issue(fakeGrant{claims: claims, nonce: nonce, challenge: ch}))
}

// binding returns the browser's OAuth cookie, to replay it from elsewhere.
func binding(c *client) string {
	u, err := url.Parse(c.base + oauthCookiePath + "/")
	if err != nil {
		c.t.Fatal(err)
	}
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == oauthStateCookie {
			return ck.Value
		}
	}
	return ""
}

// callbackWith calls back from a new client presenting the given OAuth cookie.
func (e *idpEnv) callbackWith(cookie, provider, state, code string) string {
	path := "/auth/oauth/" + provider + "/callback?state=" + url.QueryEscape(state) + "&code=" + url.QueryEscape(code)
	r := e.newClient().do("GET", path, nil, map[string]string{"Cookie": oauthStateCookie + "=" + cookie})
	expect(e.t, r, 303)
	return r.Header.Get("Location")
}

func (e *idpEnv) identities(email string) int {
	var n int
	_ = e.queryRow(`SELECT count(*) FROM identities i JOIN users u ON u.id=i.user_id WHERE u.email=$1`, email).Scan(&n)
	return n
}

func TestOIDCSignInCreatesAndReusesAccount(t *testing.T) {
	for _, provider := range []string{"google", "cognito"} {
		t.Run(provider, func(t *testing.T) {
			e := newIDPEnv(t, true)
			e.setupAdmin()
			claims := map[string]any{"sub": "sub-123", "email": "Dana.Smith@Example.com", "email_verified": true, "name": "Dana"}
			if provider == "cognito" {
				claims["email_verified"] = "true" // Cognito can emit the claim as a string
			}
			c := e.newClient()
			if loc := e.oidcLogin(c, provider, claims); loc != "/" {
				t.Fatalf("redirect %q", loc)
			}
			me := expect(t, c.get("/auth/me"), 200).JSON()["user"].(map[string]any)
			if me["email"] != "dana.smith@example.com" || me["username"] != "dana.smith" || me["email_verified"] != true || me["is_instance_admin"] != false {
				t.Fatalf("me: %v", me)
			}
			// Same subject again: same account, no duplicate.
			c2 := e.newClient()
			e.oidcLogin(c2, provider, claims)
			if id(expect(t, c2.get("/auth/me"), 200), "user", "id") != me["id"] {
				t.Fatal("second sign-in created another account")
			}
			if e.identities("dana.smith@example.com") != 1 {
				t.Fatal("identity duplicated")
			}
			// The OAuth-only account has no password: password login must fail.
			expect(t, e.newClient().post("/auth/login", map[string]string{"login": "dana.smith", "password": "anything-at-all"}), 401)
		})
	}
}

func TestOIDCRegistrationPolicies(t *testing.T) {
	claims := map[string]any{"sub": "s1", "email": "new@example.com", "email_verified": true}

	t.Run("closed registration refuses unknown people", func(t *testing.T) {
		e := newIDPEnv(t, false)
		e.setupAdmin()
		if loc := e.oidcLogin(e.newClient(), "google", claims); loc != "/login?error=registration_closed" {
			t.Fatalf("redirect %q", loc)
		}
	})
	t.Run("nobody can become admin through OAuth on a fresh install", func(t *testing.T) {
		e := newIDPEnv(t, true)
		if loc := e.oidcLogin(e.newClient(), "google", claims); loc != "/login?error=setup_required" {
			t.Fatalf("redirect %q", loc)
		}
		var n int
		_ = e.queryRow(`SELECT count(*) FROM users`).Scan(&n)
		if n != 0 {
			t.Fatal("a user was created before setup")
		}
	})
	t.Run("an admin-created account without password links on first sign-in", func(t *testing.T) {
		e := newIDPEnv(t, false)
		admin := e.setupAdmin()
		expect(t, admin.post("/admin/users", map[string]string{"email": "new@example.com", "username": "newbie"}), 201)
		c := e.newClient()
		if loc := e.oidcLogin(c, "google", claims); loc != "/" {
			t.Fatalf("redirect %q", loc)
		}
		if me := expect(t, c.get("/auth/me"), 200).JSON()["user"].(map[string]any); me["username"] != "newbie" {
			t.Fatalf("linked to the wrong account: %v", me)
		}
	})
	t.Run("account takeover: unverified password account is not linked", func(t *testing.T) {
		e := newIDPEnv(t, true)
		e.setupAdmin()
		// An attacker registers the victim's address with a password they know...
		attacker := e.newClient()
		expect(t, attacker.post("/auth/register", map[string]string{"email": "victim@example.com", "username": "attacker", "password": "attacker-password-1"}), 201)
		// ...then the real owner signs in with Google.
		victim := e.newClient()
		loc := e.oidcLogin(victim, "google", map[string]any{"sub": "victim-sub", "email": "victim@example.com", "email_verified": true})
		if loc != "/login?error=email_in_use" {
			t.Fatalf("redirect %q", loc)
		}
		expect(t, victim.get("/auth/me"), 401)
		if e.identities("victim@example.com") != 0 {
			t.Fatal("identity was linked to the attacker's account")
		}
	})
	t.Run("verified local accounts do link", func(t *testing.T) {
		e := newIDPEnv(t, true)
		e.setupAdmin()
		if err := e.exec(`UPDATE users SET email_verified = true WHERE email = 'admin@example.com'`); err != nil {
			t.Fatal(err)
		}
		c := e.newClient()
		loc := e.oidcLogin(c, "google", map[string]any{"sub": "admin-sub", "email": "admin@example.com", "email_verified": true})
		if loc != "/" {
			t.Fatalf("redirect %q", loc)
		}
		if me := expect(t, c.get("/auth/me"), 200).JSON()["user"].(map[string]any); me["username"] != "admin" {
			t.Fatalf("me: %v", me)
		}
	})
}

func TestOIDCRejectsBadTokens(t *testing.T) {
	e := newIDPEnv(t, true)
	e.setupAdmin()
	good := map[string]any{"sub": "s1", "email": "x@example.com", "email_verified": true}
	run := func(name string, mut func(*fakeGrant, *string), want string) {
		t.Run(name, func(t *testing.T) {
			c := e.newClient()
			state, nonce, ch := e.start(c, "google")
			g := fakeGrant{claims: good, nonce: nonce, challenge: ch}
			mut(&g, &state)
			if loc := e.callback(c, "google", state, e.idp.issue(g)); loc != want {
				t.Fatalf("redirect %q, want %q", loc, want)
			}
			expect(t, c.get("/auth/me"), 401)
		})
	}
	run("forged signature", func(g *fakeGrant, _ *string) { g.forge = true }, "/login?error=exchange_failed")
	run("wrong nonce", func(g *fakeGrant, _ *string) { g.nonce = "attacker-nonce" }, "/login?error=exchange_failed")
	run("wrong PKCE verifier", func(g *fakeGrant, _ *string) { g.challenge = "not-the-challenge" }, "/login?error=exchange_failed")
	run("unknown state", func(_ *fakeGrant, s *string) { *s = "forged-state" }, "/login?error=invalid_state")
	run("unverified email", func(g *fakeGrant, _ *string) {
		g.claims = map[string]any{"sub": "s2", "email": "y@example.com", "email_verified": false}
	}, "/login?error=email_unverified")
	run("expired token", func(g *fakeGrant, _ *string) {
		g.claims = map[string]any{"sub": "s3", "email": "z@example.com", "email_verified": true, "exp": time.Now().Add(-time.Hour).Unix()}
	}, "/login?error=exchange_failed")
	run("wrong audience", func(g *fakeGrant, _ *string) {
		g.claims = map[string]any{"sub": "s4", "email": "w@example.com", "email_verified": true, "aud": "someone-else"}
	}, "/login?error=exchange_failed")
	run("missing email", func(g *fakeGrant, _ *string) { g.claims = map[string]any{"sub": "s5"} }, "/login?error=exchange_failed")

	var n int
	_ = e.queryRow(`SELECT count(*) FROM users WHERE email <> 'admin@example.com'`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d accounts were created by rejected sign-ins", n)
	}
}

func TestOAuthStateIsSingleUseAndProviderBound(t *testing.T) {
	e := newIDPEnv(t, true)
	e.setupAdmin()
	claims := map[string]any{"sub": "s1", "email": "q@example.com", "email_verified": true}

	c := e.newClient()
	state, nonce, ch := e.start(c, "google")
	code := e.idp.issue(fakeGrant{claims: claims, nonce: nonce, challenge: ch})
	if loc := e.callback(c, "google", state, code); loc != "/" {
		t.Fatalf("first use: %q", loc)
	}
	// The cases below present the right cookie, so only the stored state can refuse them.
	// Replaying the same callback fails: the state is consumed.
	code2 := e.idp.issue(fakeGrant{claims: claims, nonce: nonce, challenge: ch})
	if loc := e.callbackWith(e.api.keyedDigest("oauth-state", state), "google", state, code2); loc != "/login?error=invalid_state" {
		t.Fatalf("replay: %q", loc)
	}
	// An unknown state.
	if loc := e.callbackWith(e.api.keyedDigest("oauth-state", "forged"), "google", "forged", code2); loc != "/login?error=invalid_state" {
		t.Fatalf("unknown state: %q", loc)
	}
	// A state issued for Google cannot complete a Cognito callback.
	state, nonce, ch = e.start(c, "google")
	code3 := e.idp.issue(fakeGrant{claims: claims, nonce: nonce, challenge: ch})
	if loc := e.callbackWith(binding(c), "cognito", state, code3); loc != "/login?error=invalid_state" {
		t.Fatalf("cross-provider: %q", loc)
	}
	// Expired state.
	state, nonce, ch = e.start(c, "google")
	if err := e.exec(`UPDATE oauth_states SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	code4 := e.idp.issue(fakeGrant{claims: claims, nonce: nonce, challenge: ch})
	if loc := e.callbackWith(binding(c), "google", state, code4); loc != "/login?error=invalid_state" {
		t.Fatalf("expired: %q", loc)
	}
	// The provider reporting an error never creates a session and is not reflected into the page.
	r := e.newClient().get("/auth/oauth/google/callback?error=access_denied&error_description=%3Cscript%3E")
	expect(t, r, 303)
	if loc := r.Header.Get("Location"); loc != "/login?error=provider_denied" {
		t.Fatalf("provider error: %q", loc)
	}
	expect(t, e.newClient().get("/auth/oauth/nope/start"), 404)
}

func TestDiscordSignIn(t *testing.T) {
	e := newIDPEnv(t, true)
	e.setupAdmin()
	login := func(profile map[string]any) string {
		c := e.newClient()
		state, _, ch := e.start(c, "discord")
		code := e.discord.issue(fakeGrant{challenge: ch}, profile)
		loc := e.callback(c, "discord", state, code)
		if loc == "/" {
			expect(t, c.get("/auth/me"), 200)
		}
		return loc
	}
	profile := map[string]any{"id": "80351110224678912", "username": "nelly", "global_name": "Nelly", "email": "nelly@example.com", "verified": true}
	if loc := login(profile); loc != "/" {
		t.Fatalf("redirect %q", loc)
	}
	if loc := login(profile); loc != "/" || e.identities("nelly@example.com") != 1 {
		t.Fatalf("second sign-in: %q identities=%d", loc, e.identities("nelly@example.com"))
	}
	// Discord lets people keep an unverified email: that must not create or link an account.
	if loc := login(map[string]any{"id": "2", "username": "eve", "email": "eve@example.com", "verified": false}); loc != "/login?error=email_unverified" {
		t.Fatalf("unverified: %q", loc)
	}
	if loc := login(map[string]any{"id": "3", "username": "noemail", "verified": true}); loc != "/login?error=exchange_failed" {
		t.Fatalf("no email: %q", loc)
	}
	// A wrong PKCE verifier is rejected by the provider.
	c := e.newClient()
	state, _, _ := e.start(c, "discord")
	code := e.discord.issue(fakeGrant{challenge: "different"}, profile)
	if loc := e.callback(c, "discord", state, code); loc != "/login?error=exchange_failed" {
		t.Fatalf("pkce: %q", loc)
	}
}

func TestOAuthDisabledAccountCannotSignIn(t *testing.T) {
	e := newIDPEnv(t, true)
	admin := e.setupAdmin()
	claims := map[string]any{"sub": "s9", "email": "gone@example.com", "email_verified": true}
	c := e.newClient()
	e.oidcLogin(c, "google", claims)
	uid := id(expect(t, c.get("/auth/me"), 200), "user", "id")
	expect(t, admin.patch("/admin/users/"+uid, map[string]string{"status": "disabled"}), 204)
	expect(t, c.get("/auth/me"), 401)
	if loc := e.oidcLogin(e.newClient(), "google", claims); loc != "/login?error=account_disabled" {
		t.Fatalf("redirect %q", loc)
	}
}

func TestProvidersListOnlyConfiguredOnes(t *testing.T) {
	e := newEnv(t, nil, nil)
	got := expect(t, e.newClient().get("/auth/providers"), 200).JSON()["providers"].([]any)
	if len(got) != 0 {
		t.Fatalf("providers: %v", got)
	}
	e2 := newIDPEnv(t, false)
	got = expect(t, e2.newClient().get("/auth/providers"), 200).JSON()["providers"].([]any)
	if len(got) != 3 || got[0] != "cognito" || got[1] != "discord" || got[2] != "google" {
		t.Fatalf("providers: %v", got)
	}
}
