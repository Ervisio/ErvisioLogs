package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

var (
	slugRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}$`)
	envRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}$`)
	streamRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	colorRe  = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	dashes   = regexp.MustCompile(`[^a-z0-9]+`)
)

func slugify(s string) string {
	s = strings.Trim(dashes.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 39 {
		s = strings.Trim(s[:39], "-")
	}
	return s
}

func validName(w http.ResponseWriter, r *http.Request, name string) bool {
	if n := len([]rune(name)); n < 1 || n > 100 || hasNUL(name) {
		invalid(w, r, "name must be 1 to 100 characters")
		return false
	}
	return true
}

// ---------- workspaces ----------

func (a *API) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ws, err := a.Store.WorkspacesOf(r.Context(), u.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, ws)
}

type workspaceReq struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (a *API) createWorkspace(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req workspaceReq
	if !decode(w, r, &req) || !validName(w, r, strings.TrimSpace(req.Name)) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Slug == "" {
		req.Slug = slugify(req.Name)
	}
	if !slugRe.MatchString(req.Slug) {
		invalid(w, r, "slug must be 1 to 39 characters: lowercase letters, digits and dashes")
		return
	}
	ws, err := a.Store.CreateWorkspace(r.Context(), u.ID, req.Slug, req.Name)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &ws.ID, "workspace.create", "workspace", ws.ID.String(), nil)
	httpx.WriteJSON(w, http.StatusCreated, ws)
}

func (a *API) getWorkspace(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	m, ok := a.workspaceAccess(w, r, u, id, "")
	if !ok {
		return
	}
	ws, err := a.Store.GetWorkspace(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	ws.Role = m.Role
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"workspace": ws, "permissions": m.Perms.List()})
}

func (a *API) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := a.workspaceAccess(w, r, u, id, auth.WorkspaceManage); !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) || !validName(w, r, strings.TrimSpace(req.Name)) {
		return
	}
	if err := a.Store.RenameWorkspace(r.Context(), id, strings.TrimSpace(req.Name)); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &id, "workspace.rename", "workspace", id.String(), nil)
	ws, _ := a.Store.GetWorkspace(r.Context(), id)
	httpx.WriteJSON(w, http.StatusOK, ws)
}

func (a *API) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := a.workspaceAccess(w, r, u, id, auth.WorkspaceManage); !ok {
		return
	}
	// Destructive: the caller must repeat the workspace slug, as the UI spec requires typed confirmation.
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	ws, err := a.Store.GetWorkspace(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	if req.Confirm != ws.Slug {
		invalid(w, r, "confirm must equal the workspace slug")
		return
	}
	if err := a.Store.DeleteWorkspace(r.Context(), id); err != nil {
		a.storeError(w, r, err)
		return
	}
	// At instance level: the workspace-scoped trail disappeared with the workspace.
	a.audit(r, nil, "workspace.delete", "workspace", id.String(), map[string]any{"slug": ws.Slug})
	w.WriteHeader(http.StatusNoContent)
}

// ---------- members ----------

func (a *API) listMembers(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	if _, ok := a.workspaceAccess(w, r, u, id, ""); !ok {
		return
	}
	ms, err := a.Store.ListMembers(r.Context(), id)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, ms)
}

var roleNames = map[string]bool{"owner": true, "admin": true, "developer": true, "analyst": true, "viewer": true}

// canGrant stops privilege escalation: only an owner can hand out or take away the owner role.
func canGrant(m store.Membership, role string) bool {
	return role != "owner" || m.Role == "owner"
}

func (a *API) addMember(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
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
	target, err := a.Store.UserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		// This answer tells a holder of members:manage whether an address has an account; invitations
		// reveal nothing, so clients should prefer them.
		httpx.WriteProblem(w, r, http.StatusNotFound, "no account with this email; ask an administrator to create it first")
		return
	}
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.Store.AddMember(r.Context(), id, target.ID, req.Role); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &id, "member.add", "user", target.ID.String(), map[string]any{"role": req.Role})
	httpx.WriteJSON(w, http.StatusCreated, store.Member{UserID: target.ID, Email: target.Email, Username: target.Username, Role: req.Role, JoinedAt: time.Now().UTC()})
}

func (a *API) updateMember(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	userID, ok := idParam(w, r, "userId")
	if !ok {
		return
	}
	if _, ok := a.workspaceAccess(w, r, u, id, auth.MembersManage); !ok {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !roleNames[req.Role] {
		invalid(w, r, "role must be one of owner, admin, developer, analyst, viewer")
		return
	}
	// The roles that decide are those read under the store's lock: either may have changed since the check above.
	var from string
	err := a.Store.SetMemberRole(r.Context(), id, u.ID, userID, req.Role, func(actor, target store.Membership) error {
		if !actor.Perms.Has(auth.MembersManage) || !canGrant(actor, req.Role) || !canGrant(actor, target.Role) {
			return store.ErrForbidden
		}
		from = target.Role
		return nil
	})
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &id, "member.role", "user", userID.String(), map[string]any{"from": from, "to": req.Role})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removeMember(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	userID, ok := idParam(w, r, "userId")
	if !ok {
		return
	}
	// Members may always leave; removing someone else needs members:manage.
	perm := auth.MembersManage
	if userID == u.ID {
		perm = ""
	}
	if _, ok := a.workspaceAccess(w, r, u, id, perm); !ok {
		return
	}
	// As in updateMember, the roles that decide are those read under the store's lock.
	err := a.Store.RemoveMember(r.Context(), id, u.ID, userID, func(actor, target store.Membership) error {
		if userID != u.ID && (!actor.Perms.Has(auth.MembersManage) || !canGrant(actor, target.Role)) {
			return store.ErrForbidden
		}
		return nil
	})
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &id, "member.remove", "user", userID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
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
	events, next, err := a.Store.ListAudit(r.Context(), id, queryInt(r, "limit", 50, 200), r.URL.Query().Get("cursor"))
	a.writeAuditPage(w, r, events, next, err)
}

func (a *API) writeAuditPage(w http.ResponseWriter, r *http.Request, events []store.AuditEvent, next string, err error) {
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	writePage(w, events, next)
}

// ---------- projects ----------

// projectAccess loads a project and authorises the caller in its workspace.
func (a *API) projectAccess(w http.ResponseWriter, r *http.Request, perm string) (store.Project, *store.User, bool) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return store.Project{}, nil, false
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return store.Project{}, nil, false
	}
	p, err := a.Store.GetProject(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return store.Project{}, nil, false
	}
	if _, ok := a.workspaceAccess(w, r, u, p.WorkspaceID, perm); !ok {
		return store.Project{}, nil, false
	}
	return p, u, true
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	ws, err := uuid.Parse(r.URL.Query().Get("workspace_id"))
	if err != nil {
		invalid(w, r, "workspace_id query parameter is required")
		return
	}
	if _, ok := a.workspaceAccess(w, r, u, ws, ""); !ok {
		return
	}
	ps, err := a.Store.ListProjects(r.Context(), ws)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, ps)
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		WorkspaceID uuid.UUID `json:"workspace_id"`
		Name        string    `json:"name"`
		Slug        string    `json:"slug"`
	}
	if !decode(w, r, &req) || !validName(w, r, strings.TrimSpace(req.Name)) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if _, ok := a.workspaceAccess(w, r, u, req.WorkspaceID, auth.ProjectsWrite); !ok {
		return
	}
	if req.Slug == "" {
		req.Slug = slugify(req.Name)
	}
	if !slugRe.MatchString(req.Slug) {
		invalid(w, r, "slug must be 1 to 39 characters: lowercase letters, digits and dashes")
		return
	}
	p, err := a.Store.CreateProject(r.Context(), req.WorkspaceID, req.Slug, req.Name)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "project.create", "project", p.ID.String(), nil)
	httpx.WriteJSON(w, http.StatusCreated, p)
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, "")
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (a *API) updateProject(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) || !validName(w, r, strings.TrimSpace(req.Name)) {
		return
	}
	if err := a.Store.RenameProject(r.Context(), p.WorkspaceID, p.ID, strings.TrimSpace(req.Name)); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "project.rename", "project", p.ID.String(), nil)
	p.Name = strings.TrimSpace(req.Name)
	httpx.WriteJSON(w, http.StatusOK, p)
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Confirm != p.Slug {
		invalid(w, r, "confirm must equal the project slug")
		return
	}
	if err := a.Store.DeleteProject(r.Context(), p.WorkspaceID, p.ID); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "project.delete", "project", p.ID.String(), map[string]any{"slug": p.Slug})
	w.WriteHeader(http.StatusNoContent)
}

// ---------- environments and streams ----------

// metaReq is the display metadata accepted on create and update; absent fields stay unchanged and
// an empty string clears one.
type metaReq struct {
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
}

func validMeta(w http.ResponseWriter, r *http.Request, m metaReq) (store.MetaPatch, bool) {
	trim := func(v *string) *string {
		if v == nil {
			return nil
		}
		t := strings.TrimSpace(*v)
		return &t
	}
	p := store.MetaPatch{DisplayName: trim(m.DisplayName), Description: trim(m.Description), Color: trim(m.Color)}
	if p.DisplayName != nil && (len([]rune(*p.DisplayName)) > 100 || hasNUL(*p.DisplayName)) {
		invalid(w, r, "display_name must be at most 100 characters")
		return p, false
	}
	if p.Description != nil && (len([]rune(*p.Description)) > 500 || hasNUL(*p.Description)) {
		invalid(w, r, "description must be at most 500 characters")
		return p, false
	}
	if p.Color != nil {
		*p.Color = strings.ToLower(*p.Color)
		if *p.Color != "" && !colorRe.MatchString(*p.Color) {
			invalid(w, r, "color must be a hex color like #3b82f6, or empty")
			return p, false
		}
	}
	return p, true
}

func (a *API) listEnvironments(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, "")
	if !ok {
		return
	}
	es, err := a.Store.ListEnvironments(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, es)
}

func (a *API) createEnvironment(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
		metaReq
	}
	if !decode(w, r, &req) {
		return
	}
	if !envRe.MatchString(req.Name) {
		invalid(w, r, "environment name: lowercase letters, digits and dashes, up to 39 characters")
		return
	}
	meta, ok := validMeta(w, r, req.metaReq)
	if !ok {
		return
	}
	e, err := a.Store.CreateEnvironment(r.Context(), p.WorkspaceID, p.ID, req.Name, meta.Meta())
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.forgetCatalog(p.ID)
	a.audit(r, &p.WorkspaceID, "environment.create", "environment", e.ID.String(), nil)
	httpx.WriteJSON(w, http.StatusCreated, e)
}

func (a *API) updateEnvironment(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	envID, ok := idParam(w, r, "envId")
	if !ok {
		return
	}
	var req metaReq
	if !decode(w, r, &req) {
		return
	}
	patch, ok := validMeta(w, r, req)
	if !ok {
		return
	}
	e, err := a.Store.UpdateEnvironment(r.Context(), p.WorkspaceID, p.ID, envID, patch)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "environment.update", "environment", envID.String(), nil)
	httpx.WriteJSON(w, http.StatusOK, e)
}

func (a *API) deleteEnvironment(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	envID, ok := idParam(w, r, "envId")
	if !ok {
		return
	}
	if err := a.Store.DeleteEnvironment(r.Context(), p.WorkspaceID, p.ID, envID); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.forgetCatalog(p.ID)
	a.audit(r, &p.WorkspaceID, "environment.delete", "environment", envID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listStreams(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, "")
	if !ok {
		return
	}
	ss, err := a.Store.ListStreams(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, ss)
}

func (a *API) createStream(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
		metaReq
	}
	if !decode(w, r, &req) {
		return
	}
	if !streamRe.MatchString(req.Name) {
		invalid(w, r, "stream name: lowercase letters, digits, dot, dash or underscore, up to 63 characters")
		return
	}
	meta, ok := validMeta(w, r, req.metaReq)
	if !ok {
		return
	}
	cat, err := a.projectCatalog(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if a.streamLimit(p.WorkspaceID, p.ID, len(cat.streams)) {
		httpx.WriteProblem(w, r, http.StatusConflict, errStreamLimit.Error())
		return
	}
	s, err := a.Store.CreateStream(r.Context(), p.WorkspaceID, p.ID, req.Name, meta.Meta())
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.forgetCatalog(p.ID)
	a.audit(r, &p.WorkspaceID, "stream.create", "stream", s.ID.String(), nil)
	httpx.WriteJSON(w, http.StatusCreated, s)
}

func (a *API) updateStream(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	sid, ok := idParam(w, r, "streamId")
	if !ok {
		return
	}
	var req metaReq
	if !decode(w, r, &req) {
		return
	}
	patch, ok := validMeta(w, r, req)
	if !ok {
		return
	}
	s, err := a.Store.UpdateStream(r.Context(), p.WorkspaceID, p.ID, sid, patch)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "stream.update", "stream", sid.String(), nil)
	httpx.WriteJSON(w, http.StatusOK, s)
}

func (a *API) deleteStream(w http.ResponseWriter, r *http.Request) {
	p, _, ok := a.projectAccess(w, r, auth.ProjectsWrite)
	if !ok {
		return
	}
	sid, ok := idParam(w, r, "streamId")
	if !ok {
		return
	}
	if err := a.Store.DeleteStream(r.Context(), p.WorkspaceID, p.ID, sid); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.forgetCatalog(p.ID)
	a.audit(r, &p.WorkspaceID, "stream.delete", "stream", sid.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}
