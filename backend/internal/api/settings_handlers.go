package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// settingsScope authorizes a settings request and returns the scope its route names. write asks
// for the permission that changes values there, otherwise reading is enough.
type settingsScope func(w http.ResponseWriter, r *http.Request, write bool) (settings.Scope, *store.User, bool)

func (a *API) instanceSettings(w http.ResponseWriter, r *http.Request, _ bool) (settings.Scope, *store.User, bool) {
	u, ok := a.adminAccess(w, r, auth.AdminSettings)
	return settings.Scope{}, u, ok
}

func (a *API) workspaceSettings(w http.ResponseWriter, r *http.Request, write bool) (settings.Scope, *store.User, bool) {
	u, ok := a.caller(w, r)
	if !ok {
		return settings.Scope{}, nil, false
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return settings.Scope{}, nil, false
	}
	perm := ""
	if write {
		perm = auth.WorkspaceManage
	}
	if _, ok := a.workspaceAccess(w, r, u, id, perm); !ok {
		return settings.Scope{}, nil, false
	}
	return settings.Scope{Workspace: id}, u, true
}

func (a *API) projectSettings(w http.ResponseWriter, r *http.Request, write bool) (settings.Scope, *store.User, bool) {
	perm := ""
	if write {
		perm = auth.ProjectsWrite
	}
	p, u, ok := a.projectAccess(w, r, perm)
	return settings.Scope{Workspace: p.WorkspaceID, Project: p.ID}, u, ok
}

func (a *API) listSettings(scope settingsScope) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sc, _, ok := scope(w, r, false); ok {
			items(w, a.Settings.Describe(sc))
		}
	}
}

func (a *API) putSetting(scope settingsScope) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, u, ok := scope(w, r, true)
		if !ok {
			return
		}
		var req struct {
			Value json.RawMessage `json:"value"`
		}
		if !decode(w, r, &req) {
			return
		}
		key := chi.URLParam(r, "key")
		e, err := a.Settings.Set(r.Context(), sc, key, req.Value, userRef(u))
		if err != nil {
			a.settingsError(w, r, err, e)
			return
		}
		a.auditSetting(r, sc, "settings.update", key, e.Value)
		httpx.WriteJSON(w, http.StatusOK, e)
	}
}

func (a *API) resetSetting(scope settingsScope) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, _, ok := scope(w, r, true)
		if !ok {
			return
		}
		key := chi.URLParam(r, "key")
		e, err := a.Settings.Reset(r.Context(), sc, key)
		if err != nil {
			a.settingsError(w, r, err, e)
			return
		}
		a.auditSetting(r, sc, "settings.reset", key, nil)
		httpx.WriteJSON(w, http.StatusOK, e)
	}
}

func (a *API) settingsError(w http.ResponseWriter, r *http.Request, err error, e settings.Entry) {
	var inv settings.Invalid
	switch {
	case errors.Is(err, settings.ErrUnknown):
		httpx.WriteProblem(w, r, http.StatusNotFound, "unknown setting")
	case errors.Is(err, settings.ErrLocked):
		httpx.WriteProblem(w, r, http.StatusConflict, e.Key+" is set by "+e.Env+" on the server; change it there")
	case errors.As(err, &inv):
		invalid(w, r, inv.Error())
	default:
		a.internalError(w, r, err)
	}
}

func (a *API) auditSetting(r *http.Request, sc settings.Scope, action, key string, value any) {
	detail := map[string]any{"key": key}
	if value != nil {
		detail["value"] = value
	}
	switch {
	case sc.Project != uuid.Nil:
		a.audit(r, &sc.Workspace, action, "project", sc.Project.String(), detail)
	case sc.Workspace != uuid.Nil:
		a.audit(r, &sc.Workspace, action, "workspace", sc.Workspace.String(), detail)
	default:
		a.audit(r, nil, action, "instance", "", detail)
	}
}
