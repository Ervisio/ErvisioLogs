package api

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/notify"
	"github.com/Ervisio/ErvisioLogs/backend/internal/settings"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

const (
	alertQueryTimeout = 30 * time.Second
	alertSendTimeout  = 45 * time.Second
	alertBatch        = 100
	alertWorkers      = 4
)

// RunAlerts evaluates due rules every alerts.interval until ctx ends (ADR 0010). One instance at a
// time does the work, through a Postgres advisory lock, so replicas do not send duplicates. Alert
// history older than alerts.history_retention is pruned once an hour.
func (a *API) RunAlerts(ctx context.Context) {
	if a.Integrations == nil || a.Integrations.Notify == nil || a.Logs == nil || a.Logs.CH == nil {
		return
	}
	var lastPrune time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(a.Settings.Duration(settings.AlertsInterval)):
		}
		retention := a.Settings.Duration(settings.AlertsHistory)
		ran, err := a.Store.WithAlertLock(ctx, func(ctx context.Context) error {
			a.evaluateDue(ctx)
			a.evaluateSystemAlerts(ctx)
			if retention > 0 && time.Since(lastPrune) > time.Hour {
				lastPrune = time.Now()
				n, err := a.Store.PruneAlertEvents(ctx, a.now().Add(-retention))
				if err != nil {
					return err
				}
				if n > 0 {
					a.Log.Info("pruned alert history", "rows", n)
				}
			}
			return nil
		})
		if err != nil && ctx.Err() == nil {
			a.Log.Warn("alert evaluation failed", "error", err)
		} else if !ran {
			a.Log.Debug("another instance is evaluating alerts")
		}
	}
}

func (a *API) evaluateDue(ctx context.Context) {
	rules, err := a.Store.DueRules(ctx, a.now().UTC(), alertBatch)
	if err != nil {
		if ctx.Err() == nil {
			a.Log.Warn("loading due alert rules failed", "error", err)
		}
		return
	}
	sem := make(chan struct{}, alertWorkers)
	var wg sync.WaitGroup
	for _, rule := range rules {
		sem <- struct{}{}
		wg.Add(1)
		go func(rule store.AlertRule) {
			defer func() { <-sem; wg.Done() }()
			a.evaluateRule(ctx, rule)
		}(rule)
	}
	wg.Wait()
}

// evaluateRule counts the events matching the rule over its window, stores the outcome and
// notifies on a change of state.
func (a *API) evaluateRule(ctx context.Context, rule store.AlertRule) {
	now := a.now().UTC()
	res := store.RuleResult{Evaluated: now, Next: now.Add(time.Duration(rule.IntervalSeconds) * time.Second)}
	value, err := a.countRule(ctx, rule, now)
	if ctx.Err() != nil {
		return // shutting down: the rule stays due and is evaluated on the next start
	}
	switch {
	case err != nil:
		msg := err.Error()
		res.State, res.Error = "error", &msg
	case value > rule.Threshold:
		res.State, res.Value = "firing", &value
	default:
		res.State, res.Value = "ok", &value
	}
	saved, err := a.Store.SaveRuleResult(ctx, rule.ID, rule.UpdatedAt, res)
	if err != nil {
		a.Log.Warn("saving an alert result failed", "rule_id", rule.ID, "error", err)
		return
	}
	if !saved {
		return // edited or deleted meanwhile: the new version is evaluated on its own schedule
	}

	typ, status := transition(rule.State, res.State)
	if typ == "" {
		return
	}
	detail := ""
	if res.Error != nil {
		detail = *res.Error
	}
	threshold := rule.Threshold
	p := notify.Payload{Type: typ, EventID: uuid.New(), SentAt: now, WorkspaceID: rule.WorkspaceID,
		ProjectID: &rule.ProjectID, RuleID: &rule.ID, RuleName: rule.Name, Value: res.Value,
		Threshold: &threshold, WindowSec: rule.WindowSeconds, Detail: detail}
	deliveries := a.deliver(ctx, rule, p)
	ev := store.AlertEvent{ID: p.EventID, WorkspaceID: rule.WorkspaceID, RuleID: rule.ID, Status: status,
		Value: res.Value, Deliveries: deliveries}
	if detail != "" {
		ev.Detail = &detail
	}
	if err := a.Store.AddAlertEvent(context.WithoutCancel(ctx), ev); err != nil {
		a.Log.Warn("recording an alert event failed", "rule_id", rule.ID, "error", err)
	}
}

// transition maps a state change to the notification type and history status; "" means no
// notification. Recovering from an error to ok is silent; from an error to firing notifies.
func transition(prev, next string) (typ, status string) {
	switch {
	case prev == next:
		return "", ""
	case next == "firing":
		return "alert.firing", "firing"
	case next == "ok" && prev == "firing":
		return "alert.resolved", "resolved"
	case next == "error":
		return "alert.error", "error"
	}
	return "", ""
}

// countRule counts distinct events in the hot tier over the rule window. Windows are at most a
// day, well inside hot retention, so archived segments are not read.
func (a *API) countRule(ctx context.Context, rule store.AlertRule, now time.Time) (int64, error) {
	proj, err := a.Store.GetProject(ctx, rule.ProjectID)
	if err != nil {
		return 0, errors.New("the project could not be loaded")
	}
	var f savedFilter
	if err := json.Unmarshal(rule.Query, &f); err != nil {
		return 0, errors.New("the stored query is not valid")
	}
	cat, err := a.projectCatalog(ctx, proj.WorkspaceID, proj.ID)
	if err != nil {
		return 0, errors.New("the project catalog could not be loaded")
	}
	q, err := buildSearch(f.request(proj.ID, now, time.Duration(rule.WindowSeconds)*time.Second), proj, cat, now)
	if err != nil {
		return 0, err // a stream or environment named by the rule no longer exists
	}
	qctx := clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": int(alertQueryTimeout.Seconds()), "readonly": 2}))
	qctx, cancel := context.WithTimeout(qctx, alertQueryTimeout+5*time.Second)
	defer cancel()
	var n uint64
	if err := a.Logs.CH.QueryRow(qctx, "SELECT uniqExact(event_id) FROM events WHERE "+q.sql(), q.args...).Scan(&n); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		a.Log.Warn("alert query failed", "rule_id", rule.ID, "error", err)
		return 0, errors.New("the log query failed")
	}
	return int64(n), nil
}

type deliveryResult struct {
	ChannelID uuid.UUID `json:"channel_id"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
}

// deliver sends p to the rule's enabled channels and returns the outcome per channel as JSON.
func (a *API) deliver(ctx context.Context, rule store.AlertRule, p notify.Payload) json.RawMessage {
	out := []deliveryResult{}
	if len(rule.ChannelIDs) > 0 {
		cs, err := a.Store.ChannelsByIDs(ctx, rule.WorkspaceID, rule.ChannelIDs)
		if err != nil {
			a.Log.Warn("loading alert channels failed", "rule_id", rule.ID, "error", err)
		}
		for _, c := range cs {
			if !c.Enabled {
				continue
			}
			sctx, cancel := context.WithTimeout(ctx, alertSendTimeout)
			sendErr := a.sendChannel(sctx, c, p)
			cancel()
			if err := a.Store.RecordDelivery(context.WithoutCancel(ctx), c.ID, sendErr); err != nil {
				a.Log.Warn("recording a delivery failed", "channel_id", c.ID, "error", err)
			}
			d := deliveryResult{ChannelID: c.ID, OK: sendErr == nil}
			if sendErr != nil {
				d.Error = sendErr.Error()
			}
			out = append(out, d)
		}
	}
	b, _ := json.Marshal(out)
	return b
}
