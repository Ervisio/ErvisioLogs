package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/google/uuid"
)

func (a *API) listDashboardPanels(w http.ResponseWriter, r *http.Request) {
	p, _, _, ok := a.projectFromQuery(w, r, auth.LogsRead)
	if !ok {
		return
	}
	ps, err := a.Store.ListDashboardPanels(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, ps)
}
func (a *API) dashboardPanelAccess(w http.ResponseWriter, r *http.Request, edit bool) (store.DashboardPanel, store.Project, bool) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return store.DashboardPanel{}, store.Project{}, false
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return store.DashboardPanel{}, store.Project{}, false
	}
	panel, err := a.Store.DashboardPanelByID(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return panel, store.Project{}, false
	}
	p, _, m, ok := a.projectByID(w, r, u, panel.ProjectID, auth.LogsRead)
	if !ok {
		return panel, p, false
	}
	if edit && (panel.CreatedBy == nil || *panel.CreatedBy != u.ID) && !m.Perms.Has(auth.ProjectsWrite) {
		httpx.WriteProblem(w, r, 403, "only the author or a project administrator can change this panel")
		return panel, p, false
	}
	return panel, p, true
}
func (a *API) createDashboardPanel(w http.ResponseWriter, r *http.Request) {
	a.saveDashboardPanel(w, r, true)
}
func (a *API) updateDashboardPanel(w http.ResponseWriter, r *http.Request) {
	a.saveDashboardPanel(w, r, false)
}
func (a *API) saveDashboardPanel(w http.ResponseWriter, r *http.Request, create bool) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var old store.DashboardPanel
	if !create {
		var ok bool
		old, _, ok = a.dashboardPanelAccess(w, r, true)
		if !ok {
			return
		}
	}
	var req struct {
		ProjectID       uuid.UUID `json:"project_id"`
		SavedQueryID    uuid.UUID `json:"saved_query_id"`
		Name            string    `json:"name"`
		Type            string    `json:"type"`
		Field           string    `json:"field"`
		IntervalSeconds int       `json:"interval_seconds"`
		Limit           int       `json:"limit"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !create {
		if req.ProjectID != uuid.Nil && req.ProjectID != old.ProjectID {
			invalid(w, r, "a panel cannot move to another project")
			return
		}
		req.ProjectID = old.ProjectID
	}
	p, _, _, ok := a.projectByID(w, r, u, req.ProjectID, auth.LogsRead)
	if !ok {
		return
	}
	name, ok := trimmedName(w, r, req.Name)
	if !ok {
		return
	}
	if req.Type != "count" && req.Type != "timeseries" && req.Type != "top" {
		invalid(w, r, "type must be count, timeseries or top")
		return
	}
	if req.Field == "" {
		req.Field = "level"
	}
	if _, err := panelField(req.Field); err != nil {
		invalid(w, r, err.Error())
		return
	}
	if req.IntervalSeconds == 0 {
		req.IntervalSeconds = 3600
	}
	if req.Limit == 0 {
		req.Limit = 10
	}
	if req.IntervalSeconds < 60 || req.IntervalSeconds > 86400 || req.Limit < 1 || req.Limit > 100 {
		invalid(w, r, "interval_seconds must be 60 to 86400 and limit 1 to 100")
		return
	}
	sq, err := a.Store.SavedQueryByID(r.Context(), req.SavedQueryID)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	if sq.ProjectID != p.ID || sq.WorkspaceID != p.WorkspaceID {
		httpx.WriteProblem(w, r, 404, "saved query not found")
		return
	}
	panel := store.DashboardPanel{ID: old.ID, WorkspaceID: p.WorkspaceID, ProjectID: p.ID, SavedQueryID: sq.ID, Name: name, Type: req.Type, Field: req.Field, IntervalSeconds: req.IntervalSeconds, Limit: req.Limit, CreatedBy: &u.ID}
	panel, err = a.Store.SaveDashboardPanel(r.Context(), panel, create)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "dashboard.save", "dashboard_panel", panel.ID.String(), nil)
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, panel)
}
func (a *API) deleteDashboardPanel(w http.ResponseWriter, r *http.Request) {
	p, proj, ok := a.dashboardPanelAccess(w, r, true)
	if !ok {
		return
	}
	if err := a.Store.DeleteDashboardPanel(r.Context(), proj.WorkspaceID, p.ID); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &proj.WorkspaceID, "dashboard.delete", "dashboard_panel", p.ID.String(), nil)
	w.WriteHeader(204)
}
func panelField(field string) (string, error) {
	switch field {
	case "level":
		return "severity_text", nil
	case "stream":
		return "toString(stream_id)", nil
	case "event":
		return "event_name", nil
	case "service":
		return "source_service", nil
	case "environment":
		return "toString(environment_id)", nil
	}
	if strings.HasPrefix(field, "attributes.") {
		expr, err := attrExpr(field)
		if err != nil {
			return "", err
		}
		return "toString(" + expr + ")", nil
	}
	return "", errors.New("field must be level, stream, event, service, environment or attributes.<path>")
}
func (a *API) getDashboardPanelData(w http.ResponseWriter, r *http.Request) {
	panel, p, ok := a.dashboardPanelAccess(w, r, false)
	if !ok {
		return
	}
	if a.Logs == nil || a.Logs.CH == nil {
		httpx.WriteProblem(w, r, 503, "log storage is unavailable")
		return
	}
	sq, err := a.Store.SavedQueryByID(r.Context(), panel.SavedQueryID)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	var f savedFilter
	if err = json.Unmarshal(sq.Query, &f); err != nil {
		a.internalError(w, r, err)
		return
	}
	window, err := parseRange(f.Range)
	if err != nil {
		invalid(w, r, "the saved query has an invalid range")
		return
	}
	cat, err := a.projectCatalog(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	q, err := buildSearch(f.request(p.ID, a.now().UTC(), window), p, cat, a.now().UTC())
	if err != nil {
		invalid(w, r, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	source, tiers, partial, err := a.searchSource(ctx, p, q)
	if err != nil {
		a.searchFailed(w, r, err)
		return
	}
	sql, args, err := panelSQL(panel, source, q)
	if err != nil {
		invalid(w, r, err.Error())
		return
	}
	rows, err := a.Logs.CH.Query(ctx, sql, args...)
	if err != nil {
		a.searchFailed(w, r, err)
		return
	}
	defer rows.Close()
	type value struct {
		Label string `json:"label"`
		Value uint64 `json:"value"`
	}
	out := []value{}
	for rows.Next() {
		var v value
		if err := rows.Scan(&v.Label, &v.Value); err != nil {
			a.searchFailed(w, r, err)
			return
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		a.searchFailed(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": out, "partial": partial, "searched_tiers": tiers})
}
func panelSQL(panel store.DashboardPanel, source string, q *query) (string, []any, error) {
	expr := "'count'"
	tail := ""
	args := append([]any(nil), q.args...)
	switch panel.Type {
	case "count":
	case "timeseries":
		expr = "toString(toStartOfInterval(timestamp,INTERVAL " + strconv.Itoa(panel.IntervalSeconds) + " SECOND,'UTC'))"
		tail = " GROUP BY label ORDER BY label"
	case "top":
		var err error
		expr, err = panelField(panel.Field)
		if err != nil {
			return "", nil, err
		}
		tail = " GROUP BY label ORDER BY value DESC,label LIMIT ?"
		args = append(args, panel.Limit)
	default:
		return "", nil, fmt.Errorf("invalid panel type %q", panel.Type)
	}
	return "SELECT " + expr + " AS label,uniqExact(event_id) AS value FROM " + source + " WHERE " + q.sql() + tail, args, nil
}

func (a *API) usage(w http.ResponseWriter, r *http.Request) {
	p, _, _, ok := a.projectFromQuery(w, r, auth.LogsRead)
	if !ok {
		return
	}
	if a.Logs == nil || a.Logs.CH == nil {
		httpx.WriteProblem(w, r, 503, "log storage is unavailable")
		return
	}
	rangeValue := r.URL.Query().Get("range")
	if rangeValue == "" {
		rangeValue = "24h"
	}
	window, err := parseRange(rangeValue)
	if err != nil || window > searchMaxRange {
		invalid(w, r, "range must be positive and at most 31 days")
		return
	}
	interval := queryInt(r, "interval_seconds", 3600, 86400)
	if interval < 60 {
		invalid(w, r, "interval_seconds must be 60 to 86400")
		return
	}
	cat, err := a.projectCatalog(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	q, err := buildSearch((&savedFilter{}).request(p.ID, a.now().UTC(), window), p, cat, a.now().UTC())
	if err != nil {
		invalid(w, r, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()
	source, tiers, partial, err := a.searchSource(ctx, p, q)
	if err != nil {
		a.searchFailed(w, r, err)
		return
	}
	// Usage measures the persisted uncompressed text payload, including JSON attributes.
	sql := `SELECT toStartOfInterval(timestamp,INTERVAL ` + strconv.Itoa(interval) + ` SECOND,'UTC'),toString(stream_id),count(),sum(payload_bytes) FROM (SELECT timestamp,stream_id,event_id,length(message)+length(toJSONString(attributes))+length(toJSONString(tags))+length(severity_text)+length(event_name)+length(source_service)+length(source_host)+length(source_resource)+length(trace_id)+length(span_id) AS payload_bytes FROM ` + source + ` WHERE ` + q.sql() + ` LIMIT 1 BY event_id) GROUP BY 1,2 ORDER BY 1,2 LIMIT 10001`
	rows, err := a.Logs.CH.Query(ctx, sql, q.args...)
	if err != nil {
		a.searchFailed(w, r, err)
		return
	}
	defer rows.Close()
	type point struct {
		Timestamp time.Time `json:"timestamp"`
		Stream    string    `json:"stream"`
		Events    uint64    `json:"events"`
		Bytes     uint64    `json:"bytes"`
	}
	out := []point{}
	var events, bytes uint64
	names := map[string]string{}
	for name, id := range cat.streams {
		names[id.String()] = name
	}
	for rows.Next() {
		var v point
		if err := rows.Scan(&v.Timestamp, &v.Stream, &v.Events, &v.Bytes); err != nil {
			a.searchFailed(w, r, err)
			return
		}
		if name, ok := names[v.Stream]; ok {
			v.Stream = name
		}
		if len(out) >= 10000 {
			invalid(w, r, "usage has too many buckets; increase interval_seconds or narrow the range")
			return
		}
		events += v.Events
		bytes += v.Bytes
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		a.searchFailed(w, r, err)
		return
	}
	usedEvents, usedBytes := a.limits.usage(a.now(), p.ID)
	httpx.WriteJSON(w, 200, map[string]any{"items": out, "events": events, "bytes": bytes, "byte_measurement": "uncompressed_text_payload", "partial": partial, "searched_tiers": tiers, "quota": map[string]int64{"daily_events": a.Settings.ProjectInt(p.WorkspaceID, p.ID, settings.ProjectQuotaEvents), "daily_bytes": a.Settings.ProjectInt(p.WorkspaceID, p.ID, settings.ProjectQuotaBytes)}, "quota_used": map[string]any{"events": usedEvents, "bytes": usedBytes, "scope": "current_process"}})
}
func (l *limiter) usage(now time.Time, id uuid.UUID) (int64, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	q := l.quotas[id]
	if q == nil || q.day != now.UTC().Unix()/86400 {
		return 0, 0
	}
	return q.events, q.bytes
}
