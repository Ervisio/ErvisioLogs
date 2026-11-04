package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Setting is a stored runtime setting (ADR 0018). WorkspaceID and ProjectID are nil for the
// instance; ProjectID is nil for a workspace.
type Setting struct {
	WorkspaceID *uuid.UUID
	ProjectID   *uuid.UUID
	Key         string
	Value       json.RawMessage
}

func (s *Store) ListSettings(ctx context.Context) ([]Setting, error) {
	rows, err := s.pool.Query(ctx, `SELECT workspace_id, project_id, key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Setting{}
	for rows.Next() {
		var st Setting
		if err := rows.Scan(&st.WorkspaceID, &st.ProjectID, &st.Key, &st.Value); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// PutSetting stores the value of a key at a scope, replacing the previous one.
func (s *Store) PutSetting(ctx context.Context, st Setting, by *uuid.UUID) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM settings WHERE key=$1 AND workspace_id IS NOT DISTINCT FROM $2
			AND project_id IS NOT DISTINCT FROM $3`, st.Key, st.WorkspaceID, st.ProjectID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO settings (workspace_id, project_id, key, value, updated_by) VALUES ($1,$2,$3,$4,$5)`,
			st.WorkspaceID, st.ProjectID, st.Key, st.Value, by)
		return mapErr(err)
	})
}

// DeleteSetting removes the value of a key at a scope; a missing row is not an error.
func (s *Store) DeleteSetting(ctx context.Context, workspaceID, projectID *uuid.UUID, key string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM settings WHERE key=$1 AND workspace_id IS NOT DISTINCT FROM $2
		AND project_id IS NOT DISTINCT FROM $3`, key, workspaceID, projectID)
	return err
}
