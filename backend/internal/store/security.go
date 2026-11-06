package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TOTP struct {
	Secret      []byte
	Enabled     bool
	LastCounter int64
}

func (s *Store) TOTP(ctx context.Context, user uuid.UUID) (TOTP, error) {
	var t TOTP
	err := s.pool.QueryRow(ctx, `SELECT secret,enabled,last_counter FROM user_totp WHERE user_id=$1`, user).Scan(&t.Secret, &t.Enabled, &t.LastCounter)
	return t, mapErr(err)
}
func (s *Store) SetupTOTP(ctx context.Context, user uuid.UUID, secret []byte) error {
	tag, err := s.pool.Exec(ctx, `INSERT INTO user_totp(user_id,secret) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET secret=EXCLUDED.secret,last_counter=-1,created_at=now() WHERE NOT user_totp.enabled`, user, secret)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return mapErr(err)
}
func (s *Store) AcceptTOTP(ctx context.Context, user uuid.UUID, counter int64, secret []byte) error {
	return affected(s.pool.Exec(ctx, `UPDATE user_totp SET last_counter=$2 WHERE user_id=$1 AND last_counter<$2 AND secret=$3 AND enabled`, user, counter, secret))
}
func (s *Store) DeleteTOTP(ctx context.Context, user uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM user_totp WHERE user_id=$1`, user))
}
func (s *Store) CreateChallenge(ctx context.Context, hash []byte, user uuid.UUID, purpose string, data json.RawMessage, expires time.Time) error {
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM auth_challenges WHERE expires_at<now() OR (user_id=$1 AND purpose=$2)`, user, purpose); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO auth_challenges(hash,user_id,purpose,data,expires_at) VALUES($1,$2,$3,$4,$5)`, hash, user, purpose, data, expires)
		return err
	})
}

type AuthChallenge struct {
	UserID uuid.UUID
	Data   json.RawMessage
}

func (s *Store) AttemptChallenge(ctx context.Context, hash []byte, purpose string) (AuthChallenge, error) {
	var c AuthChallenge
	err := s.pool.QueryRow(ctx, `UPDATE auth_challenges SET attempts=attempts+1 WHERE hash=$1 AND purpose=$2 AND expires_at>now() AND attempts<5 RETURNING user_id,data`, hash, purpose).Scan(&c.UserID, &c.Data)
	return c, mapErr(err)
}
func (s *Store) ConsumeChallenge(ctx context.Context, hash []byte) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM auth_challenges WHERE hash=$1 AND expires_at>now()`, hash))
}

type Passkey struct {
	ID           uuid.UUID       `json:"id"`
	UserID       uuid.UUID       `json:"-"`
	Name         string          `json:"name"`
	CredentialID []byte          `json:"-"`
	Credential   json.RawMessage `json:"-"`
	CreatedAt    time.Time       `json:"created_at"`
	LastUsedAt   *time.Time      `json:"last_used_at"`
}

func scanPasskey(r rowScanner) (Passkey, error) {
	var p Passkey
	err := r.Scan(&p.ID, &p.UserID, &p.Name, &p.CredentialID, &p.Credential, &p.CreatedAt, &p.LastUsedAt)
	return p, mapErr(err)
}

const passkeyCols = `id,user_id,name,credential_id,credential,created_at,last_used_at`

func (s *Store) ListPasskeys(ctx context.Context, user uuid.UUID) ([]Passkey, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+passkeyCols+` FROM user_passkeys WHERE user_id=$1 ORDER BY created_at,id`, user)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanPasskey)
}
func (s *Store) CreatePasskey(ctx context.Context, p Passkey) (Passkey, error) {
	return scanPasskey(s.pool.QueryRow(ctx, `INSERT INTO user_passkeys(id,user_id,name,credential_id,credential) VALUES($1,$2,$3,$4,$5) RETURNING `+passkeyCols, newID(), p.UserID, p.Name, p.CredentialID, p.Credential))
}
func (s *Store) DeletePasskey(ctx context.Context, user, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM user_passkeys WHERE user_id=$1 AND id=$2`, user, id))
}
func (s *Store) UpdatePasskey(ctx context.Context, user uuid.UUID, id []byte, old, new json.RawMessage) error {
	return affected(s.pool.Exec(ctx, `UPDATE user_passkeys SET credential=$4,last_used_at=now() WHERE user_id=$1 AND credential_id=$2 AND credential=$3`, user, id, old, new))
}
func (s *Store) RotateToken(ctx context.Context, ws, id uuid.UUID, createdBy *uuid.UUID, prefix string, hash []byte) (APIToken, error) {
	var out APIToken
	err := s.tx(ctx, func(tx pgx.Tx) error {
		old, err := scanToken(tx.QueryRow(ctx, `UPDATE api_tokens SET revoked_at=now() WHERE workspace_id=$1 AND id=$2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now()) RETURNING `+tokenCols, ws, id))
		if err != nil {
			return err
		}
		out, err = scanToken(tx.QueryRow(ctx, `INSERT INTO api_tokens(id,workspace_id,project_id,name,prefix,hash,scopes,created_by,expires_at,issued_by_operator) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+tokenCols, newID(), old.WorkspaceID, old.ProjectID, old.Name, prefix, hash, old.Scopes, createdBy, old.ExpiresAt, createdBy == nil))
		return err
	})
	return out, err
}
func (s *Store) SetRecoveryCodes(ctx context.Context, user uuid.UUID, hashes [][]byte) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id=$1`, user); err != nil {
			return err
		}
		for _, hash := range hashes {
			if _, err := tx.Exec(ctx, `INSERT INTO user_recovery_codes(user_id,hash) VALUES($1,$2)`, user, hash); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Store) ConsumeRecoveryCode(ctx context.Context, user uuid.UUID, hash []byte) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id=$1 AND hash=$2`, user, hash))
}
func (s *Store) SessionCreatedAt(ctx context.Context, user, id uuid.UUID) (time.Time, error) {
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT created_at FROM sessions WHERE user_id=$1 AND id=$2 AND revoked_at IS NULL AND expires_at>now()`, user, id).Scan(&at)
	return at, mapErr(err)
}
func (s *Store) EnableTOTP(ctx context.Context, user uuid.UUID, secret []byte, counter int64, hashes [][]byte, keep uuid.UUID) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if err := affected(tx.Exec(ctx, `UPDATE user_totp SET enabled=true,last_counter=$3 WHERE user_id=$1 AND secret=$2 AND NOT enabled AND last_counter<$3 AND created_at>now()-interval '10 minutes'`, user, secret, counter)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id=$1`, user); err != nil {
			return err
		}
		for _, hash := range hashes {
			if _, err := tx.Exec(ctx, `INSERT INTO user_recovery_codes(user_id,hash) VALUES($1,$2)`, user, hash); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, user, keep)
		return err
	})
}
