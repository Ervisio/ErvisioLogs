package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/oauth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

const (
	oauthStateTTL    = 10 * time.Minute
	oauthStateCookie = "ervisio_logs_oauth_state"
	oauthCookiePath  = "/api/v1/auth/oauth"
)

// setOAuthCookie binds the flow to this browser, so a callback URL prepared by someone else cannot
// sign the victim in to the attacker's account (login CSRF). It holds a digest, not the state.
func (a *API) setOAuthCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Value: value, Path: oauthCookiePath, MaxAge: maxAge,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
}

func (a *API) oauthStart(w http.ResponseWriter, r *http.Request) {
	p, ok := a.Providers[chi.URLParam(r, "provider")]
	if !ok {
		httpx.WriteProblem(w, r, http.StatusNotFound, "unknown sign-in provider")
		return
	}
	if !a.loginIP.Allow(a.rateIP(r)) {
		tooMany(w, r, time.Minute)
		return
	}
	a.purgeExpired(r.Context())
	state, nonce, verifier := auth.NewRandom(32), auth.NewRandom(24), auth.NewRandom(32)
	if err := a.Store.SaveOAuthState(r.Context(), auth.HashSession(state), p.Name(), nonce, verifier, a.now().Add(oauthStateTTL)); err != nil {
		a.internalError(w, r, err)
		return
	}
	u, err := p.AuthURL(r.Context(), state, nonce, verifier)
	if err != nil {
		a.Log.Error("oauth start", "provider", p.Name(), "error", err)
		httpx.WriteProblem(w, r, http.StatusBadGateway, "the sign-in provider is unreachable")
		return
	}
	a.setOAuthCookie(w, a.keyedDigest("oauth-state", state), int(oauthStateTTL/time.Second))
	http.Redirect(w, r, u, http.StatusFound)
}

// oauthCallback completes the code flow. Every failure redirects to /login with a fixed error code,
// never echoing provider-supplied text into the page.
func (a *API) oauthCallback(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "provider")
	p, ok := a.Providers[name]
	if !ok {
		httpx.WriteProblem(w, r, http.StatusNotFound, "unknown sign-in provider")
		return
	}
	// Every failure below writes an audit row, so the limit comes first. Blocked requests are only logged.
	if !a.oauthCallbackIP.Allow(a.rateIP(r)) {
		a.Log.Warn("oauth callback throttled", "request_id", httpx.RequestID(r.Context()), "ip", a.clientIP(r))
		tooMany(w, r, time.Minute)
		return
	}
	binding, cookieErr := r.Cookie(oauthStateCookie)
	a.setOAuthCookie(w, "", -1)
	fail := func(code string) {
		a.audit(r, nil, "auth.oauth_failed", "provider", name, map[string]any{"reason": code})
		http.Redirect(w, r, "/login?error="+url.QueryEscape(code), http.StatusSeeOther)
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		fail("provider_denied")
		return
	}
	state, code := q.Get("state"), q.Get("code")
	if state == "" || code == "" {
		fail("invalid_request")
		return
	}
	if cookieErr != nil || subtle.ConstantTimeCompare([]byte(binding.Value), []byte(a.keyedDigest("oauth-state", state))) != 1 {
		fail("invalid_state") // another browser: the state stays usable by the one that started the flow
		return
	}
	st, err := a.Store.ConsumeOAuthState(r.Context(), auth.HashSession(state), name)
	if err != nil {
		fail("invalid_state") // unknown, expired, replayed, or issued for another provider
		return
	}
	profile, err := p.Exchange(r.Context(), code, st.Verifier, st.Nonce)
	if err != nil {
		if errors.Is(err, oauth.ErrUnverifiedEmail) {
			fail("email_unverified")
			return
		}
		a.Log.Warn("oauth exchange failed", "provider", name, "error", err)
		fail("exchange_failed")
		return
	}
	if hasNUL(profile.Subject) || hasNUL(profile.Email) {
		fail("exchange_failed")
		return
	}

	u, reason := a.resolveOAuthUser(r, name, profile)
	if reason != "" {
		fail(reason)
		return
	}
	if u.Status != "active" {
		fail("account_disabled")
		return
	}
	if a.mfaChallenge(w, r, u, true) {
		return
	}
	if !a.startSession(w, r, u) {
		return
	}
	a.audit(r, nil, "auth.login", "user", u.ID.String(), map[string]any{"method": name})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// resolveOAuthUser maps a verified provider profile to a local account.
//  1. A known (provider, subject) signs in to its account.
//  2. Otherwise a verified email matching an existing account links to it, unless that account is
//     disabled, has a password and an unverified email (its owner never proved the address, so linking
//     would hand the account to whoever registered it first), or already has another identity of this
//     provider.
//  3. Otherwise a new account is created, only when registration is open and the instance is set up.
func (a *API) resolveOAuthUser(r *http.Request, provider string, pr oauth.Profile) (store.User, string) {
	ctx := r.Context()
	u, err := a.Store.UserByIdentity(ctx, provider, pr.Subject)
	if err == nil {
		return u, ""
	}
	if !errors.Is(err, store.ErrNotFound) {
		a.Log.Error("oauth identity lookup", "error", err)
		return store.User{}, "server_error"
	}
	existing, err := a.Store.UserByEmail(ctx, pr.Email)
	switch {
	case err == nil:
		if existing.Status != "active" {
			return store.User{}, "account_disabled"
		}
		if !existing.EmailVerified && existing.HasPassword {
			return store.User{}, "email_in_use"
		}
		if err := a.Store.LinkIdentity(ctx, existing.ID, provider, pr.Subject); err != nil {
			if errors.Is(err, store.ErrIdentityConflict) {
				return store.User{}, "identity_conflict"
			}
			a.Log.Error("oauth link", "error", err)
			return store.User{}, "server_error"
		}
		a.audit(r, nil, "auth.identity_linked", "user", existing.ID.String(), map[string]any{"provider": provider})
		return existing, ""
	case !errors.Is(err, store.ErrNotFound):
		a.Log.Error("oauth email lookup", "error", err)
		return store.User{}, "server_error"
	}

	setup, err := a.setupRequired(ctx)
	if err != nil {
		a.Log.Error("oauth setup check", "error", err)
		return store.User{}, "server_error"
	}
	if setup {
		return store.User{}, "setup_required" // the first account must be created with the setup code
	}
	if !a.Settings.Bool(settings.OpenRegistration) {
		return store.User{}, "registration_closed"
	}
	for i := 0; i < 5; i++ {
		username, err := a.freeUsername(r, pr, i)
		if err != nil {
			return store.User{}, "server_error"
		}
		u, err = a.Store.CreateOAuthUser(ctx, store.NewUser{Email: pr.Email, Username: username}, provider, pr.Subject)
		if err == nil {
			a.audit(r, nil, "auth.register", "user", u.ID.String(), map[string]any{"method": provider})
			return u, ""
		}
		if !errors.Is(err, store.ErrConflict) {
			a.Log.Error("oauth create user", "error", err)
			return store.User{}, "server_error"
		}
		// A concurrent callback may have created the identity: retry the lookup once.
		if again, lerr := a.Store.UserByIdentity(ctx, provider, pr.Subject); lerr == nil {
			return again, ""
		}
	}
	return store.User{}, "server_error"
}

var nonUsernameChars = regexp.MustCompile(`[^a-z0-9_.-]+`)

func (a *API) freeUsername(r *http.Request, pr oauth.Profile, attempt int) (string, error) {
	base := strings.ToLower(strings.SplitN(pr.Email, "@", 2)[0])
	base = strings.Trim(nonUsernameChars.ReplaceAllString(base, "-"), "-_.")
	if len(base) > 24 {
		base = base[:24]
	}
	if len(base) < 3 {
		base += "user"
	}
	cand := base
	if attempt > 0 {
		cand = fmt.Sprintf("%s-%s", base, strings.ToLower(auth.NewRandom(4))[:4])
	}
	taken, err := a.Store.UsernameTaken(r.Context(), cand)
	if err != nil {
		return "", err
	}
	if taken {
		return fmt.Sprintf("%s-%s", base, strings.ToLower(auth.NewRandom(5))[:5]), nil
	}
	return cand, nil
}
