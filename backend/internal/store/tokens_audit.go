package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
)

func affected(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- API tokens ----------

type APIToken struct {
	ID          uuid.UUID  `json:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	ProjectID   uuid.UUID  `json:"project_id"`
	Name        string     `json:"name"`
	Prefix      string     `json:"prefix"`
	Scopes      []string   `json:"scopes"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
}

const tokenCols = `id, workspace_id, project_id, name, prefix, scopes, created_at, expires_at, last_used_at, revoked_at`

type rowScanner interface{ Scan(...any) error }

func scanToken(r rowScanner) (APIToken, error) {
	var t APIToken
	err := r.Scan(&t.ID, &t.WorkspaceID, &t.ProjectID, &t.Name, &t.Prefix, &t.Scopes, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt)
	return t, mapErr(err)
}

// CreateToken stores a token. A nil createdBy means an operator issued it (ADR 0019).
func (s *Store) CreateToken(ctx context.Context, workspaceID, projectID uuid.UUID, createdBy *uuid.UUID, name, prefix string, hash []byte, scopes []string, expires *time.Time) (APIToken, error) {
	return scanToken(s.pool.QueryRow(ctx, `INSERT INTO api_tokens (id, workspace_id, project_id, name, prefix, hash, scopes, created_by, expires_at, issued_by_operator)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+tokenCols,
		newID(), workspaceID, projectID, name, prefix, hash, scopes, createdBy, expires, createdBy == nil))
}

func (s *Store) ListTokens(ctx context.Context, workspaceID, projectID uuid.UUID) ([]APIToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+tokenCols+` FROM api_tokens WHERE workspace_id=$1 AND project_id=$2 ORDER BY created_at DESC, id`, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetToken(ctx context.Context, id uuid.UUID) (APIToken, error) {
	return scanToken(s.pool.QueryRow(ctx, `SELECT `+tokenCols+` FROM api_tokens WHERE id=$1`, id))
}

// RevokeToken is idempotent: revoking a revoked token succeeds.
func (s *Store) RevokeToken(ctx context.Context, workspaceID, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `UPDATE api_tokens SET revoked_at = COALESCE(revoked_at, now()) WHERE id=$2 AND workspace_id=$1`, workspaceID, id))
}

// TokenByHash returns a token that is neither revoked nor expired, and whose creator is still an
// active member of the workspace. A token never exceeds its creator's permissions, evaluated on every
// call: Scopes holds the stored scopes the creator's current role allows. The stored scopes are left
// untouched, so restoring the creator's standing restores the token. A token an operator issued has no
// creator and keeps only logs:ingest. ErrNotFound when none remains.
func (s *Store) TokenByHash(ctx context.Context, hash []byte) (APIToken, error) {
	t, err := scanToken(s.pool.QueryRow(ctx, `SELECT t.id, t.workspace_id, t.project_id, t.name, t.prefix,
			ARRAY(SELECT x.scope FROM unnest(t.scopes) WITH ORDINALITY AS x(scope, ord)
				WHERE CASE WHEN t.issued_by_operator THEN x.scope = $2
					ELSE x.scope IN (SELECT rp.permission FROM role_permissions rp WHERE rp.role_id = m.role_id) END
				ORDER BY x.ord),
			t.created_at, t.expires_at, t.last_used_at, t.revoked_at
		FROM api_tokens t
		LEFT JOIN users u ON u.id = t.created_by AND u.status = 'active'
		LEFT JOIN workspace_members m ON m.workspace_id = t.workspace_id AND m.user_id = u.id
		WHERE t.hash=$1 AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > now())
			AND (t.issued_by_operator OR m.user_id IS NOT NULL)`, hash, auth.LogsIngest))
	if err != nil {
		return APIToken{}, err
	}
	if len(t.Scopes) == 0 {
		return APIToken{}, ErrNotFound
	}
	// last_used_at is advisory: refresh at most once a minute to keep the hot path write-free.
	_, _ = s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at=now() WHERE id=$1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, t.ID)
	return t, nil
}

// ---------- audit ----------

type AuditEvent struct {
	ID          uuid.UUID       `json:"id"`
	WorkspaceID *uuid.UUID      `json:"workspace_id"`
	ActorUserID *uuid.UUID      `json:"actor_user_id"`
	ActorToken  *uuid.UUID      `json:"actor_token_id"`
	ActorName   string          `json:"actor_name"`
	Action      string          `json:"action"`
	TargetType  string          `json:"target_type"`
	TargetID    string          `json:"target_id"`
	RequestID   string          `json:"request_id"`
	IP          string          `json:"ip"`
	Detail      json.RawMessage `json:"detail"`
	CreatedAt   time.Time       `json:"created_at"`
}

// RecordAudit stores a security event. Detail must not contain secrets.
func (s *Store) RecordAudit(ctx context.Context, e AuditEvent) error {
	if len(e.Detail) == 0 {
		e.Detail = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO audit_events (id, workspace_id, actor_user_id, actor_token_id, actor_name, action, target_type, target_id, request_id, ip, detail)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		newID(), e.WorkspaceID, e.ActorUserID, e.ActorToken, e.ActorName, e.Action, e.TargetType, e.TargetID, e.RequestID, e.IP, e.Detail)
	return err
}

// ListAudit returns a workspace's events newest first. cursor is opaque; next is empty on the last
// page. A malformed cursor gives ErrBadCursor.
func (s *Store) ListAudit(ctx context.Context, workspaceID uuid.UUID, limit int, cursor string) ([]AuditEvent, string, error) {
	return s.listAudit(ctx, `workspace_id = $4`, limit, cursor, workspaceID)
}

// ListInstanceAudit returns the instance-level events (those of no workspace), paginated like ListAudit.
func (s *Store) ListInstanceAudit(ctx context.Context, limit int, cursor string) ([]AuditEvent, string, error) {
	return s.listAudit(ctx, `workspace_id IS NULL`, limit, cursor)
}

func (s *Store) listAudit(ctx context.Context, scope string, limit int, cursor string, args ...any) (events []AuditEvent, next string, err error) {
	ts, id := time.Now().Add(24*time.Hour), uuid.Max
	if cursor != "" {
		if ts, id, err = decodeCursor(cursor); err != nil {
			return nil, "", err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT id, workspace_id, actor_user_id, actor_token_id, actor_name, action, target_type, target_id, request_id, ip, detail, created_at
		FROM audit_events WHERE `+scope+` AND (created_at, id) < ($1, $2)
		ORDER BY created_at DESC, id DESC LIMIT $3`, append([]any{ts, id, limit + 1}, args...)...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.ActorUserID, &e.ActorToken, &e.ActorName, &e.Action, &e.TargetType, &e.TargetID, &e.RequestID, &e.IP, &e.Detail, &e.CreatedAt); err != nil {
			return nil, "", err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(events) > limit {
		events = events[:limit]
		last := events[len(events)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	return events, next, nil
}

// encodeCursor and decodeCursor carry a keyset position (time, id) as an opaque page cursor.
func encodeCursor(ts time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(ts.Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(cursor string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	ts, id, ok := strings.Cut(string(raw), "|")
	if err != nil || !ok {
		return time.Time{}, uuid.Nil, ErrBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrBadCursor
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrBadCursor
	}
	return t, u, nil
}
