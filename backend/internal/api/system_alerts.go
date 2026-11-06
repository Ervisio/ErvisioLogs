package api

import (
	"context"
	"errors"
	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/notify"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/google/uuid"
	"net/http"
	"time"
)

func (a *API) systemSubscriptionAccess(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	u, ok := a.requireAdmin(w, r)
	if !ok {
		return uuid.Nil, false
	}
	ws, ok := idParam(w, r, "id")
	if !ok {
		return ws, false
	}
	_, ok = a.workspaceAccess(w, r, u, ws, auth.AlertsManage)
	return ws, ok
}
func (a *API) getSystemSubscription(w http.ResponseWriter, r *http.Request) {
	ws, ok := a.systemSubscriptionAccess(w, r)
	if !ok {
		return
	}
	sub, err := a.Store.SystemSubscription(r.Context(), ws)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, sub)
}
func (a *API) putSystemSubscription(w http.ResponseWriter, r *http.Request) {
	ws, ok := a.systemSubscriptionAccess(w, r)
	if !ok {
		return
	}
	var req struct {
		ChannelIDs []uuid.UUID `json:"channel_ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	ids := uniqueIDs(req.ChannelIDs)
	if len(ids) > 20 {
		invalid(w, r, "at most 20 channels")
		return
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	if len(ids) > 0 {
		cs, err := a.Store.ChannelsByIDs(r.Context(), ws, ids)
		if err != nil {
			a.internalError(w, r, err)
			return
		}
		if len(cs) != len(ids) {
			invalid(w, r, "channels must belong to this workspace")
			return
		}
	}
	if err := a.Store.SaveSystemSubscription(r.Context(), ws, ids); err != nil {
		a.storeError(w, r, err)
		return
	}
	a.audit(r, &ws, "system_alerts.update", "workspace", ws.String(), map[string]any{"channel_ids": ids})
	httpx.WriteJSON(w, 200, store.SystemSubscription{WorkspaceID: ws, ChannelIDs: ids})
}
func (a *API) evaluateSystemAlerts(ctx context.Context) {
	type condition struct {
		name, detail string
		firing       bool
	}
	conditions := []condition{}
	if a.Logs != nil && a.Logs.WAL != nil {
		s := a.Logs.WAL.Stats()
		conditions = append(conditions, condition{"WAL capacity", "The durable ingest queue exceeds 80% of capacity.", s.MaxBytes > 0 && s.Bytes*100/s.MaxBytes > 80})
	}
	if a.archiver() != nil {
		provider, err := a.Store.ActiveProvider(ctx)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			a.Log.Warn("system alert provider check failed", "error", err)
			return
		}
		conditions = append(conditions, condition{"Archive destination", "The active archive destination is unavailable.", errors.Is(err, store.ErrNotFound) || provider.Status == "error"})
		qctx, cancel := context.WithTimeout(ctx, alertQueryTimeout)
		oldest, ok, err := a.archiver().OldestUnarchived(qctx)
		cancel()
		if err != nil {
			a.Log.Warn("system alert archive check failed", "error", err)
			return
		}
		delay := max(48*time.Hour, 2*a.Settings.Duration(settings.ArchiveInterval))
		conditions = append(conditions, condition{"Archive delay", "Events have been waiting for archive for more than " + delay.String() + ".", ok && a.now().Sub(oldest) > delay})
	}
	subscriptions, err := a.Store.SystemSubscriptions(ctx)
	if err != nil {
		a.Log.Warn("system alert subscriptions failed", "error", err)
		return
	}
	for _, c := range conditions {
		changed, err := a.Store.SystemAlertTransition(ctx, c.name, c.firing)
		if err != nil {
			a.Log.Warn("system alert state failed", "error", err)
			continue
		}
		if !changed {
			continue
		}
		for _, sub := range subscriptions {
			typ := "alert.firing"
			if !c.firing {
				typ = "alert.resolved"
			}
			p := notify.Payload{Type: typ, EventID: uuid.New(), SentAt: a.now().UTC(), WorkspaceID: sub.WorkspaceID, RuleName: c.name, Detail: c.detail}
			a.deliver(ctx, store.AlertRule{WorkspaceID: sub.WorkspaceID, ChannelIDs: sub.ChannelIDs}, p)
		}
	}
}
