package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/google/uuid"
)

const authChallengeTTL = 5 * time.Minute

func (a *API) checkPassword(w http.ResponseWriter, r *http.Request, u *store.User, password string) bool {
	if !a.loginAccountIP.Allow("security:" + u.ID.String() + "|" + a.rateIP(r)) {
		tooMany(w, r, 15*time.Minute)
		return false
	}
	hash, err := a.Store.PasswordHash(r.Context(), u.ID)
	if err != nil {
		a.internalError(w, r, err)
		return false
	}
	if hash == nil {
		at, err := a.Store.SessionCreatedAt(r.Context(), u.ID, currentSession(r))
		if err != nil {
			a.storeError(w, r, err)
			return false
		}
		if a.now().Sub(at) > authChallengeTTL {
			httpx.WriteProblem(w, r, 409, "sign in again through your external provider before changing security methods")
			return false
		}
		return true
	}
	if len(password) > 1024 || password == "" {
		httpx.WriteProblem(w, r, 401, "invalid credentials")
		return false
	}
	if err := a.Hasher.Verify(r.Context(), password, *hash); err != nil {
		if errors.Is(err, auth.ErrBusy) {
			a.hashFailed(w, r, err)
		} else {
			httpx.WriteProblem(w, r, 401, "invalid credentials")
		}
		return false
	}
	return true
}
func (a *API) userSecurity(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	t, err := a.Store.TOTP(r.Context(), u.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.internalError(w, r, err)
		return
	}
	ps, err := a.Store.ListPasskeys(r.Context(), u.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"totp_enabled": t.Enabled, "passkeys": ps})
}
func (a *API) setupTOTP(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &req) || !a.checkPassword(w, r, u, req.Password) {
		return
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	encrypted, err := auth.Seal(a.secret, []byte(secret), "totp:"+u.ID.String())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.Store.SetupTOTP(r.Context(), u.ID, encrypted); err != nil {
		a.storeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, 200, map[string]string{"secret": secret, "uri": auth.TOTPURI(secret, u.Email)})
}
func (a *API) verifyUserTOTP(ctx context.Context, user uuid.UUID, code string) error {
	t, err := a.Store.TOTP(ctx, user)
	if err != nil {
		return err
	}
	if !t.Enabled {
		return store.ErrNotFound
	}
	secret, err := auth.Open(a.secret, t.Secret, "totp:"+user.String())
	if err != nil {
		return err
	}
	counter, ok := auth.VerifyTOTP(string(secret), code, a.now(), t.LastCounter)
	if !ok {
		if len(code) >= 20 {
			return a.Store.ConsumeRecoveryCode(ctx, user, auth.HashSession(strings.TrimSpace(code)))
		}
		return store.ErrNotFound
	}
	return a.Store.AcceptTOTP(ctx, user, counter, t.Secret)
}
func (a *API) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !a.loginAccountIP.Allow("totp:" + u.ID.String() + "|" + a.rateIP(r)) {
		tooMany(w, r, 15*time.Minute)
		return
	}
	t, err := a.Store.TOTP(r.Context(), u.ID)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	if t.Enabled {
		httpx.WriteProblem(w, r, 409, "TOTP is already enabled")
		return
	}
	secret, err := auth.Open(a.secret, t.Secret, "totp:"+u.ID.String())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	counter, ok := auth.VerifyTOTP(string(secret), req.Code, a.now(), t.LastCounter)
	if !ok {
		a.securityCodeError(w, r, store.ErrNotFound)
		return
	}
	codes := make([]string, 10)
	hashes := make([][]byte, 10)
	for i := range codes {
		codes[i] = auth.NewRandom(18)
		hashes[i] = auth.HashSession(codes[i])
	}
	if err := a.Store.EnableTOTP(r.Context(), u.ID, t.Secret, counter, hashes, currentSession(r)); err != nil {
		a.securityCodeError(w, r, err)
		return
	}
	a.audit(r, nil, "auth.totp_enabled", "user", u.ID.String(), nil)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, 200, map[string]any{"totp_enabled": true, "recovery_codes": codes})
}
func (a *API) securityCodeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteProblem(w, r, 401, "invalid or already used verification code")
		return
	}
	a.internalError(w, r, err)
}
func (a *API) disableTOTP(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &req) || !a.checkPassword(w, r, u, req.Password) {
		return
	}
	if err := a.verifyUserTOTP(r.Context(), u.ID, req.Code); err != nil {
		a.securityCodeError(w, r, err)
		return
	}
	if err := a.Store.DeleteTOTP(r.Context(), u.ID); err != nil {
		a.storeError(w, r, err)
		return
	}
	if err := a.Store.SetRecoveryCodes(r.Context(), u.ID, nil); err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, nil, "auth.totp_disabled", "user", u.ID.String(), nil)
	w.WriteHeader(204)
}
func (a *API) createAuthChallenge(ctx context.Context, user uuid.UUID, purpose string, data json.RawMessage) (string, error) {
	secret, hash := auth.NewSessionSecret()
	err := a.Store.CreateChallenge(ctx, hash, user, purpose, data, a.now().Add(authChallengeTTL))
	return secret, err
}
func (a *API) mfaChallenge(w http.ResponseWriter, r *http.Request, u store.User, redirect bool) bool {
	t, err := a.Store.TOTP(r.Context(), u.ID)
	if errors.Is(err, store.ErrNotFound) || err == nil && !t.Enabled {
		return false
	}
	if err != nil {
		a.internalError(w, r, err)
		return true
	}
	challenge, err := a.createAuthChallenge(r.Context(), u.ID, "mfa", nil)
	if err != nil {
		a.internalError(w, r, err)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	if redirect {
		http.Redirect(w, r, "/login#mfa="+challenge, http.StatusSeeOther)
	} else {
		httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"mfa_required": true, "challenge": challenge})
	}
	return true
}
func (a *API) verifyMFA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Challenge) > 256 || !a.loginIP.Allow(a.rateIP(r)) {
		tooMany(w, r, time.Minute)
		return
	}
	hash := auth.HashSession(req.Challenge)
	c, err := a.Store.AttemptChallenge(r.Context(), hash, "mfa")
	if err != nil {
		a.securityCodeError(w, r, err)
		return
	}
	if err := a.verifyUserTOTP(r.Context(), c.UserID, req.Code); err != nil {
		a.securityCodeError(w, r, err)
		return
	}
	if err := a.Store.ConsumeChallenge(r.Context(), hash); err != nil {
		a.securityCodeError(w, r, err)
		return
	}
	u, err := a.Store.UserByID(r.Context(), c.UserID)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	if u.Status != "active" {
		httpx.WriteProblem(w, r, 401, "invalid credentials")
		return
	}
	if !a.startSession(w, r, u) {
		return
	}
	a.audit(r, nil, "auth.login", "user", u.ID.String(), map[string]any{"method": "totp"})
	httpx.WriteJSON(w, 200, a.meBody(r.Context(), u, principalOf(r)))
}
func (a *API) rotateToken(w http.ResponseWriter, r *http.Request) {
	u, ok := a.caller(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	t, err := a.Store.GetToken(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	m, ok := a.workspaceAccess(w, r, u, t.WorkspaceID, auth.TokensManage)
	if !ok {
		return
	}
	for _, scope := range t.Scopes {
		if !m.Perms.Has(scope) {
			httpx.WriteProblem(w, r, 403, "you cannot rotate a token with a scope you do not hold")
			return
		}
	}
	plaintext, prefix, hash := auth.NewAPIToken(a.secret)
	out, err := a.Store.RotateToken(r.Context(), t.WorkspaceID, t.ID, userRef(u), prefix, hash)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &t.WorkspaceID, "token.rotate", "token", t.ID.String(), map[string]any{"replacement_id": out.ID})
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, 201, map[string]any{"token": plaintext, "api_token": out})
}
