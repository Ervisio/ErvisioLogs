package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AdminToken is an instance admin token (ADR 0019): admin:* scopes, no workspace, no log access.
type AdminToken struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	CreatedBy  *uuid.UUID `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

const adminTokenCols = `id, name, prefix, scopes, created_by, created_at, expires_at, last_used_at, revoked_at`

func scanAdminToken(r rowScanner) (AdminToken, error) {
	var t AdminToken
	err := r.Scan(&t.ID, &t.Name, &t.Prefix, &t.Scopes, &t.CreatedBy, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt)
	return t, mapErr(err)
}

func (s *Store) CreateAdminToken(ctx context.Context, createdBy uuid.UUID, name, prefix string, hash []byte, scopes []string, expires time.Time) (AdminToken, error) {
	return scanAdminToken(s.pool.QueryRow(ctx, `INSERT INTO admin_tokens (id, name, prefix, hash, scopes, created_by, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+adminTokenCols,
		newID(), name, prefix, hash, scopes, createdBy, expires))
}

func (s *Store) ListAdminTokens(ctx context.Context) ([]AdminToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+adminTokenCols+` FROM admin_tokens ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminToken{}
	for rows.Next() {
		t, err := scanAdminToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeAdminToken is idempotent: revoking a revoked token succeeds.
func (s *Store) RevokeAdminToken(ctx context.Context, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `UPDATE admin_tokens SET revoked_at = COALESCE(revoked_at, now()) WHERE id=$1`, id))
}

// AdminTokenByHash returns a live admin token whose creator is still an active instance administrator,
// so disabling, demoting or deleting the creator stops the token. ErrNotFound otherwise.
func (s *Store) AdminTokenByHash(ctx context.Context, hash []byte) (AdminToken, error) {
	t, err := scanAdminToken(s.pool.QueryRow(ctx, `SELECT t.id, t.name, t.prefix, t.scopes, t.created_by, t.created_at,
			t.expires_at, t.last_used_at, t.revoked_at
		FROM admin_tokens t JOIN users u ON u.id = t.created_by AND u.status = 'active' AND u.is_instance_admin
		WHERE t.hash=$1 AND t.revoked_at IS NULL AND t.expires_at > now()`, hash))
	if err != nil {
		return AdminToken{}, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE admin_tokens SET last_used_at=now() WHERE id=$1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, t.ID)
	return t, nil
}
