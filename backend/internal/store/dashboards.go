package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type DashboardPanel struct {
	ID              uuid.UUID  `json:"id"`
	WorkspaceID     uuid.UUID  `json:"workspace_id"`
	ProjectID       uuid.UUID  `json:"project_id"`
	SavedQueryID    uuid.UUID  `json:"saved_query_id"`
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	Field           string     `json:"field"`
	IntervalSeconds int        `json:"interval_seconds"`
	Limit           int        `json:"limit"`
	CreatedBy       *uuid.UUID `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

const panelCols = `id,workspace_id,project_id,saved_query_id,name,type,field,interval_seconds,limit_values,created_by,created_at,updated_at`

func scanPanel(r rowScanner) (DashboardPanel, error) {
	var p DashboardPanel
	err := r.Scan(&p.ID, &p.WorkspaceID, &p.ProjectID, &p.SavedQueryID, &p.Name, &p.Type, &p.Field, &p.IntervalSeconds, &p.Limit, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	return p, mapErr(err)
}
func (s *Store) ListDashboardPanels(ctx context.Context, ws, project uuid.UUID) ([]DashboardPanel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+panelCols+` FROM dashboard_panels WHERE workspace_id=$1 AND project_id=$2 ORDER BY created_at,id`, ws, project)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanPanel)
}
func (s *Store) DashboardPanelByID(ctx context.Context, id uuid.UUID) (DashboardPanel, error) {
	return scanPanel(s.pool.QueryRow(ctx, `SELECT `+panelCols+` FROM dashboard_panels WHERE id=$1`, id))
}
func (s *Store) SaveDashboardPanel(ctx context.Context, p DashboardPanel, create bool) (DashboardPanel, error) {
	if create {
		return scanPanel(s.pool.QueryRow(ctx, `INSERT INTO dashboard_panels(id,workspace_id,project_id,saved_query_id,name,type,field,interval_seconds,limit_values,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+panelCols, newID(), p.WorkspaceID, p.ProjectID, p.SavedQueryID, p.Name, p.Type, p.Field, p.IntervalSeconds, p.Limit, p.CreatedBy))
	}
	return scanPanel(s.pool.QueryRow(ctx, `UPDATE dashboard_panels SET saved_query_id=$3,name=$4,type=$5,field=$6,interval_seconds=$7,limit_values=$8,updated_at=now() WHERE workspace_id=$1 AND id=$2 RETURNING `+panelCols, p.WorkspaceID, p.ID, p.SavedQueryID, p.Name, p.Type, p.Field, p.IntervalSeconds, p.Limit))
}
func (s *Store) DeleteDashboardPanel(ctx context.Context, ws, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM dashboard_panels WHERE workspace_id=$1 AND id=$2`, ws, id))
}

type NotificationDelivery struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	Error     *string   `json:"error"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListChannelDeliveries(ctx context.Context, id uuid.UUID, limit int) ([]NotificationDelivery, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,status,error,created_at FROM notification_deliveries WHERE channel_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r rowScanner) (NotificationDelivery, error) {
		var d NotificationDelivery
		err := r.Scan(&d.ID, &d.Status, &d.Error, &d.CreatedAt)
		return d, err
	})
}
