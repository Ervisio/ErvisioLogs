package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------- saved queries (ADR 0010) ----------

type SavedQuery struct {
	ID          uuid.UUID       `json:"id"`
	WorkspaceID uuid.UUID       `json:"workspace_id"`
	ProjectID   uuid.UUID       `json:"project_id"`
	Name        string          `json:"name"`
	Query       json.RawMessage `json:"query"`
	CreatedBy   *uuid.UUID      `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

const savedQueryCols = `id, workspace_id, project_id, name, query, created_by, created_at, updated_at`

func scanSavedQuery(r rowScanner) (SavedQuery, error) {
	var q SavedQuery
	err := r.Scan(&q.ID, &q.WorkspaceID, &q.ProjectID, &q.Name, &q.Query, &q.CreatedBy, &q.CreatedAt, &q.UpdatedAt)
	return q, mapErr(err)
}

func (s *Store) CreateSavedQuery(ctx context.Context, workspaceID, projectID, createdBy uuid.UUID, name string, query json.RawMessage) (SavedQuery, error) {
	return scanSavedQuery(s.pool.QueryRow(ctx, `INSERT INTO saved_queries (id, workspace_id, project_id, name, query, created_by)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING `+savedQueryCols, newID(), workspaceID, projectID, name, query, createdBy))
}

func (s *Store) ListSavedQueries(ctx context.Context, workspaceID, projectID uuid.UUID) ([]SavedQuery, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+savedQueryCols+` FROM saved_queries WHERE workspace_id=$1 AND project_id=$2 ORDER BY name, id`, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanSavedQuery)
}

// SavedQueryByID returns the view without a tenant filter: callers check the workspace.
func (s *Store) SavedQueryByID(ctx context.Context, id uuid.UUID) (SavedQuery, error) {
	return scanSavedQuery(s.pool.QueryRow(ctx, `SELECT `+savedQueryCols+` FROM saved_queries WHERE id=$1`, id))
}

func (s *Store) UpdateSavedQuery(ctx context.Context, workspaceID, id uuid.UUID, name string, query json.RawMessage) (SavedQuery, error) {
	return scanSavedQuery(s.pool.QueryRow(ctx, `UPDATE saved_queries SET name=$3, query=$4, updated_at=now()
		WHERE workspace_id=$1 AND id=$2 RETURNING `+savedQueryCols, workspaceID, id, name, query))
}

func (s *Store) DeleteSavedQuery(ctx context.Context, workspaceID, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM saved_queries WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
}

// ---------- notification channels ----------

type NotificationChannel struct {
	ID            uuid.UUID  `json:"id"`
	WorkspaceID   uuid.UUID  `json:"workspace_id"`
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	Format        string     `json:"format"`
	URL           string     `json:"-"`
	SecretVersion int32      `json:"-"`
	Enabled       bool       `json:"enabled"`
	LastStatus    *string    `json:"last_status"`
	LastError     *string    `json:"last_error"`
	LastSentAt    *time.Time `json:"last_sent_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

const channelCols = `id, workspace_id, name, type, format, url, secret_version, enabled, last_status, last_error, last_sent_at, created_at, updated_at`

func scanChannel(r rowScanner) (NotificationChannel, error) {
	var c NotificationChannel
	err := r.Scan(&c.ID, &c.WorkspaceID, &c.Name, &c.Type, &c.Format, &c.URL, &c.SecretVersion, &c.Enabled, &c.LastStatus, &c.LastError,
		&c.LastSentAt, &c.CreatedAt, &c.UpdatedAt)
	return c, mapErr(err)
}

func (s *Store) CreateChannel(ctx context.Context, workspaceID, createdBy uuid.UUID, name, typ, format, url string) (NotificationChannel, error) {
	return scanChannel(s.pool.QueryRow(ctx, `INSERT INTO notification_channels (id, workspace_id, name, type, format, url, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+channelCols, newID(), workspaceID, name, typ, format, url, createdBy))
}

func (s *Store) ListChannels(ctx context.Context, workspaceID uuid.UUID) ([]NotificationChannel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+channelCols+` FROM notification_channels WHERE workspace_id=$1 ORDER BY name, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanChannel)
}

// ChannelByID returns the channel without a tenant filter: callers check the workspace.
func (s *Store) ChannelByID(ctx context.Context, id uuid.UUID) (NotificationChannel, error) {
	return scanChannel(s.pool.QueryRow(ctx, `SELECT `+channelCols+` FROM notification_channels WHERE id=$1`, id))
}

// ChannelsByIDs returns the channels of the workspace among ids; others are left out.
func (s *Store) ChannelsByIDs(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) ([]NotificationChannel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+channelCols+` FROM notification_channels WHERE workspace_id=$1 AND id = ANY($2) ORDER BY name, id`, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanChannel)
}

// ChannelUpdate changes the fields that are not nil. RotateSecret bumps the signing key version.
type ChannelUpdate struct {
	Name         *string
	Format       *string
	URL          *string
	Enabled      *bool
	RotateSecret bool
}

func (s *Store) UpdateChannel(ctx context.Context, workspaceID, id uuid.UUID, u ChannelUpdate) (NotificationChannel, error) {
	return scanChannel(s.pool.QueryRow(ctx, `UPDATE notification_channels SET
		name = COALESCE($3, name), url = COALESCE($4, url), enabled = COALESCE($5, enabled),
		secret_version = secret_version + CASE WHEN $6 THEN 1 ELSE 0 END, format = COALESCE($7, format), updated_at = now()
		WHERE workspace_id=$1 AND id=$2 RETURNING `+channelCols, workspaceID, id, u.Name, u.URL, u.Enabled, u.RotateSecret, u.Format))
}

// DeleteChannel also removes the channel from the rules of the workspace.
func (s *Store) DeleteChannel(ctx context.Context, workspaceID, id uuid.UUID) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if err := affected(tx.Exec(ctx, `DELETE FROM notification_channels WHERE workspace_id=$1 AND id=$2`, workspaceID, id)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE alert_rules SET channel_ids = array_remove(channel_ids, $2), updated_at = now()
			WHERE workspace_id=$1 AND $2 = ANY(channel_ids)`, workspaceID, id)
		return err
	})
}

// RecordDelivery stores the outcome of the last send to a channel.
func (s *Store) RecordDelivery(ctx context.Context, id uuid.UUID, sendErr error) error {
	status, msg := "ok", (*string)(nil)
	if sendErr != nil {
		status = "error"
		m := truncate(sendErr.Error(), 500)
		msg = &m
	}
	_, err := s.pool.Exec(ctx, `UPDATE notification_channels SET last_status=$2, last_error=$3, last_sent_at=now() WHERE id=$1`, id, status, msg)
	return err
}

// ---------- alert rules ----------

type AlertRule struct {
	ID              uuid.UUID       `json:"id"`
	WorkspaceID     uuid.UUID       `json:"workspace_id"`
	ProjectID       uuid.UUID       `json:"project_id"`
	Name            string          `json:"name"`
	Query           json.RawMessage `json:"query"`
	WindowSeconds   int32           `json:"window_seconds"`
	Threshold       int64           `json:"threshold"`
	IntervalSeconds int32           `json:"interval_seconds"`
	ChannelIDs      []uuid.UUID     `json:"channel_ids"`
	Enabled         bool            `json:"enabled"`
	State           string          `json:"state"`
	LastValue       *int64          `json:"last_value"`
	LastError       *string         `json:"last_error"`
	LastEvaluatedAt *time.Time      `json:"last_evaluated_at"`
	NextEvalAt      time.Time       `json:"next_eval_at"`
	CreatedBy       *uuid.UUID      `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

const ruleCols = `id, workspace_id, project_id, name, query, window_seconds, threshold, interval_seconds, channel_ids,
	enabled, state, last_value, last_error, last_evaluated_at, next_eval_at, created_by, created_at, updated_at`

func scanRule(r rowScanner) (AlertRule, error) {
	var a AlertRule
	err := r.Scan(&a.ID, &a.WorkspaceID, &a.ProjectID, &a.Name, &a.Query, &a.WindowSeconds, &a.Threshold, &a.IntervalSeconds,
		&a.ChannelIDs, &a.Enabled, &a.State, &a.LastValue, &a.LastError, &a.LastEvaluatedAt, &a.NextEvalAt, &a.CreatedBy,
		&a.CreatedAt, &a.UpdatedAt)
	if a.ChannelIDs == nil {
		a.ChannelIDs = []uuid.UUID{}
	}
	return a, mapErr(err)
}

// RuleSpec holds the user-editable fields of a rule.
type RuleSpec struct {
	Name            string
	Query           json.RawMessage
	WindowSeconds   int32
	Threshold       int64
	IntervalSeconds int32
	ChannelIDs      []uuid.UUID
	Enabled         bool
}

func (s *Store) CreateRule(ctx context.Context, workspaceID, projectID, createdBy uuid.UUID, r RuleSpec) (AlertRule, error) {
	return scanRule(s.pool.QueryRow(ctx, `INSERT INTO alert_rules (id, workspace_id, project_id, name, query, window_seconds,
		threshold, interval_seconds, channel_ids, enabled, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+ruleCols,
		newID(), workspaceID, projectID, r.Name, r.Query, r.WindowSeconds, r.Threshold, r.IntervalSeconds, r.ChannelIDs, r.Enabled, createdBy))
}

func (s *Store) ListRules(ctx context.Context, workspaceID, projectID uuid.UUID) ([]AlertRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE workspace_id=$1 AND project_id=$2 ORDER BY name, id`, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanRule)
}

// RuleByID returns the rule without a tenant filter: callers check the workspace.
func (s *Store) RuleByID(ctx context.Context, id uuid.UUID) (AlertRule, error) {
	return scanRule(s.pool.QueryRow(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE id=$1`, id))
}

// UpdateRule replaces the editable fields. A changed rule is evaluated on the next pass and its
// state goes back to ok, so a new threshold does not inherit an old firing.
func (s *Store) UpdateRule(ctx context.Context, workspaceID, id uuid.UUID, r RuleSpec) (AlertRule, error) {
	return scanRule(s.pool.QueryRow(ctx, `UPDATE alert_rules SET name=$3, query=$4, window_seconds=$5, threshold=$6,
		interval_seconds=$7, channel_ids=$8, enabled=$9, state='ok', last_error=NULL, next_eval_at=now(), updated_at=now()
		WHERE workspace_id=$1 AND id=$2 RETURNING `+ruleCols,
		workspaceID, id, r.Name, r.Query, r.WindowSeconds, r.Threshold, r.IntervalSeconds, r.ChannelIDs, r.Enabled))
}

func (s *Store) DeleteRule(ctx context.Context, workspaceID, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM alert_rules WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
}

// DueRules returns enabled rules whose next evaluation time has passed, oldest first.
func (s *Store) DueRules(ctx context.Context, now time.Time, limit int) ([]AlertRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE enabled AND next_eval_at <= $1
		ORDER BY next_eval_at, id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanRule)
}

// RuleResult is the outcome of one evaluation.
type RuleResult struct {
	State     string
	Value     *int64
	Error     *string
	Evaluated time.Time
	Next      time.Time
}

// SaveRuleResult stores an evaluation unless the rule changed since it was read (updated_at),
// in which case it returns false and the edit wins.
func (s *Store) SaveRuleResult(ctx context.Context, id uuid.UUID, readAt time.Time, res RuleResult) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE alert_rules SET state=$3, last_value=$4, last_error=$5, last_evaluated_at=$6, next_eval_at=$7
		WHERE id=$1 AND updated_at=$2`, id, readAt, res.State, res.Value, res.Error, res.Evaluated, res.Next)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

type AlertEvent struct {
	ID          uuid.UUID       `json:"id"`
	WorkspaceID uuid.UUID       `json:"workspace_id"`
	RuleID      uuid.UUID       `json:"rule_id"`
	Status      string          `json:"status"`
	Value       *int64          `json:"value"`
	Detail      *string         `json:"detail"`
	Deliveries  json.RawMessage `json:"deliveries"`
	CreatedAt   time.Time       `json:"created_at"`
}

const alertEventCols = `id, workspace_id, rule_id, status, value, detail, deliveries, created_at`

func scanAlertEvent(r rowScanner) (AlertEvent, error) {
	var e AlertEvent
	err := r.Scan(&e.ID, &e.WorkspaceID, &e.RuleID, &e.Status, &e.Value, &e.Detail, &e.Deliveries, &e.CreatedAt)
	return e, mapErr(err)
}

func (s *Store) AddAlertEvent(ctx context.Context, e AlertEvent) error {
	if e.Deliveries == nil {
		e.Deliveries = json.RawMessage("[]")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO alert_events (id, workspace_id, rule_id, status, value, detail, deliveries)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, newID(), e.WorkspaceID, e.RuleID, e.Status, e.Value, e.Detail, e.Deliveries)
	return mapErr(err)
}

func (s *Store) ListAlertEvents(ctx context.Context, workspaceID, ruleID uuid.UUID, limit int) ([]AlertEvent, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+alertEventCols+` FROM alert_events WHERE workspace_id=$1 AND rule_id=$2
		ORDER BY created_at DESC, id DESC LIMIT $3`, workspaceID, ruleID, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanAlertEvent)
}

// PruneAlertEvents deletes history older than before.
func (s *Store) PruneAlertEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_events WHERE created_at < $1`, before)
	return tag.RowsAffected(), err
}

// WithAlertLock runs fn while holding the instance-wide alert evaluator lock, or returns false.
func (s *Store) WithAlertLock(ctx context.Context, fn func(context.Context) error) (bool, error) {
	return s.withAdvisoryLock(ctx, alertLockKey, fn)
}

// ---------- helpers ----------

func collect[T any](rows pgx.Rows, scan func(rowScanner) (T, error)) ([]T, error) {
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
