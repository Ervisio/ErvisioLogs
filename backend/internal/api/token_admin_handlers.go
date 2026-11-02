package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// ---------- API tokens ----------

func (a *API) createToken(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		ProjectID uuid.UUID  `json:"project_id"`
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !decode(w, r, &req) || !validName(w, r, strings.TrimSpace(req.Name)) {
		return
	}
	p, err := a.Store.GetProject(r.Context(), req.ProjectID)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	m, ok := a.workspaceAccess(w, r, u, p.WorkspaceID, auth.TokensManage)
	if !ok {
		return
	}
	if len(req.Scopes) == 0 || len(req.Scopes) > len(auth.TokenScopes) {
		invalid(w, r, "scopes must list at least one of logs:ingest, logs:read, logs:export")
		return
	}
	seen := map[string]bool{}
	for _, sc := range req.Scopes {
		switch {
		case !auth.TokenScopes[sc]:
			invalid(w, r, "unknown token scope "+sc)
			return
		case !m.Perms.Has(sc):
			httpx.WriteProblem(w, r, http.StatusForbidden, "you cannot grant a scope you do not hold: "+sc)
			return
		case seen[sc]:
			invalid(w, r, "duplicate scope "+sc)
			return
		}
		seen[sc] = true
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(a.now()) {
		invalid(w, r, "expires_at must be in the future")
		return
	}
	tok, prefix, hash := auth.NewAPIToken(a.secret)
	t, err := a.Store.CreateToken(r.Context(), p.WorkspaceID, p.ID, u.ID, strings.TrimSpace(req.Name), prefix, hash, req.Scopes, req.ExpiresAt)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "token.create", "token", t.ID.String(), map[string]any{"scopes": t.Scopes, "project_id": p.ID})
	// The plaintext token appears in this response only.
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"token": tok, "api_token": t})
}

func (a *API) listTokens(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	pid, err := uuid.Parse(r.URL.Query().Get("project_id"))
	if err != nil {
		invalid(w, r, "project_id query parameter is required")
		return
	}
	p, err := a.Store.GetProject(r.Context(), pid)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	if _, ok := a.workspaceAccess(w, r, u, p.WorkspaceID, auth.TokensManage); !ok {
		return
	}
	ts, err := a.Store.ListTokens(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, ts)
}

func (a *API) revokeToken(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
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
	if _, ok := a.workspaceAccess(w, r, u, t.WorkspaceID, auth.TokensManage); !ok {
		return
	}
	if err := a.Store.RevokeToken(r.Context(), t.WorkspaceID, t.ID); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &t.WorkspaceID, "token.revoke", "token", t.ID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// currentToken describes the API token used for the call. Ingestion and search (P2) will authenticate
// the same way; this endpoint lets clients and tests confirm a token is valid and see its effective
// scopes (the stored ones its creator's current role still allows).
func (a *API) currentToken(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r)
	if p.token == nil {
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "an API token is required")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p.token)
}

// ---------- instance administration ----------

func (a *API) adminListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	us, err := a.Store.ListUsers(r.Context(), queryInt(r, "limit", 100, 500))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, us)
}

// adminCreateUser creates an account. Without a password the account can only sign in through an
// external provider, which proves control of the address on first sign-in.
func (a *API) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
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

// adminListAudit returns the instance-level trail: sign-ins, account changes, deleted workspaces.
func (a *API) adminListAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	events, next, err := a.Store.ListInstanceAudit(r.Context(), queryInt(r, "limit", 50, 200), r.URL.Query().Get("cursor"))
	a.writeAuditPage(w, r, events, next, err)
}

func (a *API) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	admin, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Status != "active" && req.Status != "disabled" {
		invalid(w, r, "status must be active or disabled")
		return
	}
	if id == admin.ID {
		invalid(w, r, "you cannot change your own status")
		return
	}
	if err := a.Store.SetUserStatus(r.Context(), id, req.Status); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, nil, "user.status", "user", id.String(), map[string]any{"status": req.Status})
	w.WriteHeader(http.StatusNoContent)
}
