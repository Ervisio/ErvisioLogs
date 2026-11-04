package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

var (
	emailRe    = regexp.MustCompile(`^[a-z0-9.!#$%&'*+/=?^_{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
	usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,31}$`)
)

// maxLoginLen is above the longest email (254) and username accepted at registration.
const maxLoginLen = 320

// hasNUL reports a NUL character: valid in JSON, refused by PostgreSQL text.
func hasNUL(s string) bool { return strings.IndexByte(s, 0) >= 0 }

// setupRequired reports whether the instance has no account yet. Callers must fail on error: guessing
// "already set up" could create a first account that is not the administrator.
func (a *API) setupRequired(ctx context.Context) (bool, error) {
	n, err := a.countUsers(ctx)
	return err == nil && n == 0, err
}

func (a *API) listProviders(w http.ResponseWriter, r *http.Request) {
	setup, err := a.setupRequired(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	names := make([]string, 0, len(a.Providers))
	for n := range a.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"local":             true,
		"providers":         names,
		"open_registration": a.Settings.Bool(settings.OpenRegistration),
		"setup_required":    setup,
	})
}

type registerReq struct {
	Email     string `json:"email"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	SetupCode string `json:"setup_code"`
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	if !a.registerIP.Allow(a.rateIP(r)) {
		tooMany(w, r, time.Hour)
		return
	}
	var req registerReq
	if !decode(w, r, &req) {
		return
	}
	req.Email, req.Username = strings.ToLower(strings.TrimSpace(req.Email)), strings.ToLower(strings.TrimSpace(req.Username))
	if len(req.Email) > 254 || !emailRe.MatchString(req.Email) {
		invalid(w, r, "enter a valid email address")
		return
	}
	if !usernameRe.MatchString(req.Username) {
		invalid(w, r, "username must be 3 to 32 characters: lowercase letters, digits, dot, dash or underscore")
		return
	}
	if err := auth.ValidatePassword(req.Password, req.Email, req.Username); err != nil {
		invalid(w, r, err.Error())
		return
	}

	setup, err := a.setupRequired(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if setup {
		if !a.setupCodeMatches(r.Context(), req.SetupCode) {
			httpx.WriteProblem(w, r, http.StatusForbidden, "a valid setup code is required to create the first account")
			return
		}
	} else if !a.Settings.Bool(settings.OpenRegistration) {
		httpx.WriteProblem(w, r, http.StatusForbidden, "registration is closed; ask an administrator to create your account")
		return
	}

	hash, err := a.Hasher.Hash(r.Context(), req.Password)
	if err != nil {
		a.hashFailed(w, r, err)
		return
	}
	n := store.NewUser{Email: req.Email, Username: req.Username, PasswordHash: &hash}
	var u store.User
	if setup {
		u, err = a.Store.CreateFirstAdmin(r.Context(), n)
	} else {
		u, err = a.Store.CreateUser(r.Context(), n)
	}
	switch {
	case errors.Is(err, store.ErrSetupDone):
		httpx.WriteProblem(w, r, http.StatusForbidden, "setup is already completed")
		return
	case errors.Is(err, store.ErrConflict):
		httpx.WriteProblem(w, r, http.StatusConflict, "email or username already in use")
		return
	case err != nil:
		a.internalError(w, r, err)
		return
	}
	a.audit(r, nil, "auth.register", "user", u.ID.String(), map[string]any{"first_admin": setup})
	if setup {
		a.clearSetupCode() // single use
	}
	if !a.startSession(w, r, u) {
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a.meBody(r.Context(), u, principalOf(r)))
}

type loginReq struct {
	Login    string `json:"login"` // email or username
	Password string `json:"password"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !decode(w, r, &req) {
		return
	}
	login := strings.ToLower(strings.TrimSpace(req.Login))
	ip := a.rateIP(r)
	if !a.loginIP.Allow(ip) {
		a.loginThrottled(w, r, time.Minute)
		return
	}
	if login == "" || len(login) > maxLoginLen || hasNUL(login) || req.Password == "" || len(req.Password) > 1024 {
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "invalid credentials")
		return
	}
	u, hash, err := a.Store.UserForLogin(r.Context(), login)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.internalError(w, r, err)
		return
	}
	// Email and username of one account share its counters. An unknown login is counted as typed, and
	// recorded only as a digest: people do paste passwords into the login field.
	account, targetType, targetID := "login:"+login, "login", a.keyedDigest("audit-login", login)[:22]
	if err == nil {
		account, targetType, targetID = "user:"+u.ID.String(), "user", u.ID.String()
	}
	accountIP := account + "|" + ip
	if !a.loginAccountIP.Allow(accountIP) || !a.loginAccountGlobal.Allow(account) {
		a.loginThrottled(w, r, 15*time.Minute)
		return
	}
	// Same work and same answer whether the account is unknown, has no password, is disabled or the
	// password is wrong, so the response does not reveal which accounts exist.
	var hashErr error
	if err == nil && hash != nil && u.Status == "active" {
		hashErr = a.Hasher.Verify(r.Context(), req.Password, *hash)
	} else if hashErr = a.Hasher.Burn(r.Context(), req.Password); hashErr == nil {
		hashErr = auth.ErrMismatch
	}
	if errors.Is(hashErr, auth.ErrBusy) {
		a.hashFailed(w, r, hashErr)
		return
	}
	if hashErr != nil {
		a.audit(r, nil, "auth.login_failed", targetType, targetID, nil)
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "invalid credentials")
		return
	}
	a.loginAccountIP.Reset(accountIP)
	if !a.startSession(w, r, u) {
		return
	}
	a.audit(r, nil, "auth.login", "user", u.ID.String(), map[string]any{"method": "password"})
	httpx.WriteJSON(w, http.StatusOK, a.meBody(r.Context(), u, principalOf(r)))
}

// loginThrottled answers 429 and only logs: an audit row per blocked request would let anyone grow
// the table at will.
func (a *API) loginThrottled(w http.ResponseWriter, r *http.Request, retry time.Duration) {
	a.Log.Warn("login throttled", "request_id", httpx.RequestID(r.Context()), "ip", a.clientIP(r))
	tooMany(w, r, retry)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	if p.session != nil {
		// Already revoked elsewhere (another tab, an administrator): signing out still succeeds.
		if err := a.Store.RevokeSession(r.Context(), p.user.ID, p.session.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			a.internalError(w, r, err)
			return
		}
		a.audit(r, nil, "auth.logout", "user", p.user.ID.String(), nil)
	}
	a.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.meBody(r.Context(), *u, principalOf(r)))
}

func (a *API) meBody(ctx context.Context, u store.User, p principal) map[string]any {
	ws, _ := a.Store.WorkspacesOf(ctx, u.ID)
	body := map[string]any{"user": u, "workspaces": ws}
	if p.session != nil {
		body["csrf_token"] = p.session.CSRFToken
	}
	return body
}

// startSession creates a session for u and sets the cookie. The principal in the request context is
// replaced so the response body can carry the new CSRF token.
func (a *API) startSession(w http.ResponseWriter, r *http.Request, u store.User) bool {
	secret, hash := auth.NewSessionSecret()
	csrf := auth.NewCSRFToken()
	exp := a.now().Add(a.Settings.Duration(settings.SessionTTL))
	a.purgeExpired(r.Context())
	id, err := a.Store.CreateSession(r.Context(), u.ID, hash, csrf, exp, r.UserAgent(), a.clientIP(r))
	if err != nil {
		a.internalError(w, r, err)
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: secret, Path: "/", Expires: exp,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
	*r = *r.WithContext(context.WithValue(r.Context(), ctxKey{},
		principal{user: &u, session: &store.Session{ID: id, UserID: u.ID, CSRFToken: csrf, ExpiresAt: exp, User: u}}))
	return true
}

func (a *API) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
}

func tooMany(w http.ResponseWriter, r *http.Request, retry time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
	httpx.WriteProblem(w, r, http.StatusTooManyRequests, "too many attempts, try again later")
}
