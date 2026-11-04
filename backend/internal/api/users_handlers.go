package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// ---------- user administration ----------

func (a *API) adminListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminAccess(w, r, auth.AdminUsers); !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > maxLoginLen || hasNUL(q) {
		invalid(w, r, "search text is too long")
		return
	}
	us, next, err := a.Store.ListUsers(r.Context(), q, queryInt(r, "limit", 100, 500), r.URL.Query().Get("cursor"))
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	writePage(w, us, next)
}

func (a *API) adminGetUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminAccess(w, r, auth.AdminUsers); !ok {
		return
	}
	u, ok := a.userParam(w, r)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, u)
}

// adminCreateUser creates an account. Without a password the account can only sign in through an
// external provider, which proves control of the address on first sign-in.
func (a *API) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminAccess(w, r, auth.AdminUsers); !ok {
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
	n := store.NewUser{Email: req.Email, Username: req.Username}
	if req.Password != "" {
		if err := auth.ValidatePassword(req.Password, req.Email, req.Username); err != nil {
			invalid(w, r, err.Error())
			return
		}
		h, err := a.Hasher.Hash(r.Context(), req.Password)
		if err != nil {
			a.hashFailed(w, r, err)
			return
		}
		n.PasswordHash = &h
	}
	u, err := a.Store.CreateUser(r.Context(), n)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, nil, "user.create", "user", u.ID.String(), map[string]any{"with_password": n.PasswordHash != nil})
	httpx.WriteJSON(w, http.StatusCreated, u)
}

func (a *API) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.adminAccess(w, r, auth.AdminUsers)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Status          *string `json:"status"`
		IsInstanceAdmin *bool   `json:"is_instance_admin"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch {
	case req.Status == nil && req.IsInstanceAdmin == nil:
		invalid(w, r, "set status or is_instance_admin")
		return
	case req.Status != nil && *req.Status != "active" && *req.Status != "disabled":
		invalid(w, r, "status must be active or disabled")
		return
	case admin != nil && id == admin.ID:
		invalid(w, r, "you cannot change your own status or administrator role")
		return
	}
	if err := a.Store.UpdateUser(r.Context(), id, req.Status, req.IsInstanceAdmin); err != nil {
		a.storeError(w, r, err)
		return
	}
	if req.Status != nil {
		a.audit(r, nil, "user.status", "user", id.String(), map[string]any{"status": *req.Status})
	}
	if req.IsInstanceAdmin != nil {
		a.audit(r, nil, "user.admin", "user", id.String(), map[string]any{"is_instance_admin": *req.IsInstanceAdmin})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.adminAccess(w, r, auth.AdminUsers)
	if !ok {
		return
	}
	u, ok := a.userParam(w, r)
	if !ok {
		return
	}
	if admin != nil && u.ID == admin.ID {
		invalid(w, r, "you cannot delete your own account")
		return
	}
	err := a.Store.DeleteUser(r.Context(), u.ID)
	if errors.Is(err, store.ErrLastOwner) {
		httpx.WriteProblem(w, r, http.StatusConflict, "the user is the only owner of a workspace; make another member owner first")
		return
	}
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	// The row is gone, so the trail keeps who it was.
	a.audit(r, nil, "user.delete", "user", u.ID.String(), map[string]any{"email": u.Email, "username": u.Username})
	w.WriteHeader(http.StatusNoContent)
}

// adminResetPassword sets a new password and signs the user out everywhere.
func (a *API) adminResetPassword(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.adminAccess(w, r, auth.AdminUsers)
	if !ok {
		return
	}
	u, ok := a.userParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if admin != nil && u.ID == admin.ID {
		invalid(w, r, "change your own password from your account")
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
	if err := a.Store.SetPassword(r.Context(), u.ID, h, uuid.Nil); err != nil {
		a.storeError(w, r, err)
		return
	}
	// Guesses against the old password no longer matter.
	a.loginAccountGlobal.Reset("user:" + u.ID.String())
	a.audit(r, nil, "user.password_reset", "user", u.ID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) adminListUserSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminAccess(w, r, auth.AdminUsers); !ok {
		return
	}
	if u, ok := a.userParam(w, r); ok {
		a.writeSessions(w, r, u.ID)
	}
}

func (a *API) adminRevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminAccess(w, r, auth.AdminUsers); !ok {
		return
	}
	if u, ok := a.userParam(w, r); ok {
		a.revokeSessions(w, r, u.ID, "user.sessions_revoke")
	}
}

func (a *API) adminRevokeUserSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminAccess(w, r, auth.AdminUsers); !ok {
		return
	}
	if u, ok := a.userParam(w, r); ok {
		a.revokeSession(w, r, u.ID, "user.session_revoke")
	}
}

// userParam loads the user named by the {id} path parameter, answering 404 when there is none.
func (a *API) userParam(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return store.User{}, false
	}
	u, err := a.Store.UserByID(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return store.User{}, false
	}
	return u, true
}

// ---------- own account ----------

// changePassword needs the current password, so a stolen session alone cannot lock the owner out.
// Guesses share the login counters of the account.
func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decode(w, r, &req) {
		return
	}
	accountIP := "user:" + u.ID.String() + "|" + a.rateIP(r)
	if !a.loginAccountIP.Allow(accountIP) {
		tooMany(w, r, 15*time.Minute)
		return
	}
	hash, err := a.Store.PasswordHash(r.Context(), u.ID)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	if hash == nil {
		httpx.WriteProblem(w, r, http.StatusConflict, "this account signs in through an external provider and has no password")
		return
	}
	if len(req.CurrentPassword) > 1024 {
		req.CurrentPassword = ""
	}
	err = a.Hasher.Verify(r.Context(), req.CurrentPassword, *hash)
	if errors.Is(err, auth.ErrBusy) {
		a.hashFailed(w, r, err)
		return
	}
	if err != nil {
		a.audit(r, nil, "auth.password_change_failed", "user", u.ID.String(), nil)
		httpx.WriteProblem(w, r, http.StatusForbidden, "the current password is not correct")
		return
	}
	if err := auth.ValidatePassword(req.NewPassword, u.Email, u.Username); err != nil {
		invalid(w, r, err.Error())
		return
	}
	h, err := a.Hasher.Hash(r.Context(), req.NewPassword)
	if err != nil {
		a.hashFailed(w, r, err)
		return
	}
	if err := a.Store.SetPassword(r.Context(), u.ID, h, currentSession(r)); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.loginAccountIP.Reset(accountIP)
	a.audit(r, nil, "auth.password_change", "user", u.ID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listOwnSessions(w http.ResponseWriter, r *http.Request) {
	if u, ok := a.requireUser(w, r); ok {
		a.writeSessions(w, r, u.ID)
	}
}

// revokeOtherSessions signs the caller out everywhere else; the current session stays.
func (a *API) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	if u, ok := a.requireUser(w, r); ok {
		a.revokeSessions(w, r, u.ID, "auth.sessions_revoke")
	}
}

func (a *API) revokeOwnSession(w http.ResponseWriter, r *http.Request) {
	if u, ok := a.requireUser(w, r); ok {
		a.revokeSession(w, r, u.ID, "auth.session_revoke")
	}
}

// ---------- sessions, shared by account and administration ----------

// currentSession is the caller's session id, uuid.Nil for token callers.
func currentSession(r *http.Request) uuid.UUID {
	if s := principalOf(r).session; s != nil {
		return s.ID
	}
	return uuid.Nil
}

func (a *API) writeSessions(w http.ResponseWriter, r *http.Request, userID uuid.UUID) {
	ss, err := a.Store.ListSessions(r.Context(), userID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	cur := currentSession(r)
	for i := range ss {
		ss[i].Current = ss[i].ID == cur
	}
	items(w, ss)
}

// revokeSessions ends every session of the user except the caller's own.
func (a *API) revokeSessions(w http.ResponseWriter, r *http.Request, userID uuid.UUID, action string) {
	n, err := a.Store.RevokeSessions(r.Context(), userID, currentSession(r))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	a.audit(r, nil, action, "user", userID.String(), map[string]any{"revoked": n})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"revoked": n})
}

// revokeSession ends the {sessionId} session of the user. Ending the caller's own also drops its cookie.
func (a *API) revokeSession(w http.ResponseWriter, r *http.Request, userID uuid.UUID, action string) {
	sid, ok := idParam(w, r, "sessionId")
	if !ok {
		return
	}
	if err := a.Store.RevokeSession(r.Context(), userID, sid); err != nil {
		a.storeError(w, r, err)
		return
	}
	if sid == currentSession(r) {
		a.clearCookie(w)
	}
	a.audit(r, nil, action, "user", userID.String(), map[string]any{"session_id": sid})
	w.WriteHeader(http.StatusNoContent)
}
