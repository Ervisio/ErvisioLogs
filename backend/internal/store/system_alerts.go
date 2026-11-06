package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SystemSubscription struct {
	WorkspaceID uuid.UUID   `json:"workspace_id"`
	ChannelIDs  []uuid.UUID `json:"channel_ids"`
}

func (s *Store) SystemSubscription(ctx context.Context, ws uuid.UUID) (SystemSubscription, error) {
	v := SystemSubscription{WorkspaceID: ws, ChannelIDs: []uuid.UUID{}}
	err := s.pool.QueryRow(ctx, `SELECT channel_ids FROM system_alert_subscriptions WHERE workspace_id=$1`, ws).Scan(&v.ChannelIDs)
	if err == pgx.ErrNoRows {
		return v, nil
	}
	return v, err
}
func (s *Store) SaveSystemSubscription(ctx context.Context, ws uuid.UUID, ids []uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO system_alert_subscriptions(workspace_id,channel_ids) VALUES($1,$2) ON CONFLICT(workspace_id) DO UPDATE SET channel_ids=EXCLUDED.channel_ids,updated_at=now()`, ws, ids)
	return err
}
func (s *Store) SystemSubscriptions(ctx context.Context) ([]SystemSubscription, error) {
	rows, err := s.pool.Query(ctx, `SELECT workspace_id,channel_ids FROM system_alert_subscriptions WHERE cardinality(channel_ids)>0`)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r rowScanner) (SystemSubscription, error) {
		var v SystemSubscription
		err := r.Scan(&v.WorkspaceID, &v.ChannelIDs)
		return v, err
	})
}
func (s *Store) SystemAlertTransition(ctx context.Context, name string, firing bool) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO system_alert_states(name,firing) SELECT $1,$2 WHERE $2 OR EXISTS(SELECT 1 FROM system_alert_states WHERE name=$1) ON CONFLICT(name) DO UPDATE SET firing=EXCLUDED.firing,updated_at=now() WHERE system_alert_states.firing<>EXCLUDED.firing`, name, firing)
	return tag.RowsAffected() > 0, err
}
