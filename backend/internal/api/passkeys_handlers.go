package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const ceremonyCookie = "ervisio_logs_ceremony"

type passkeyUser struct {
	store.User
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.ID[:] }
func (u passkeyUser) WebAuthnName() string                       { return u.Username }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.Username }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

type passkeyCeremony struct {
	Session webauthn.SessionData `json:"session"`
	Name    string               `json:"name,omitempty"`
}

func (a *API) webAuthn() (*webauthn.WebAuthn, error) {
	origin, err := url.Parse(a.origin)
	if err != nil {
		return nil, err
	}
	if origin.Scheme != "https" && origin.Hostname() != "localhost" {
		return nil, errors.New("passkeys require HTTPS or localhost")
	}
	return webauthn.New(&webauthn.Config{RPID: origin.Hostname(), RPDisplayName: "Ervisio Logs", RPOrigins: []string{a.origin}, AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired}, Timeouts: webauthn.TimeoutsConfig{Login: webauthn.TimeoutConfig{Enforce: true, Timeout: authChallengeTTL}, Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: authChallengeTTL}}})
}
func (a *API) loadPasskeyUser(r *http.Request, u store.User) (passkeyUser, []store.Passkey, error) {
	ps, err := a.Store.ListPasskeys(r.Context(), u.ID)
	if err != nil {
		return passkeyUser{}, nil, err
	}
	out := passkeyUser{User: u, credentials: []webauthn.Credential{}}
	for _, p := range ps {
		var c webauthn.Credential
		if err := json.Unmarshal(p.Credential, &c); err != nil {
			return out, nil, err
		}
		out.credentials = append(out.credentials, c)
	}
	return out, ps, nil
}
func (a *API) ceremonyCookie(w http.ResponseWriter, challenge string) {
	http.SetCookie(w, &http.Cookie{Name: ceremonyCookie, Value: challenge, Path: "/api/v1/auth/passkeys", MaxAge: int(authChallengeTTL.Seconds()), HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode})
}
func (a *API) ceremonyBound(r *http.Request, challenge string) bool {
	c, err := r.Cookie(ceremonyCookie)
	return err == nil && len(challenge) > 0 && len(challenge) < 256 && subtle.ConstantTimeCompare([]byte(c.Value), []byte(challenge)) == 1
}
func (a *API) beginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
		Name     string `json:"name"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &req) || !a.checkPassword(w, r, u, req.Password) {
		return
	}
	if !a.securityTOTPProof(w, r, u, req.Code) {
		return
	}
	name, ok := trimmedName(w, r, req.Name)
	if !ok {
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		httpx.WriteProblem(w, r, 409, err.Error())
		return
	}
	pu, ps, err := a.loadPasskeyUser(r, *u)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if len(ps) >= 20 {
		invalid(w, r, "at most 20 passkeys per account")
		return
	}
	exclusions := make([]protocol.CredentialDescriptor, 0, len(pu.credentials))
	for _, c := range pu.credentials {
		exclusions = append(exclusions, c.Descriptor())
	}
	options, session, err := wa.BeginRegistration(pu, webauthn.WithExclusions(exclusions), webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	data, err := json.Marshal(passkeyCeremony{Session: *session, Name: name})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	challenge, err := a.createAuthChallenge(r.Context(), u.ID, "passkey_register", data)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.ceremonyCookie(w, challenge)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, 200, map[string]any{"challenge": challenge, "options": options})
}
func (a *API) finishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Challenge  string          `json:"challenge"`
		Credential json.RawMessage `json:"credential"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !a.ceremonyBound(r, req.Challenge) {
		httpx.WriteProblem(w, r, 401, "invalid passkey ceremony")
		return
	}
	hash := auth.HashSession(req.Challenge)
	c, err := a.Store.AttemptChallenge(r.Context(), hash, "passkey_register")
	if err != nil {
		a.securityCodeError(w, r, err)
		return
	}
	if c.UserID != u.ID {
		httpx.WriteProblem(w, r, 401, "invalid passkey ceremony")
		return
	}
	var saved passkeyCeremony
	if err := json.Unmarshal(c.Data, &saved); err != nil {
		a.internalError(w, r, err)
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	pu, _, err := a.loadPasskeyUser(r, *u)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		invalid(w, r, "invalid passkey response")
		return
	}
	credential, err := wa.CreateCredential(pu, saved.Session, parsed)
	if err != nil {
		httpx.WriteProblem(w, r, 401, "passkey verification failed")
		return
	}
	if err := a.Store.ConsumeChallenge(r.Context(), hash); err != nil {
		a.securityCodeError(w, r, err)
		return
	}
	data, err := json.Marshal(credential)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	p, err := a.Store.CreatePasskey(r.Context(), store.Passkey{UserID: u.ID, Name: saved.Name, CredentialID: credential.ID, Credential: data})
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, nil, "auth.passkey_created", "passkey", p.ID.String(), nil)
	httpx.WriteJSON(w, 201, p)
}
func (a *API) beginPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login string `json:"login"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !a.loginIP.Allow(a.rateIP(r)) {
		tooMany(w, r, time.Minute)
		return
	}
	login := strings.ToLower(strings.TrimSpace(req.Login))
	if login == "" || len(login) > maxLoginLen || hasNUL(login) {
		httpx.WriteProblem(w, r, 401, "passkey sign-in is unavailable for these credentials")
		return
	}
	u, _, err := a.Store.UserForLogin(r.Context(), login)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.WriteProblem(w, r, 401, "passkey sign-in is unavailable for these credentials")
		} else {
			a.internalError(w, r, err)
		}
		return
	}
	if u.Status != "active" {
		httpx.WriteProblem(w, r, 401, "passkey sign-in is unavailable for these credentials")
		return
	}
	if !a.loginAccountIP.Allow("passkey:" + u.ID.String() + "|" + a.rateIP(r)) {
		tooMany(w, r, 15*time.Minute)
		return
	}
	pu, _, err := a.loadPasskeyUser(r, u)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if len(pu.credentials) == 0 {
		httpx.WriteProblem(w, r, 401, "passkey sign-in is unavailable for these credentials")
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		httpx.WriteProblem(w, r, 409, err.Error())
		return
	}
	options, session, err := wa.BeginLogin(pu, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	data, err := json.Marshal(passkeyCeremony{Session: *session})
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	challenge, err := a.createAuthChallenge(r.Context(), u.ID, "passkey_login", data)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.ceremonyCookie(w, challenge)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, 200, map[string]any{"challenge": challenge, "options": options})
}
func (a *API) finishPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Challenge  string          `json:"challenge"`
		Credential json.RawMessage `json:"credential"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !a.loginIP.Allow(a.rateIP(r)) {
		tooMany(w, r, time.Minute)
		return
	}
	if !a.ceremonyBound(r, req.Challenge) {
		httpx.WriteProblem(w, r, 401, "invalid passkey ceremony")
		return
	}
	hash := auth.HashSession(req.Challenge)
	c, err := a.Store.AttemptChallenge(r.Context(), hash, "passkey_login")
	if err != nil {
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
	var saved passkeyCeremony
	if err := json.Unmarshal(c.Data, &saved); err != nil {
		a.internalError(w, r, err)
		return
	}
	pu, ps, err := a.loadPasskeyUser(r, u)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	wa, err := a.webAuthn()
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(req.Credential)
	if err != nil {
		invalid(w, r, "invalid passkey response")
		return
	}
	credential, err := wa.ValidateLogin(pu, saved.Session, parsed)
	if err != nil || credential.Authenticator.CloneWarning {
		httpx.WriteProblem(w, r, 401, "passkey verification failed")
		return
	}
	if err := a.Store.ConsumeChallenge(r.Context(), hash); err != nil {
		a.securityCodeError(w, r, err)
		return
	}
	var old json.RawMessage
	for _, p := range ps {
		if bytes.Equal(p.CredentialID, credential.ID) {
			old = p.Credential
			break
		}
	}
	data, err := json.Marshal(credential)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.Store.UpdatePasskey(r.Context(), u.ID, credential.ID, old, data); err != nil {
		a.securityCodeError(w, r, err)
		return
	}
	if !a.startSession(w, r, u) {
		return
	}
	a.audit(r, nil, "auth.login", "user", u.ID.String(), map[string]any{"method": "passkey"})
	httpx.WriteJSON(w, 200, a.meBody(r.Context(), u, principalOf(r)))
}
func (a *API) securityTOTPProof(w http.ResponseWriter, r *http.Request, u *store.User, code string) bool {
	t, err := a.Store.TOTP(r.Context(), u.ID)
	if errors.Is(err, store.ErrNotFound) || err == nil && !t.Enabled {
		return true
	}
	if err != nil {
		a.internalError(w, r, err)
		return false
	}
	if err := a.verifyUserTOTP(r.Context(), u.ID, code); err != nil {
		a.securityCodeError(w, r, err)
		return false
	}
	return true
}
func (a *API) deletePasskey(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &req) || !a.checkPassword(w, r, u, req.Password) || !a.securityTOTPProof(w, r, u, req.Code) {
		return
	}
	if err := a.Store.DeletePasskey(r.Context(), u.ID, id); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, nil, "auth.passkey_deleted", "passkey", id.String(), nil)
	w.WriteHeader(204)
}
