package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/archive"
	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/mail"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// Account email: password reset, address verification and workspace invitations. Links carry the
// token in the URL fragment and the web app posts it back in a body, so it never reaches a log.

const (
	resetTTL      = time.Hour
	verifyTTL     = 48 * time.Hour
	invitationTTL = 7 * 24 * time.Hour
)

// Mail enables account email. Without it, or without an SMTP host and sender in the settings, email is off.
type Mail struct {
	Sender mail.Sender
	// Password is SMTP_PASSWORD from the environment; without it the password file setting is read.
	Password string
}

func (a *API) mailEnabled() bool {
	return a.Mail != nil && a.Settings.String(settings.SMTPHost) != "" && a.Settings.String(settings.MailFrom) != ""
}

// mailConfig is the SMTP configuration in force. Callers check mailEnabled first.
func (a *API) mailConfig() (mail.Config, error) {
	c := mail.Config{
		Host:     a.Settings.String(settings.SMTPHost),
		Port:     int(a.Settings.Int(settings.SMTPPort)),
		Security: a.Settings.String(settings.SMTPSecurity),
		Username: a.Settings.String(settings.SMTPUsername),
		Password: a.Mail.Password,
		From:     a.Settings.String(settings.MailFrom),
	}
	if name := a.Settings.String(settings.SMTPPasswordFile); c.Password == "" && name != "" {
		if a.Storage == nil || a.Storage.SecretsDir == "" || !archive.ValidSecretFile(name) {
			return c, errors.New("the SMTP password file cannot be read: no secrets directory or an invalid name")
		}
		b, err := os.ReadFile(filepath.Join(a.Storage.SecretsDir, name))
		if err != nil {
			return c, fmt.Errorf("SMTP password file: %w", err)
		}
		c.Password = strings.TrimSpace(string(b))
	}
	return c, nil
}

func (a *API) sendMail(ctx context.Context, m mail.Message) error {
	c, err := a.mailConfig()
	if err != nil {
		return err
	}
	return a.Mail.Sender.Send(ctx, c, m)
}

// sendMailLater delivers in the background: the answer must neither wait on the delivery nor reveal,
// by its timing, whether there was anything to send.
func (a *API) sendMailLater(kind string, m mail.Message) {
	go func() {
		if err := a.sendMail(context.Background(), m); err != nil {
			a.Log.Warn("could not send email", "kind", kind, "error", err)
		}
	}()
}

func (a *API) link(page, token string) string { return a.origin + "/" + page + "#token=" + token }

// sendVerification mails a new verification link to the user, replacing the earlier one.
func (a *API) sendVerification(ctx context.Context, u store.User) error {
	secret, hash := auth.NewSessionSecret()
	if err := a.Store.CreateAccountToken(ctx, u.ID, store.PurposeVerify, hash, a.now().Add(verifyTTL)); err != nil {
		return err
	}
	a.sendMailLater("email_verify", mail.Message{To: u.Email, Subject: "Confirm your email address",
		Body: "Hello " + u.Username + ",\n\nopen this link to confirm your email address for Ervisio Logs:\n\n" +
			a.link("verify-email", secret) + "\n\nThe link works once and expires in 48 hours."})
	return nil
}

// ---------- password reset ----------

// forgotPassword answers 202 whether or not the address has an account, so it reveals none.
func (a *API) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !a.mailEnabled() {
		httpx.WriteProblem(w, r, http.StatusConflict, "email is not configured; ask an administrator to reset your password")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if len(email) > 254 || !emailRe.MatchString(email) {
		invalid(w, r, "enter a valid email address")
		return
	}
	if !a.forgotIP.Allow(a.rateIP(r)) || !a.forgotEmail.Allow(email) {
		tooMany(w, r, time.Hour)
		return
	}
	a.purgeExpired(r.Context())
	u, err := a.Store.UserByEmail(r.Context(), email)
	if err == nil && u.Status == "active" {
		secret, hash := auth.NewSessionSecret()
		if err = a.Store.CreateAccountToken(r.Context(), u.ID, store.PurposeReset, hash, a.now().Add(resetTTL)); err == nil {
			a.audit(r, nil, "auth.password_reset_request", "user", u.ID.String(), nil)
			a.sendMailLater("password_reset", mail.Message{To: u.Email, Subject: "Reset your password",
				Body: "Hello " + u.Username + ",\n\nopen this link to choose a new Ervisio Logs password:\n\n" +
					a.link("reset-password", secret) + "\n\nThe link works once and expires in one hour. " +
					"If you did not ask for it, ignore this email: your password stays as it is."})
		}
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// resetPassword sets the password chosen through a reset link and signs the user out everywhere.
func (a *API) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	hash := auth.HashSession(req.Token)
	u, err := a.Store.AccountTokenUser(r.Context(), hash, store.PurposeReset)
	if errors.Is(err, store.ErrNotFound) {
		linkGone(w, r)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := auth.ValidatePassword(req.Password, u.Email, u.Username); err != nil {
		invalid(w, r, err.Error())
		return
	}
	h, err := a.Hasher.Hash(r.Context(), req.Password)
	if err != nil {
		a.hashFailed(w, r, err)
		return
	}
	err = a.Store.ResetPassword(r.Context(), hash, u.ID, h)
	if errors.Is(err, store.ErrNotFound) {
		linkGone(w, r)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.loginAccountGlobal.Reset("user:" + u.ID.String())
	a.audit(r, nil, "auth.password_reset", "user", u.ID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---------- email verification ----------

func (a *API) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	u, err := a.Store.VerifyEmail(r.Context(), auth.HashSession(req.Token))
	if errors.Is(err, store.ErrNotFound) {
		linkGone(w, r)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, nil, "auth.email_verify", "user", u.ID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) resendVerification(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	switch {
	case u.EmailVerified:
		httpx.WriteProblem(w, r, http.StatusConflict, "your email address is already verified")
		return
	case !a.mailEnabled():
		httpx.WriteProblem(w, r, http.StatusConflict, "email is not configured on this instance")
		return
	case !a.verifyUser.Allow(u.ID.String()):
		tooMany(w, r, time.Hour)
		return
	}
	if err := a.sendVerification(r.Context(), *u); err != nil {
		a.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ---------- invitations ----------

// invitationResp carries the link only when it was not emailed: an emailed link must reach the
// invited address alone, since accepting it verifies that address.
type invitationResp struct {
	store.Invitation
	URL       string `json:"url,omitempty"`
	MailError string `json:"mail_error,omitempty"`
}

// createInvitation invites an address whether or not it has an account, so the answer reveals neither.
func (a *API) createInvitation(w http.ResponseWriter, r *http.Request) {
	u, ok := a.caller(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	m, ok := a.workspaceAccess(w, r, u, id, auth.MembersManage)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !roleNames[req.Role] {
		invalid(w, r, "role must be one of owner, admin, developer, analyst, viewer")
		return
	}
	if !canGrant(m, req.Role) {
		httpx.WriteProblem(w, r, http.StatusForbidden, "only an owner can grant the owner role")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if len(email) > 254 || !emailRe.MatchString(email) {
		invalid(w, r, "enter a valid email address")
		return
	}
	ws, err := a.Store.GetWorkspace(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.purgeExpired(r.Context())
	secret, hash := auth.NewSessionSecret()
	inv, err := a.Store.CreateInvitation(r.Context(), id, userRef(u), email, req.Role, hash, a.now().Add(invitationTTL))
	if errors.Is(err, store.ErrConflict) {
		httpx.WriteProblem(w, r, http.StatusConflict, "this address already belongs to a member")
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	resp := invitationResp{Invitation: inv}
	if a.mailEnabled() {
		inviter := "An administrator"
		if u != nil {
			inviter = u.Username
		}
		err = a.sendMail(r.Context(), mail.Message{To: email, Subject: "Join " + ws.Name + " on Ervisio Logs",
			Body: inviter + " invited you to the workspace " + ws.Name + " on Ervisio Logs as " + req.Role + ".\n\n" +
				"Open this link to accept, with your account or a new one:\n\n" + a.link("invite", secret) +
				"\n\nThe link works once and expires in 7 days."})
		if err == nil {
			err = a.Store.MarkInvitationEmailed(r.Context(), hash)
		}
		if err == nil {
			resp.Emailed = true
		} else {
			a.Log.Warn("could not email an invitation", "request_id", httpx.RequestID(r.Context()), "error", err)
			resp.MailError = "the email could not be sent; share the link yourself"
		}
	}
	if !resp.Emailed {
		resp.URL = a.link("invite", secret)
	}
	a.audit(r, &id, "invitation.create", "invitation", inv.ID.String(), map[string]any{"email": email, "role": req.Role, "emailed": resp.Emailed})
	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (a *API) listInvitations(w http.ResponseWriter, r *http.Request) {
	u, ok := a.caller(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := a.workspaceAccess(w, r, u, id, auth.MembersManage); !ok {
		return
	}
	invs, err := a.Store.ListInvitations(r.Context(), id)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, invs)
}

func (a *API) deleteInvitation(w http.ResponseWriter, r *http.Request) {
	u, ok := a.caller(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	invID, ok := idParam(w, r, "invitationId")
	if !ok {
		return
	}
	if _, ok := a.workspaceAccess(w, r, u, id, auth.MembersManage); !ok {
		return
	}
	if err := a.Store.DeleteInvitation(r.Context(), id, invID); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &id, "invitation.delete", "invitation", invID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

type invitationTokenReq struct {
	Token string `json:"token"`
}

// previewInvitation tells the holder of a link what it is for and whether to sign in or sign up.
func (a *API) previewInvitation(w http.ResponseWriter, r *http.Request) {
	var req invitationTokenReq
	if !decode(w, r, &req) {
		return
	}
	p, err := a.Store.PreviewInvitation(r.Context(), auth.HashSession(req.Token))
	if errors.Is(err, store.ErrNotFound) {
		invitationGone(w, r)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p)
}

// acceptInvitation adds the signed-in user, whose address must be the invited one.
func (a *API) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req invitationTokenReq
	if !decode(w, r, &req) {
		return
	}
	inv, err := a.Store.AcceptInvitation(r.Context(), auth.HashSession(req.Token), u.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		invitationGone(w, r)
		return
	case errors.Is(err, store.ErrForbidden):
		httpx.WriteProblem(w, r, http.StatusForbidden, "this invitation is for another email address; sign in with that account")
		return
	case err != nil:
		a.internalError(w, r, err)
		return
	}
	a.audit(r, &inv.WorkspaceID, "invitation.accept", "user", u.ID.String(), map[string]any{"role": inv.Role})
	a.writeJoined(w, r, inv)
}

// registerWithInvitation creates the account of an invited address, even with registration closed.
func (a *API) registerWithInvitation(w http.ResponseWriter, r *http.Request) {
	if !a.registerIP.Allow(a.rateIP(r)) {
		tooMany(w, r, time.Hour)
		return
	}
	var req struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	hash := auth.HashSession(req.Token)
	p, err := a.Store.PreviewInvitation(r.Context(), hash)
	if errors.Is(err, store.ErrNotFound) {
		invitationGone(w, r)
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if !usernameRe.MatchString(req.Username) {
		invalid(w, r, "username must be 3 to 32 characters: lowercase letters, digits, dot, dash or underscore")
		return
	}
	if err := auth.ValidatePassword(req.Password, p.Email, req.Username); err != nil {
		invalid(w, r, err.Error())
		return
	}
	h, err := a.Hasher.Hash(r.Context(), req.Password)
	if err != nil {
		a.hashFailed(w, r, err)
		return
	}
	u, inv, err := a.Store.AcceptInvitationNewUser(r.Context(), hash, store.NewUser{Username: req.Username, PasswordHash: &h})
	switch {
	case errors.Is(err, store.ErrNotFound):
		invitationGone(w, r)
		return
	case errors.Is(err, store.ErrConflict):
		httpx.WriteProblem(w, r, http.StatusConflict, "email or username already in use; if you have an account, sign in to accept")
		return
	case err != nil:
		a.internalError(w, r, err)
		return
	}
	a.audit(r, &inv.WorkspaceID, "auth.register", "user", u.ID.String(), map[string]any{"invitation": inv.ID})
	if !a.startSession(w, r, u) {
		return
	}
	a.audit(r, &inv.WorkspaceID, "invitation.accept", "user", u.ID.String(), map[string]any{"role": inv.Role})
	if !u.EmailVerified && a.mailEnabled() {
		if err := a.sendVerification(r.Context(), u); err != nil {
			a.Log.Warn("could not start email verification", "user_id", u.ID, "error", err)
		}
	}
	httpx.WriteJSON(w, http.StatusCreated, a.meBody(r.Context(), u, principalOf(r)))
}

func (a *API) writeJoined(w http.ResponseWriter, r *http.Request, inv store.Invitation) {
	ws, err := a.Store.GetWorkspace(r.Context(), inv.WorkspaceID)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	// An existing membership was kept, so the role may not be the invited one.
	m, err := a.Store.MembershipOf(r.Context(), inv.WorkspaceID, principalOf(r).user.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	ws.Role = m.Role
	httpx.WriteJSON(w, http.StatusOK, ws)
}

func linkGone(w http.ResponseWriter, r *http.Request) {
	httpx.WriteProblem(w, r, http.StatusGone, "this link is invalid, used or expired; ask for a new one")
}

func invitationGone(w http.ResponseWriter, r *http.Request) {
	httpx.WriteProblem(w, r, http.StatusGone, "this invitation is invalid, used, revoked or expired")
}
