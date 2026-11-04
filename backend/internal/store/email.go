package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------- account tokens ----------

// Purposes of a single-use account token sent by email.
const (
	PurposeReset  = "password_reset"
	PurposeVerify = "email_verify"
)

// CreateAccountToken stores a token for the user, replacing the earlier ones of the same purpose so
// only the latest link works.
func (s *Store) CreateAccountToken(ctx context.Context, userID uuid.UUID, purpose string, hash []byte, expires time.Time) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM account_tokens WHERE user_id=$1 AND purpose=$2`, userID, purpose); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO account_tokens (token_hash, user_id, purpose, expires_at) VALUES ($1,$2,$3,$4)`,
			hash, userID, purpose, expires)
		return mapErr(err)
	})
}

// AccountTokenUser returns the active user a live token belongs to, without using the token.
func (s *Store) AccountTokenUser(ctx context.Context, hash []byte, purpose string) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE status='active' AND id =
		(SELECT user_id FROM account_tokens WHERE token_hash=$1 AND purpose=$2 AND expires_at > now())`, hash, purpose))
}

// ResetPassword uses a reset token of the user to set a new password and signs them out everywhere.
// The link reached the inbox, so the address counts as verified. ErrNotFound when the token is gone.
func (s *Store) ResetPassword(ctx context.Context, hash []byte, userID uuid.UUID, password string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if err := affected(tx.Exec(ctx, `DELETE FROM account_tokens
			WHERE token_hash=$1 AND user_id=$2 AND purpose=$3 AND expires_at > now()`, hash, userID, PurposeReset)); err != nil {
			return err
		}
		if err := affected(tx.Exec(ctx, `UPDATE users SET password_hash=$2, email_verified=true WHERE id=$1 AND status='active'`,
			userID, password)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
		return err
	})
}

// VerifyEmail uses a verification token and marks the user's address verified.
func (s *Store) VerifyEmail(ctx context.Context, hash []byte) (User, error) {
	var u User
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var userID uuid.UUID
		if err := tx.QueryRow(ctx, `DELETE FROM account_tokens WHERE token_hash=$1 AND purpose=$2 AND expires_at > now()
			RETURNING user_id`, hash, PurposeVerify).Scan(&userID); err != nil {
			return mapErr(err)
		}
		var err error
		u, err = scanUser(tx.QueryRow(ctx, `UPDATE users SET email_verified=true WHERE id=$1 AND status='active' RETURNING `+userCols, userID))
		return err
	})
	return u, err
}

// ---------- invitations ----------

type Invitation struct {
	ID          uuid.UUID  `json:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	InvitedBy   *uuid.UUID `json:"invited_by"`
	Emailed     bool       `json:"emailed"`
	ExpiresAt   time.Time  `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

const invitationCols = `id, workspace_id, email, role, invited_by, emailed, expires_at, created_at`

func scanInvitation(r pgx.Row) (Invitation, error) {
	var i Invitation
	err := r.Scan(&i.ID, &i.WorkspaceID, &i.Email, &i.Role, &i.InvitedBy, &i.Emailed, &i.ExpiresAt, &i.CreatedAt)
	return i, mapErr(err)
}

// CreateInvitation invites email to the workspace. Inviting the same address again replaces the earlier
// invitation and its link. A nil invitedBy is an operator. ErrConflict when the address already belongs to a member.
func (s *Store) CreateInvitation(ctx context.Context, workspaceID uuid.UUID, invitedBy *uuid.UUID, email, role string, hash []byte, expires time.Time) (Invitation, error) {
	var inv Invitation
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var member bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace_members m JOIN users u ON u.id = m.user_id
			WHERE m.workspace_id=$1 AND u.email=$2)`, workspaceID, email).Scan(&member); err != nil {
			return err
		}
		if member {
			return ErrConflict
		}
		var err error
		inv, err = scanInvitation(tx.QueryRow(ctx, `INSERT INTO invitations (id, workspace_id, email, role, token_hash, invited_by, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (workspace_id, email) DO UPDATE SET role=EXCLUDED.role, token_hash=EXCLUDED.token_hash,
				invited_by=EXCLUDED.invited_by, emailed=false, expires_at=EXCLUDED.expires_at, created_at=now()
			RETURNING `+invitationCols, newID(), workspaceID, email, role, hash, invitedBy, expires))
		return err
	})
	return inv, err
}

// MarkInvitationEmailed records that the link went to the invited address only; the token pins the
// version, so a newer invitation of the same address is left alone.
func (s *Store) MarkInvitationEmailed(ctx context.Context, hash []byte) error {
	return affected(s.pool.Exec(ctx, `UPDATE invitations SET emailed=true WHERE token_hash=$1`, hash))
}

// ListInvitations returns the workspace's pending invitations, newest first.
func (s *Store) ListInvitations(ctx context.Context, workspaceID uuid.UUID) ([]Invitation, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+invitationCols+` FROM invitations WHERE workspace_id=$1 AND expires_at > now()
		ORDER BY created_at DESC, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		i, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) DeleteInvitation(ctx context.Context, workspaceID, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM invitations WHERE id=$2 AND workspace_id=$1`, workspaceID, id))
}

// InvitationPreview is what the holder of a link sees before accepting it.
type InvitationPreview struct {
	WorkspaceName string    `json:"workspace_name"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	ExpiresAt     time.Time `json:"expires_at"`
	HasAccount    bool      `json:"has_account"`
}

func (s *Store) PreviewInvitation(ctx context.Context, hash []byte) (InvitationPreview, error) {
	var p InvitationPreview
	err := s.pool.QueryRow(ctx, `SELECT w.name, i.email, i.role, i.expires_at, EXISTS (SELECT 1 FROM users u WHERE u.email = i.email)
		FROM invitations i JOIN workspaces w ON w.id = i.workspace_id
		WHERE i.token_hash=$1 AND i.expires_at > now()`, hash).Scan(&p.WorkspaceName, &p.Email, &p.Role, &p.ExpiresAt, &p.HasAccount)
	return p, mapErr(err)
}

// AcceptInvitation adds the user to the invitation's workspace and uses the invitation up. The user's
// address must be the invited one (ErrForbidden otherwise); an emailed link also verifies it.
func (s *Store) AcceptInvitation(ctx context.Context, hash []byte, userID uuid.UUID) (Invitation, error) {
	var inv Invitation
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		if inv, err = takeInvitation(ctx, tx, hash); err != nil {
			return err
		}
		var email string
		if err := tx.QueryRow(ctx, `SELECT email FROM users WHERE id=$1 AND status='active'`, userID).Scan(&email); err != nil {
			return mapErr(err)
		}
		if email != inv.Email {
			return ErrForbidden
		}
		if inv.Emailed {
			if _, err := tx.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, userID); err != nil {
				return err
			}
		}
		return joinInvited(ctx, tx, inv, userID)
	})
	return inv, err
}

// AcceptInvitationNewUser creates the account for the invited address and adds it to the workspace.
// The account is verified only when the link was emailed. ErrConflict when the address or username
// is taken.
func (s *Store) AcceptInvitationNewUser(ctx context.Context, hash []byte, n NewUser) (User, Invitation, error) {
	var u User
	var inv Invitation
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		if inv, err = takeInvitation(ctx, tx, hash); err != nil {
			return err
		}
		n.Email, n.EmailVerified = inv.Email, inv.Emailed
		if u, err = insertUser(ctx, tx, n); err != nil {
			return err
		}
		return joinInvited(ctx, tx, inv, u.ID)
	})
	return u, inv, err
}

// takeInvitation deletes and returns a live invitation: concurrent accepts of one link cannot both win.
func takeInvitation(ctx context.Context, tx pgx.Tx, hash []byte) (Invitation, error) {
	return scanInvitation(tx.QueryRow(ctx, `DELETE FROM invitations WHERE token_hash=$1 AND expires_at > now() RETURNING `+invitationCols, hash))
}

// joinInvited adds the user with the invited role; an existing membership is kept as it is.
func joinInvited(ctx context.Context, tx pgx.Tx, inv Invitation, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO workspace_members (workspace_id, user_id, role_id)
		SELECT $1, $2, id FROM roles WHERE workspace_id=$1 AND name=$3
		ON CONFLICT DO NOTHING`, inv.WorkspaceID, userID, inv.Role)
	return mapErr(err)
}
