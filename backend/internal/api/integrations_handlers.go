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

	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/notify"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

// Integrations configures saved views, alerts, channels and export (ADR 0010). Nil turns the
// alert and channel endpoints into 503; saved views keep working.
type Integrations struct {
	Notify *notify.Client
}

// savedFilter is the stored form of a search: the filter fields of a search request plus, for
// saved views, a range relative to the time the view is opened.
type savedFilter struct {
	Range        string       `json:"range,omitempty"`
	Query        string       `json:"query,omitempty"`
	SeverityMin  uint8        `json:"severity_min,omitempty"`
	Levels       []string     `json:"levels,omitempty"`
	Streams      []string     `json:"streams,omitempty"`
	Environments []string     `json:"environments,omitempty"`
	Events       []string     `json:"events,omitempty"`
	Services     []string     `json:"services,omitempty"`
	TraceID      string       `json:"trace_id,omitempty"`
	Filters      []AttrFilter `json:"filters,omitempty"`
}

// parseRange accepts Go durations plus a day suffix ("15m", "1h", "7d").
func parseRange(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n <= 0 {
			return 0, errors.New("range must look like 15m, 1h or 7d")
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, errors.New("range must look like 15m, 1h or 7d")
	}
	return d, nil
}

// request builds the search over [now-window, now).
func (f savedFilter) request(proj uuid.UUID, now time.Time, window time.Duration) *searchReq {
	from := now.Add(-window)
	return &searchReq{ProjectID: proj, From: &from, To: &now, Query: f.Query, SeverityMin: f.SeverityMin,
		Levels: f.Levels, Streams: f.Streams, Environments: f.Environments, Events: f.Events,
		Services: f.Services, TraceID: f.TraceID, Filters: f.Filters}
}

// checkFilter validates f against the project as a search would, and returns its canonical JSON.
// needRange is true for saved views.
func (a *API) checkFilter(ctx context.Context, proj store.Project, f savedFilter, needRange bool) (json.RawMessage, error) {
	window := 24 * time.Hour
	if needRange {
		if f.Range == "" {
			f.Range = "24h"
		}
		d, err := parseRange(f.Range)
		if err != nil {
			return nil, errInvalid{err}
		}
		if d > searchMaxRange {
			return nil, errInvalid{fmt.Errorf("range is longer than %d days", int(searchMaxRange.Hours()/24))}
		}
		window = d
	} else if f.Range != "" {
		return nil, errInvalid{errors.New("an alert uses window_seconds, not range")}
	}
	cat, err := a.projectCatalog(ctx, proj.WorkspaceID, proj.ID)
	if err != nil {
		return nil, err
	}
	if _, err := buildSearch(f.request(proj.ID, a.now().UTC(), window), proj, cat, a.now().UTC()); err != nil {
		return nil, errInvalid{err}
	}
	return json.Marshal(f)
}

type errInvalid struct{ error }

// filterError answers a checkFilter error: 422 for a bad filter, 500 otherwise.
func (a *API) filterError(w http.ResponseWriter, r *http.Request, err error) {
	var bad errInvalid
	if errors.As(err, &bad) {
		invalid(w, r, err.Error())
		return
	}
	a.internalError(w, r, err)
}

// projectFromQuery loads ?project_id= and checks perm for the session user.
func (a *API) projectFromQuery(w http.ResponseWriter, r *http.Request, perm string) (store.Project, *store.User, store.Membership, bool) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return store.Project{}, nil, store.Membership{}, false
	}
	id, err := uuid.Parse(r.URL.Query().Get("project_id"))
	if err != nil {
		invalid(w, r, "project_id query parameter is required")
		return store.Project{}, nil, store.Membership{}, false
	}
	return a.projectByID(w, r, u, id, perm)
}

func (a *API) projectByID(w http.ResponseWriter, r *http.Request, u *store.User, id uuid.UUID, perm string) (store.Project, *store.User, store.Membership, bool) {
	p, err := a.Store.GetProject(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return store.Project{}, nil, store.Membership{}, false
	}
	m, ok := a.workspaceAccess(w, r, u, p.WorkspaceID, perm)
	if !ok {
		return store.Project{}, nil, store.Membership{}, false
	}
	return p, u, m, true
}

func trimmedName(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, validName(w, r, name)
}

// ---------- saved views ----------

type savedQueryReq struct {
	ProjectID uuid.UUID   `json:"project_id"`
	Name      string      `json:"name"`
	Query     savedFilter `json:"query"`
}

func (a *API) listSavedQueries(w http.ResponseWriter, r *http.Request) {
	p, _, _, ok := a.projectFromQuery(w, r, auth.LogsRead)
	if !ok {
		return
	}
	qs, err := a.Store.ListSavedQueries(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, qs)
}

func (a *API) createSavedQuery(w http.ResponseWriter, r *http.Request) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req savedQueryReq
	if !decode(w, r, &req) {
		return
	}
	p, _, _, ok := a.projectByID(w, r, u, req.ProjectID, auth.LogsRead)
	if !ok {
		return
	}
	name, ok := trimmedName(w, r, req.Name)
	if !ok {
		return
	}
	q, err := a.checkFilter(r.Context(), p, req.Query, true)
	if err != nil {
		a.filterError(w, r, err)
		return
	}
	sq, err := a.Store.CreateSavedQuery(r.Context(), p.WorkspaceID, p.ID, u.ID, name, q)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, sq)
}

// savedQueryAccess loads a view; editing needs to be its author or to hold projects:write.
func (a *API) savedQueryAccess(w http.ResponseWriter, r *http.Request, edit bool) (store.SavedQuery, store.Project, bool) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return store.SavedQuery{}, store.Project{}, false
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return store.SavedQuery{}, store.Project{}, false
	}
	sq, err := a.Store.SavedQueryByID(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return store.SavedQuery{}, store.Project{}, false
	}
	p, _, m, ok := a.projectByID(w, r, u, sq.ProjectID, auth.LogsRead)
	if !ok {
		return store.SavedQuery{}, store.Project{}, false
	}
	if edit && (sq.CreatedBy == nil || *sq.CreatedBy != u.ID) && !m.Perms.Has(auth.ProjectsWrite) {
		httpx.WriteProblem(w, r, http.StatusForbidden, "only the author or a member with "+auth.ProjectsWrite+" can change this view")
		return store.SavedQuery{}, store.Project{}, false
	}
	return sq, p, true
}

func (a *API) getSavedQuery(w http.ResponseWriter, r *http.Request) {
	sq, _, ok := a.savedQueryAccess(w, r, false)
	if ok {
		httpx.WriteJSON(w, http.StatusOK, sq)
	}
}

func (a *API) updateSavedQuery(w http.ResponseWriter, r *http.Request) {
	sq, p, ok := a.savedQueryAccess(w, r, true)
	if !ok {
		return
	}
	var req struct {
		ProjectID uuid.UUID   `json:"project_id"`
		Name      string      `json:"name"`
		Query     savedFilter `json:"query"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ProjectID != uuid.Nil && req.ProjectID != sq.ProjectID {
		invalid(w, r, "a saved query cannot move to another project")
		return
	}
	name, ok := trimmedName(w, r, req.Name)
	if !ok {
		return
	}
	q, err := a.checkFilter(r.Context(), p, req.Query, true)
	if err != nil {
		a.filterError(w, r, err)
		return
	}
	out, err := a.Store.UpdateSavedQuery(r.Context(), p.WorkspaceID, sq.ID, name, q)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteSavedQuery(w http.ResponseWriter, r *http.Request) {
	sq, p, ok := a.savedQueryAccess(w, r, true)
	if !ok {
		return
	}
	if err := a.Store.DeleteSavedQuery(r.Context(), p.WorkspaceID, sq.ID); err != nil {
		a.storeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- notification channels ----------

// channelView is a channel as the API shows it: the URL reduced to scheme and host, and the
// signing secret only in the answer that creates or rotates it.
type channelView struct {
	store.NotificationChannel
	URLHint string `json:"url_hint"`
	Secret  string `json:"secret,omitempty"`
}

func (a *API) viewChannel(c store.NotificationChannel, withSecret bool) channelView {
	v := channelView{NotificationChannel: c, URLHint: notify.RedactURL(c.URL)}
	if withSecret && c.Format == notify.FormatErvisio {
		v.Secret = notify.DisplaySecret(notify.DeriveKey(a.secret, c.ID, c.SecretVersion))
	}
	return v
}

func (a *API) integrationsOn(w http.ResponseWriter, r *http.Request) bool {
	if a.Integrations == nil || a.Integrations.Notify == nil {
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "alerts are not available on this instance")
		return false
	}
	return true
}

func (a *API) listChannels(w http.ResponseWriter, r *http.Request) {
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
	cs, err := a.Store.ListChannels(r.Context(), ws)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	out := make([]channelView, len(cs))
	for i, c := range cs {
		out[i] = a.viewChannel(c, false)
	}
	items(w, out)
}

func (a *API) createChannel(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w, r) {
		return
	}
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		WorkspaceID uuid.UUID `json:"workspace_id"`
		Name        string    `json:"name"`
		Type        string    `json:"type"`
		Format      string    `json:"format"`
		URL         string    `json:"url"`
	}
	if !decode(w, r, &req) {
		return
	}
	if _, ok := a.workspaceAccess(w, r, u, req.WorkspaceID, auth.AlertsManage); !ok {
		return
	}
	name, ok := trimmedName(w, r, req.Name)
	if !ok {
		return
	}
	if req.Type == "" {
		req.Type = "webhook"
	}
	if req.Type != "webhook" {
		invalid(w, r, "type must be webhook")
		return
	}
	if req.Format == "" {
		req.Format = notify.FormatErvisio
	}
	if !notify.ValidFormat(req.Format) {
		invalid(w, r, "format must be ervisio, slack or discord")
		return
	}
	if err := notify.ValidateURL(req.URL, a.Settings.Bool(settings.WebhookAllowPrivate)); err != nil {
		invalid(w, r, err.Error())
		return
	}
	c, err := a.Store.CreateChannel(r.Context(), req.WorkspaceID, u.ID, name, req.Type, req.Format, req.URL)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &c.WorkspaceID, "channel.create", "notification_channel", c.ID.String(),
		map[string]any{"name": c.Name, "format": c.Format, "url": notify.RedactURL(c.URL)})
	httpx.WriteJSON(w, http.StatusCreated, a.viewChannel(c, true))
}

// channelAccess loads a channel and checks perm in its workspace.
func (a *API) channelAccess(w http.ResponseWriter, r *http.Request, perm string) (store.NotificationChannel, bool) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return store.NotificationChannel{}, false
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return store.NotificationChannel{}, false
	}
	c, err := a.Store.ChannelByID(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return store.NotificationChannel{}, false
	}
	if _, ok := a.workspaceAccess(w, r, u, c.WorkspaceID, perm); !ok {
		return store.NotificationChannel{}, false
	}
	return c, true
}

func (a *API) getChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := a.channelAccess(w, r, "")
	if ok {
		httpx.WriteJSON(w, http.StatusOK, a.viewChannel(c, false))
	}
}

func (a *API) updateChannel(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w, r) {
		return
	}
	c, ok := a.channelAccess(w, r, auth.AlertsManage)
	if !ok {
		return
	}
	var req struct {
		Name    *string `json:"name"`
		Format  *string `json:"format"`
		URL     *string `json:"url"`
		Enabled *bool   `json:"enabled"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil {
		n, ok := trimmedName(w, r, *req.Name)
		if !ok {
			return
		}
		req.Name = &n
	}
	if req.Format != nil && !notify.ValidFormat(*req.Format) {
		invalid(w, r, "format must be ervisio, slack or discord")
		return
	}
	if req.URL != nil {
		if err := notify.ValidateURL(*req.URL, a.Settings.Bool(settings.WebhookAllowPrivate)); err != nil {
			invalid(w, r, err.Error())
			return
		}
	}
	out, err := a.Store.UpdateChannel(r.Context(), c.WorkspaceID, c.ID,
		store.ChannelUpdate{Name: req.Name, Format: req.Format, URL: req.URL, Enabled: req.Enabled})
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &c.WorkspaceID, "channel.update", "notification_channel", c.ID.String(),
		map[string]any{"name_changed": req.Name != nil, "url_changed": req.URL != nil, "enabled": req.Enabled})
	httpx.WriteJSON(w, http.StatusOK, a.viewChannel(out, false))
}

func (a *API) rotateChannelSecret(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w, r) {
		return
	}
	c, ok := a.channelAccess(w, r, auth.AlertsManage)
	if !ok {
		return
	}
	out, err := a.Store.UpdateChannel(r.Context(), c.WorkspaceID, c.ID, store.ChannelUpdate{RotateSecret: true})
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &c.WorkspaceID, "channel.rotate_secret", "notification_channel", c.ID.String(), nil)
	httpx.WriteJSON(w, http.StatusOK, a.viewChannel(out, true))
}

func (a *API) deleteChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := a.channelAccess(w, r, auth.AlertsManage)
	if !ok {
		return
	}
	if err := a.Store.DeleteChannel(r.Context(), c.WorkspaceID, c.ID); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &c.WorkspaceID, "channel.delete", "notification_channel", c.ID.String(), map[string]any{"name": c.Name})
	w.WriteHeader(http.StatusNoContent)
}

// testChannel sends a test notification now and reports the outcome.
func (a *API) testChannel(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w, r) {
		return
	}
	c, ok := a.channelAccess(w, r, auth.AlertsManage)
	if !ok {
		return
	}
	p := notify.Payload{Type: "test", EventID: uuid.New(), SentAt: a.now().UTC(), WorkspaceID: c.WorkspaceID}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	sendErr := a.Integrations.Notify.Send(ctx, a.delivery(c, p))
	if err := a.Store.RecordDelivery(context.WithoutCancel(r.Context()), c.ID, sendErr); err != nil {
		a.Log.Warn("recording a delivery failed", "channel_id", c.ID, "error", err)
	}
	resp := map[string]any{"ok": sendErr == nil}
	if sendErr != nil {
		resp["error"] = sendErr.Error()
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (a *API) delivery(c store.NotificationChannel, p notify.Payload) notify.Delivery {
	return notify.Delivery{URL: c.URL, Format: c.Format, Key: notify.DeriveKey(a.secret, c.ID, c.SecretVersion), Payload: p}
}

// ---------- alert rules ----------

type ruleReq struct {
	ProjectID       uuid.UUID   `json:"project_id"`
	Name            string      `json:"name"`
	Query           savedFilter `json:"query"`
	WindowSeconds   int32       `json:"window_seconds"`
	Threshold       *int64      `json:"threshold"`
	IntervalSeconds int32       `json:"interval_seconds"`
	ChannelIDs      []uuid.UUID `json:"channel_ids"`
	Enabled         *bool       `json:"enabled"`
}

// ruleSpec validates req for project p.
func (a *API) ruleSpec(w http.ResponseWriter, r *http.Request, p store.Project, req ruleReq) (store.RuleSpec, bool) {
	name, ok := trimmedName(w, r, req.Name)
	if !ok {
		return store.RuleSpec{}, false
	}
	if req.WindowSeconds < 60 || req.WindowSeconds > 86400 {
		invalid(w, r, "window_seconds must be 60 to 86400")
		return store.RuleSpec{}, false
	}
	if req.Threshold == nil || *req.Threshold < 0 {
		invalid(w, r, "threshold is required and must be 0 or more")
		return store.RuleSpec{}, false
	}
	if req.IntervalSeconds == 0 {
		req.IntervalSeconds = 60
	}
	if req.IntervalSeconds < 30 || req.IntervalSeconds > 86400 {
		invalid(w, r, "interval_seconds must be 30 to 86400")
		return store.RuleSpec{}, false
	}
	if len(req.ChannelIDs) > 20 {
		invalid(w, r, "at most 20 channels")
		return store.RuleSpec{}, false
	}
	ids := uniqueIDs(req.ChannelIDs)
	if len(ids) > 0 {
		cs, err := a.Store.ChannelsByIDs(r.Context(), p.WorkspaceID, ids)
		if err != nil {
			a.internalError(w, r, err)
			return store.RuleSpec{}, false
		}
		if len(cs) != len(ids) {
			invalid(w, r, "channel_ids must name channels of this workspace")
			return store.RuleSpec{}, false
		}
	}
	q, err := a.checkFilter(r.Context(), p, req.Query, false)
	if err != nil {
		a.filterError(w, r, err)
		return store.RuleSpec{}, false
	}
	enabled := req.Enabled == nil || *req.Enabled
	return store.RuleSpec{Name: name, Query: q, WindowSeconds: req.WindowSeconds, Threshold: *req.Threshold,
		IntervalSeconds: req.IntervalSeconds, ChannelIDs: ids, Enabled: enabled}, true
}

func uniqueIDs(in []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (a *API) listRules(w http.ResponseWriter, r *http.Request) {
	p, _, _, ok := a.projectFromQuery(w, r, auth.LogsRead)
	if !ok {
		return
	}
	rs, err := a.Store.ListRules(r.Context(), p.WorkspaceID, p.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, rs)
}

func (a *API) createRule(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w, r) {
		return
	}
	u, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	var req ruleReq
	if !decode(w, r, &req) {
		return
	}
	p, _, _, ok := a.projectByID(w, r, u, req.ProjectID, auth.AlertsManage)
	if !ok {
		return
	}
	spec, ok := a.ruleSpec(w, r, p, req)
	if !ok {
		return
	}
	rule, err := a.Store.CreateRule(r.Context(), p.WorkspaceID, p.ID, u.ID, spec)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "alert.create", "alert_rule", rule.ID.String(), map[string]any{"name": rule.Name})
	httpx.WriteJSON(w, http.StatusCreated, rule)
}

func (a *API) ruleAccess(w http.ResponseWriter, r *http.Request, perm string) (store.AlertRule, store.Project, bool) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return store.AlertRule{}, store.Project{}, false
	}
	id, ok := idParam(w, r, "id")
	if !ok {
		return store.AlertRule{}, store.Project{}, false
	}
	rule, err := a.Store.RuleByID(r.Context(), id)
	if err != nil {
		a.storeError(w, r, err)
		return store.AlertRule{}, store.Project{}, false
	}
	p, _, _, ok := a.projectByID(w, r, u, rule.ProjectID, perm)
	if !ok {
		return store.AlertRule{}, store.Project{}, false
	}
	return rule, p, true
}

func (a *API) getRule(w http.ResponseWriter, r *http.Request) {
	rule, _, ok := a.ruleAccess(w, r, auth.LogsRead)
	if ok {
		httpx.WriteJSON(w, http.StatusOK, rule)
	}
}

func (a *API) updateRule(w http.ResponseWriter, r *http.Request) {
	if !a.integrationsOn(w, r) {
		return
	}
	rule, p, ok := a.ruleAccess(w, r, auth.AlertsManage)
	if !ok {
		return
	}
	var req ruleReq
	if !decode(w, r, &req) {
		return
	}
	if req.ProjectID != uuid.Nil && req.ProjectID != rule.ProjectID {
		invalid(w, r, "a rule cannot move to another project")
		return
	}
	spec, ok := a.ruleSpec(w, r, p, req)
	if !ok {
		return
	}
	out, err := a.Store.UpdateRule(r.Context(), p.WorkspaceID, rule.ID, spec)
	if err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "alert.update", "alert_rule", rule.ID.String(), map[string]any{"name": out.Name, "enabled": out.Enabled})
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteRule(w http.ResponseWriter, r *http.Request) {
	rule, p, ok := a.ruleAccess(w, r, auth.AlertsManage)
	if !ok {
		return
	}
	if err := a.Store.DeleteRule(r.Context(), p.WorkspaceID, rule.ID); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &p.WorkspaceID, "alert.delete", "alert_rule", rule.ID.String(), map[string]any{"name": rule.Name})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listRuleEvents(w http.ResponseWriter, r *http.Request) {
	rule, p, ok := a.ruleAccess(w, r, auth.LogsRead)
	if !ok {
		return
	}
	es, err := a.Store.ListAlertEvents(r.Context(), p.WorkspaceID, rule.ID, queryInt(r, "limit", 50, 200))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	items(w, es)
}
