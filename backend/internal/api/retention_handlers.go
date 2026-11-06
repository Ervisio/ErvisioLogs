package api

import (
	"net/http"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (a *API) retentionRoutes(r chi.Router) {
	r.Get("/admin/workspaces", a.adminWorkspaces)
	for _, route := range []struct {
		path  string
		scope settingsScope
		kind  string
	}{
		{"/workspaces/{id}/retention", a.workspaceSettings, "workspace"},
		{"/projects/{id}/retention", a.projectSettings, "project"},
	} {
		r.MethodFunc("GET", route.path, a.scopedRetention(route.scope, route.kind))
		r.MethodFunc("PUT", route.path, a.scopedRetention(route.scope, route.kind))
		r.MethodFunc("DELETE", route.path, a.scopedRetention(route.scope, route.kind))
	}
}

func (a *API) scopedRetention(scope settingsScope, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, user, ok := scope(w, r, r.Method != http.MethodGet)
		if !ok {
			return
		}
		id := sc.Workspace
		if kind == "project" {
			id = sc.Project
		}
		switch r.Method {
		case http.MethodGet:
			rows, err := a.Store.RetentionPolicies(r.Context())
			if err != nil {
				a.internalError(w, r, err)
				return
			}
			items(w, visibleRetentionPolicies(rows, sc.Workspace, sc.Project))
		case http.MethodPut:
			var req retentionReq
			if !decode(w, r, &req) {
				return
			}
			if msg := validRetention(kind, req); msg != "" {
				invalid(w, r, msg)
				return
			}
			policy, err := a.Store.PutRetentionPolicy(r.Context(), store.RetentionPolicy{ScopeType: kind, ScopeID: id, LocalAgeHours: req.LocalAgeHours, GlobalAgeHours: req.GlobalAgeHours, UpdatedBy: userRef(user)})
			if err != nil {
				a.storeError(w, r, err)
				return
			}
			a.audit(r, &sc.Workspace, "retention.update", kind, id.String(), map[string]any{"local_age_hours": req.LocalAgeHours, "global_age_hours": req.GlobalAgeHours})
			httpx.WriteJSON(w, http.StatusOK, policy)
		case http.MethodDelete:
			if err := a.Store.DeleteRetentionPolicy(r.Context(), kind, id); err != nil {
				a.storeError(w, r, err)
				return
			}
			a.audit(r, &sc.Workspace, "retention.delete", kind, id.String(), nil)
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func (a *API) adminWorkspaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminAccess(w, r, auth.AdminTenancy); !ok {
		return
	}
	rows, err := a.Store.ListWorkspaces(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, rows)
}

func visibleRetentionPolicies(rows []store.RetentionPolicy, workspace, project uuid.UUID) []store.RetentionPolicy {
	visible := []store.RetentionPolicy{}
	for _, policy := range rows {
		switch policy.ScopeType {
		case "instance":
		case "workspace":
			if policy.ScopeID != workspace {
				continue
			}
		case "project":
			if project == uuid.Nil || policy.ScopeID != project {
				continue
			}
		default:
			continue
		}
		policy.CacheMaxBytes, policy.HighPct, policy.LowPct = nil, nil, nil
		policy.UpdatedBy = nil
		visible = append(visible, policy)
	}
	return visible
}
