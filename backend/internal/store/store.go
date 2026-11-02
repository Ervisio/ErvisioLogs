// Package store is the PostgreSQL data access layer. Every query touching tenant data takes the
// workspace (directly or through a workspace-scoped parent) so isolation does not rest on callers alone.
package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrSetupDone = errors.New("setup already completed")
	ErrLastOwner = errors.New("a workspace needs at least one owner")
	ErrForbidden = errors.New("not allowed")
	ErrBadCursor = errors.New("invalid cursor")
	// ErrIdentityConflict means the account already has another identity of the provider, or the
	// identity belongs to another account.
	ErrIdentityConflict = errors.New("identity conflict")
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// mapErr translates driver errors into the package's sentinel errors.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505": // unique_violation
			return ErrConflict
		case "23503", "22P02": // foreign_key_violation, invalid_text_representation
			return ErrNotFound
		}
	}
	return err
}

func (s *Store) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------- users ----------

type User struct {
	ID              uuid.UUID `json:"id"`
	Email           string    `json:"email"`
	Username        string    `json:"username"`
	EmailVerified   bool      `json:"email_verified"`
	IsInstanceAdmin bool      `json:"is_instance_admin"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	HasPassword     bool      `json:"-"`
}

type NewUser struct {
	Email, Username string
	PasswordHash    *string
	EmailVerified   bool
	IsInstanceAdmin bool
}

const userCols = `id, email, username, email_verified, is_instance_admin, status, created_at, password_hash IS NOT NULL`

func scanUser(r pgx.Row) (User, error) {
	var u User
	err := r.Scan(&u.ID, &u.Email, &u.Username, &u.EmailVerified, &u.IsInstanceAdmin, &u.Status, &u.CreatedAt, &u.HasPassword)
	return u, mapErr(err)
}

func insertUser(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, n NewUser) (User, error) {
	return scanUser(q.QueryRow(ctx, `INSERT INTO users (id, email, username, password_hash, email_verified, is_instance_admin)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING `+userCols,
		newID(), n.Email, n.Username, n.PasswordHash, n.EmailVerified, n.IsInstanceAdmin))
}

func (s *Store) CreateUser(ctx context.Context, n NewUser) (User, error) {
	return insertUser(ctx, s.pool, n)
}

// CreateFirstAdmin creates the instance admin only while no user exists. The advisory lock makes
// two concurrent setups safe: exactly one wins, the other gets ErrSetupDone.
func (s *Store) CreateFirstAdmin(ctx context.Context, n NewUser) (User, error) {
	var u User
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('ervisio_logs_first_admin'))`); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return ErrSetupDone
		}
		n.IsInstanceAdmin = true
		var err error
		u, err = insertUser(ctx, tx, n)
		return err
	})
	return u, err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id=$1`, id))
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE email=$1`, email))
}

// UserForLogin finds an account by email or username and returns its password hash (nil when it has none).
func (s *Store) UserForLogin(ctx context.Context, login string) (User, *string, error) {
	var hash *string
	row := s.pool.QueryRow(ctx, `SELECT `+userCols+`, password_hash FROM users WHERE email=$1 OR username=$1`, login)
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.Username, &u.EmailVerified, &u.IsInstanceAdmin, &u.Status, &u.CreatedAt, &u.HasPassword, &hash); err != nil {
		return User{}, nil, mapErr(err)
	}
	return u, hash, nil
}

func (s *Store) UserByIdentity(ctx context.Context, provider, subject string) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id =
		(SELECT user_id FROM identities WHERE provider=$1 AND subject=$2)`, provider, subject))
}

// LinkIdentity attaches an external identity to an account. Linking the same identity again is a
// no-op. ErrIdentityConflict means the account has another subject of this provider, or the identity
// belongs to another account.
func (s *Store) LinkIdentity(ctx context.Context, userID uuid.UUID, provider, subject string) error {
	tag, err := s.pool.Exec(ctx, `INSERT INTO identities (user_id, provider, subject) VALUES ($1,$2,$3)
		ON CONFLICT (provider, subject) DO NOTHING`, userID, provider, subject)
	if err != nil {
		if err = mapErr(err); errors.Is(err, ErrConflict) {
			return ErrIdentityConflict
		}
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var owner uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT user_id FROM identities WHERE provider=$1 AND subject=$2`, provider, subject).Scan(&owner); err != nil {
		return mapErr(err)
	}
	if owner != userID {
		return ErrIdentityConflict
	}
	return nil
}

// CreateOAuthUser creates a verified account and links the identity atomically.
func (s *Store) CreateOAuthUser(ctx context.Context, n NewUser, provider, subject string) (User, error) {
	var u User
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		n.EmailVerified = true
		if u, err = insertUser(ctx, tx, n); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO identities (user_id, provider, subject) VALUES ($1,$2,$3)`, u.ID, provider, subject)
		return mapErr(err)
	})
	return u, err
}

func (s *Store) UsernameTaken(ctx context.Context, name string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE username=$1)`, name).Scan(&ok)
	return ok, err
}

func (s *Store) SetUserStatus(ctx context.Context, id uuid.UUID, status string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET status=$2 WHERE id=$1`, id, status)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if status != "active" {
			_, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
		}
		return err
	})
}

func (s *Store) ListUsers(ctx context.Context, limit int) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at, id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---------- sessions ----------

type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	CSRFToken string
	ExpiresAt time.Time
	User      User
}

func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, hash []byte, csrf string, expires time.Time, userAgent string) error {
	// The header is client-supplied bytes; PostgreSQL text accepts only valid UTF-8.
	userAgent = strings.ToValidUTF8(userAgent, "")
	if len(userAgent) > 300 {
		userAgent = strings.ToValidUTF8(userAgent[:300], "")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (id,user_id,secret_hash,csrf_token,expires_at,user_agent) VALUES ($1,$2,$3,$4,$5,$6)`,
		newID(), userID, hash, csrf, expires, userAgent)
	return err
}

// SessionByHash returns a live session of an active user.
func (s *Store) SessionByHash(ctx context.Context, hash []byte) (Session, error) {
	var se Session
	u := &se.User
	err := s.pool.QueryRow(ctx, `SELECT s.id, s.user_id, s.csrf_token, s.expires_at,
			u.id, u.email, u.username, u.email_verified, u.is_instance_admin, u.status, u.created_at, u.password_hash IS NOT NULL
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.secret_hash=$1 AND s.revoked_at IS NULL AND s.expires_at > now() AND u.status='active'`, hash).
		Scan(&se.ID, &se.UserID, &se.CSRFToken, &se.ExpiresAt,
			&u.ID, &u.Email, &u.Username, &u.EmailVerified, &u.IsInstanceAdmin, &u.Status, &u.CreatedAt, &u.HasPassword)
	return se, mapErr(err)
}

func (s *Store) RevokeSession(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, id)
	return err
}

// PurgeExpired deletes expired sessions (revoked ones included) and expired OAuth states.
func (s *Store) PurgeExpired(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM oauth_states WHERE expires_at < now()`)
	return err
}

// ---------- OAuth state ----------

type OAuthState struct{ Nonce, Verifier string }

func (s *Store) SaveOAuthState(ctx context.Context, hash []byte, provider, nonce, verifier string, expires time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_states (state_hash, provider, nonce, verifier, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		hash, provider, nonce, verifier, expires)
	return err
}

// ConsumeOAuthState atomically deletes and returns the state: it works exactly once.
func (s *Store) ConsumeOAuthState(ctx context.Context, hash []byte, provider string) (OAuthState, error) {
	var st OAuthState
	err := s.pool.QueryRow(ctx, `DELETE FROM oauth_states WHERE state_hash=$1 AND provider=$2 AND expires_at > now()
		RETURNING nonce, verifier`, hash, provider).Scan(&st.Nonce, &st.Verifier)
	return st, mapErr(err)
}
