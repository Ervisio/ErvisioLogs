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
	ErrLastAdmin = errors.New("the instance needs at least one active administrator")
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

// UpdateUser changes status and instance administration (nil leaves a field as is). Disabling
// revokes the user's sessions; leaving the instance without an active administrator is refused.
func (s *Store) UpdateUser(ctx context.Context, id uuid.UUID, status *string, admin *bool) error {
	disable := status != nil && *status != "active"
	return s.tx(ctx, func(tx pgx.Tx) error {
		if disable || (admin != nil && !*admin) {
			if err := ensureOtherAdmin(ctx, tx, id); err != nil {
				return err
			}
		}
		if err := affected(tx.Exec(ctx, `UPDATE users SET status=COALESCE($2, status),
			is_instance_admin=COALESCE($3, is_instance_admin) WHERE id=$1`, id, status, admin)); err != nil {
			return err
		}
		if !disable {
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
		return err
	})
}

// DeleteUser removes an account with its sessions, identities and memberships. It refuses to delete
// the last active administrator (ErrLastAdmin) or the only owner of a workspace (ErrLastOwner).
// Tokens the user created stop working, since a token never outlives its creator's standing.
func (s *Store) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if err := ensureOtherAdmin(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT m.workspace_id FROM workspace_members m JOIN roles r ON r.id=m.role_id
			WHERE m.user_id=$1 AND r.name='owner' ORDER BY m.workspace_id`, id)
		if err != nil {
			return err
		}
		owned, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		// Sorted ids keep the advisory locks in one order across concurrent deletions.
		for _, ws := range owned {
			if err := lockOwners(ctx, tx, ws); err != nil {
				return err
			}
			if err := ensureOtherOwner(ctx, tx, ws, id); err != nil {
				return err
			}
		}
		return affected(tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, id))
	})
}

// ensureOtherAdmin fails with ErrLastAdmin if userID is the only active instance administrator. The
// lock serialises changes that could each remove one of two remaining administrators.
func ensureOtherAdmin(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('ervisio_logs_instance_admins'))`); err != nil {
		return err
	}
	var isAdmin bool
	var others int
	err := tx.QueryRow(ctx, `SELECT COALESCE(bool_or(id=$1), false), count(*) FILTER (WHERE id<>$1)
		FROM users WHERE is_instance_admin AND status='active'`, userID).Scan(&isAdmin, &others)
	if err != nil {
		return err
	}
	if isAdmin && others == 0 {
		return ErrLastAdmin
	}
	return nil
}

// ListUsers pages accounts by creation, oldest first. query matches email or username, case-insensitive.
func (s *Store) ListUsers(ctx context.Context, query string, limit int, cursor string) ([]User, string, error) {
	after, afterID := time.Time{}, uuid.Nil
	if cursor != "" {
		var err error
		if after, afterID, err = decodeCursor(cursor); err != nil {
			return nil, "", err
		}
	}
	pattern := ""
	if query != "" {
		pattern = "%" + likeEscaper.Replace(strings.ToLower(query)) + "%"
	}
	rows, err := s.pool.Query(ctx, `SELECT `+userCols+` FROM users
		WHERE (created_at, id) > ($1, $2) AND ($3 = '' OR email LIKE $3 OR username LIKE $3)
		ORDER BY created_at, id LIMIT $4`, after, afterID, pattern, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	return out, next, nil
}

// likeEscaper makes user input literal inside a LIKE pattern (backslash is the default escape).
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// PasswordHash returns the account's password hash, nil when it signs in only through a provider.
func (s *Store) PasswordHash(ctx context.Context, id uuid.UUID) (*string, error) {
	var h *string
	err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, id).Scan(&h)
	return h, mapErr(err)
}

// SetPassword replaces the password and revokes every session of the user except keep (uuid.Nil: all).
func (s *Store) SetPassword(ctx context.Context, id uuid.UUID, hash string, keep uuid.UUID) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if err := affected(tx.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, id, hash)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, id, keep)
		return err
	})
}

// ---------- sessions ----------

type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	CSRFToken string
	ExpiresAt time.Time
	User      User
}

// CreateSession stores a new session and returns its id.
func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, hash []byte, csrf string, expires time.Time, userAgent, ip string) (uuid.UUID, error) {
	// The header is client-supplied bytes; PostgreSQL text accepts only valid UTF-8.
	userAgent = strings.ToValidUTF8(userAgent, "")
	if len(userAgent) > 300 {
		userAgent = strings.ToValidUTF8(userAgent[:300], "")
	}
	id := newID()
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (id,user_id,secret_hash,csrf_token,expires_at,user_agent,ip,last_seen_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,now())`, id, userID, hash, csrf, expires, userAgent, ip)
	return id, err
}

// SessionByHash returns a live session of an active user and records its use.
func (s *Store) SessionByHash(ctx context.Context, hash []byte, ip string) (Session, error) {
	var se Session
	var lastSeen *time.Time
	u := &se.User
	err := s.pool.QueryRow(ctx, `SELECT s.id, s.user_id, s.csrf_token, s.expires_at, s.last_seen_at,
			u.id, u.email, u.username, u.email_verified, u.is_instance_admin, u.status, u.created_at, u.password_hash IS NOT NULL
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.secret_hash=$1 AND s.revoked_at IS NULL AND s.expires_at > now() AND u.status='active'`, hash).
		Scan(&se.ID, &se.UserID, &se.CSRFToken, &se.ExpiresAt, &lastSeen,
			&u.ID, &u.Email, &u.Username, &u.EmailVerified, &u.IsInstanceAdmin, &u.Status, &u.CreatedAt, &u.HasPassword)
	if err != nil {
		return se, mapErr(err)
	}
	// Advisory, like a token's last_used_at: written at most once a minute to keep requests read-only.
	if lastSeen == nil || time.Since(*lastSeen) > time.Minute {
		_, _ = s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at=now(), ip=$2 WHERE id=$1`, se.ID, ip)
	}
	return se, nil
}

// SessionInfo describes a live session for the session lists; Current is set by the caller.
type SessionInfo struct {
	ID         uuid.UUID  `json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	IP         string     `json:"ip"`
	UserAgent  string     `json:"user_agent"`
	Current    bool       `json:"current"`
}

// ListSessions returns the user's live sessions, most recently used first.
func (s *Store) ListSessions(ctx context.Context, userID uuid.UUID) ([]SessionInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, created_at, expires_at, last_seen_at, ip, user_agent FROM sessions
		WHERE user_id=$1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY COALESCE(last_seen_at, created_at) DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionInfo{}
	for rows.Next() {
		var si SessionInfo
		if err := rows.Scan(&si.ID, &si.CreatedAt, &si.ExpiresAt, &si.LastSeenAt, &si.IP, &si.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

// RevokeSession ends one live session of the user; ErrNotFound if there is none with that id.
func (s *Store) RevokeSession(ctx context.Context, userID, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now()
		WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at > now()`, id, userID))
}

// RevokeSessions ends every live session of the user except keep (uuid.Nil: all) and returns how many.
func (s *Store) RevokeSessions(ctx context.Context, userID, keep uuid.UUID) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at=now()
		WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL AND expires_at > now()`, userID, keep)
	return tag.RowsAffected(), err
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
