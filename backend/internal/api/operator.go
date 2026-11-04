package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// operator manages the instance without being a user: the local admin socket or an instance admin
// token (ADR 0019). It holds admin:* scopes and, in every workspace, auth.OperatorPermissions only.
type operator struct {
	name    string
	tokenID *uuid.UUID
	scopes  auth.PermSet
}

// maxAdminTokenTTL caps an admin token's life: it must expire.
const maxAdminTokenTTL = 365 * 24 * time.Hour

type socketKey struct{}

// Socket marks requests that came through the local admin socket. Reaching the socket is the
// authorization, as with docker.sock: wrap only the handler served on it.
func Socket(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), socketKey{}, true)))
	})
}

func socketOperator() *operator {
	scopes := auth.PermSet{}
	for s := range auth.AdminScopes {
		scopes[s] = true
	}
	return &operator{name: "plugin", scopes: scopes}
}

func (a *API) adminTokenOperator(ctx context.Context, tok string) (*operator, error) {
	t, err := a.Store.AdminTokenByHash(ctx, auth.HashAPIToken(a.secret, tok))
	if err != nil {
		return nil, err
	}
	return &operator{name: t.Name, tokenID: &t.ID, scopes: auth.NewPermSet(t.Scopes)}, nil
}

// operatorAllowed answers 403 unless the operator holds scope; admin:read also allows safe methods.
func operatorAllowed(w http.ResponseWriter, r *http.Request, op *operator, scope string) bool {
	if op.scopes.Has(scope) || (!isUnsafe(r.Method) && op.scopes.Has(auth.AdminRead)) {
		return true
	}
	httpx.WriteProblem(w, r, http.StatusForbidden, "missing scope "+scope)
	return false
}

// adminAccess authorises instance administration: an instance admin session, or an operator holding
// scope. The user is nil for an operator.
func (a *API) adminAccess(w http.ResponseWriter, r *http.Request, scope string) (*store.User, bool) {
	op := principalOf(r).operator
	if op == nil {
		return a.requireAdmin(w, r)
	}
	return nil, operatorAllowed(w, r, op, scope)
}

// caller is requireUser for the tenancy routes, which operators may also use: the user is nil for them.
func (a *API) caller(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	if principalOf(r).operator != nil {
		return nil, true
	}
	return a.requireUser(w, r)
}

// operatorAccess is workspaceAccess for an operator: any existing workspace, as an owner without log access.
func (a *API) operatorAccess(w http.ResponseWriter, r *http.Request, ws uuid.UUID, perm string) (store.Membership, bool) {
	m := store.Membership{Role: "owner", Perms: auth.NewPermSet(auth.OperatorPermissions)}
	op := principalOf(r).operator
	if op == nil {
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "authentication required")
		return m, false
	}
	if !operatorAllowed(w, r, op, auth.AdminTenancy) {
		return m, false
	}
	if _, err := a.Store.GetWorkspace(r.Context(), ws); err != nil {
		a.storeError(w, r, err)
		return m, false
	}
	if perm != "" && !m.Perms.Has(perm) {
		httpx.WriteProblem(w, r, http.StatusForbidden, "missing permission "+perm)
		return m, false
	}
	return m, true
}

// userRef is the user's id, or nil for an operator.
func userRef(u *store.User) *uuid.UUID {
	if u == nil {
		return nil
	}
	return &u.ID
}

// actorID is the user's id, or uuid.Nil, which the store reads as an operator.
func actorID(u *store.User) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return u.ID
}

// corsPaths are the routes an admin token may call from a browser page, such as the Ervisio plugin.
var corsPaths = []string{"/admin/", "/workspaces", "/projects", "/tokens"}

// cors lets other origins call the admin-token routes. It never allows credentials, so a cookie
// session cannot be used cross-origin; cookie requests still pass the Origin and CSRF checks.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePath != "" {
			path = rc.RoutePath
		}
		if !corsPath(path) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func corsPath(p string) bool {
	for _, c := range corsPaths {
		if strings.HasPrefix(p, c) {
			return true
		}
	}
	return false
}

// ---------- instance admin tokens ----------

func (a *API) createAdminToken(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	var req struct {
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !decode(w, r, &req) || !validName(w, r, strings.TrimSpace(req.Name)) {
		return
	}
	if len(req.Scopes) == 0 {
		invalid(w, r, "scopes must list at least one of admin:read, admin:users, admin:settings, admin:tenancy, admin:storage")
		return
	}
	seen := map[string]bool{}
	for _, sc := range req.Scopes {
		switch {
		case !auth.AdminScopes[sc]:
			invalid(w, r, "unknown admin scope "+sc)
			return
		case seen[sc]:
			invalid(w, r, "duplicate scope "+sc)
			return
		}
		seen[sc] = true
	}
	switch {
	case req.ExpiresAt == nil:
		invalid(w, r, "expires_at is required")
		return
	case !req.ExpiresAt.After(a.now()):
		invalid(w, r, "expires_at must be in the future")
		return
	case req.ExpiresAt.After(a.now().Add(maxAdminTokenTTL)):
		invalid(w, r, "expires_at must be within one year")
		return
	}
	tok, prefix, hash := auth.NewAdminToken(a.secret)
	t, err := a.Store.CreateAdminToken(r.Context(), principalOf(r).user.ID, strings.TrimSpace(req.Name), prefix, hash, req.Scopes, *req.ExpiresAt)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, nil, "admin_token.create", "admin_token", t.ID.String(), map[string]any{"scopes": t.Scopes, "expires_at": t.ExpiresAt})
	// The plaintext token appears in this response only.
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"token": tok, "admin_token": t})
}

func (a *API) listAdminTokens(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	ts, err := a.Store.ListAdminTokens(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, ts)
}

func (a *API) revokeAdminToken(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	if err := a.Store.RevokeAdminToken(r.Context(), id); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, nil, "admin_token.revoke", "admin_token", id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}
