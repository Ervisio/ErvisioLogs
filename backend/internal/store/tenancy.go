package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
)

// ---------- workspaces ----------

type Workspace struct {
	ID        uuid.UUID `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Role      string    `json:"role,omitempty"` // caller's role, filled by list/get-for-user
}

// CreateWorkspace creates the workspace, its system roles and makes owner the first owner.
func (s *Store) CreateWorkspace(ctx context.Context, owner uuid.UUID, slug, name string) (Workspace, error) {
	w := Workspace{ID: newID(), Slug: slug, Name: name, Role: "owner"}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO workspaces (id, slug, name) VALUES ($1,$2,$3) RETURNING created_at`,
			w.ID, slug, name).Scan(&w.CreatedAt); err != nil {
			return mapErr(err)
		}
		var ownerRole uuid.UUID
		for _, role := range auth.SystemRoleNames() {
			id := newID()
			if _, err := tx.Exec(ctx, `INSERT INTO roles (id, workspace_id, name, is_system) VALUES ($1,$2,$3,true)`, id, w.ID, role); err != nil {
				return mapErr(err)
			}
			for _, p := range auth.SystemRoles[role] {
				if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES ($1,$2)`, id, p); err != nil {
					return err
				}
			}
			if role == "owner" {
				ownerRole = id
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO workspace_members (workspace_id, user_id, role_id) VALUES ($1,$2,$3)`, w.ID, owner, ownerRole)
		return mapErr(err)
	})
	return w, err
}

// Membership is a user's standing in a workspace.
type Membership struct {
	Role  string
	Perms auth.PermSet
}

// MembershipOf returns ErrNotFound when the user is not a member.
func (s *Store) MembershipOf(ctx context.Context, workspaceID, userID uuid.UUID) (Membership, error) {
	return membershipOf(ctx, s.pool, workspaceID, userID)
}

func membershipOf(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, workspaceID, userID uuid.UUID) (Membership, error) {
	rows, err := q.Query(ctx, `SELECT r.name, rp.permission FROM workspace_members m
		JOIN roles r ON r.id = m.role_id AND r.workspace_id = m.workspace_id
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE m.workspace_id=$1 AND m.user_id=$2`, workspaceID, userID)
	if err != nil {
		return Membership{}, err
	}
	defer rows.Close()
	m := Membership{Perms: auth.PermSet{}}
	found := false
	for rows.Next() {
		var p *string
		if err := rows.Scan(&m.Role, &p); err != nil {
			return Membership{}, err
		}
		found = true
		if p != nil {
			m.Perms[*p] = true
		}
	}
	if err := rows.Err(); err != nil {
		return Membership{}, err
	}
	if !found {
		return Membership{}, ErrNotFound
	}
	return m, nil
}

func (s *Store) WorkspacesOf(ctx context.Context, userID uuid.UUID) ([]Workspace, error) {
	rows, err := s.pool.Query(ctx, `SELECT w.id, w.slug, w.name, w.created_at, r.name FROM workspace_members m
		JOIN workspaces w ON w.id = m.workspace_id JOIN roles r ON r.id = m.role_id
		WHERE m.user_id=$1 ORDER BY w.created_at, w.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedAt, &w.Role); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) GetWorkspace(ctx context.Context, id uuid.UUID) (Workspace, error) {
	var w Workspace
	err := s.pool.QueryRow(ctx, `SELECT id, slug, name, created_at FROM workspaces WHERE id=$1`, id).Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedAt)
	return w, mapErr(err)
}

func (s *Store) RenameWorkspace(ctx context.Context, id uuid.UUID, name string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE workspaces SET name=$2 WHERE id=$1`, id, name)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteWorkspace(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- members ----------

type Member struct {
	UserID   uuid.UUID `json:"user_id"`
	Email    string    `json:"email"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

func (s *Store) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]Member, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.email, u.username, r.name, m.created_at FROM workspace_members m
		JOIN users u ON u.id = m.user_id JOIN roles r ON r.id = m.role_id
		WHERE m.workspace_id=$1 ORDER BY m.created_at, u.id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Username, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) AddMember(ctx context.Context, workspaceID, userID uuid.UUID, role string) error {
	tag, err := s.pool.Exec(ctx, `INSERT INTO workspace_members (workspace_id, user_id, role_id)
		SELECT $1, $2, id FROM roles WHERE workspace_id=$1 AND name=$3`, workspaceID, userID, role)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound // unknown role
	}
	return nil
}

// MemberGuard authorises a membership change from the roles of the acting user and of the target, read
// under the lock that serialises such changes. A returned error aborts the change and is passed on.
type MemberGuard func(actor, target Membership) error

// guardMemberChange runs guard on the current roles. ErrNotFound when either user is not a member.
func guardMemberChange(ctx context.Context, tx pgx.Tx, workspaceID, actorID, userID uuid.UUID, guard MemberGuard) error {
	actor, err := membershipOf(ctx, tx, workspaceID, actorID)
	if err != nil {
		return err
	}
	target, err := membershipOf(ctx, tx, workspaceID, userID)
	if err != nil {
		return err
	}
	return guard(actor, target)
}

// SetMemberRole changes a member's role if guard allows it, refusing to demote the last owner.
func (s *Store) SetMemberRole(ctx context.Context, workspaceID, actorID, userID uuid.UUID, role string, guard MemberGuard) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if err := lockOwners(ctx, tx, workspaceID); err != nil {
			return err
		}
		if err := guardMemberChange(ctx, tx, workspaceID, actorID, userID, guard); err != nil {
			return err
		}
		if role != "owner" {
			if err := ensureOtherOwner(ctx, tx, workspaceID, userID); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE workspace_members SET role_id = (SELECT id FROM roles WHERE workspace_id=$1 AND name=$3)
			WHERE workspace_id=$1 AND user_id=$2 AND EXISTS (SELECT 1 FROM roles WHERE workspace_id=$1 AND name=$3)`, workspaceID, userID, role)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// RemoveMember removes a member if guard allows it, refusing to remove the last owner.
func (s *Store) RemoveMember(ctx context.Context, workspaceID, actorID, userID uuid.UUID, guard MemberGuard) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if err := lockOwners(ctx, tx, workspaceID); err != nil {
			return err
		}
		if err := guardMemberChange(ctx, tx, workspaceID, actorID, userID, guard); err != nil {
			return err
		}
		if err := ensureOtherOwner(ctx, tx, workspaceID, userID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func lockOwners(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, workspaceID.String())
	return err
}

// ensureOtherOwner fails with ErrLastOwner if userID is the only owner of the workspace.
func ensureOtherOwner(ctx context.Context, tx pgx.Tx, workspaceID, userID uuid.UUID) error {
	var isOwner bool
	var others int
	err := tx.QueryRow(ctx, `SELECT
		COALESCE(bool_or(m.user_id=$2), false),
		count(*) FILTER (WHERE m.user_id<>$2)
		FROM workspace_members m JOIN roles r ON r.id=m.role_id
		WHERE m.workspace_id=$1 AND r.name='owner'`, workspaceID, userID).Scan(&isOwner, &others)
	if err != nil {
		return err
	}
	if isOwner && others == 0 {
		return ErrLastOwner
	}
	return nil
}

// ---------- projects, environments, streams ----------

type Project struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateProject also creates the default "production" environment and "default" stream, so a
// new project can receive a first event right away.
func (s *Store) CreateProject(ctx context.Context, workspaceID uuid.UUID, slug, name string) (Project, error) {
	p := Project{ID: newID(), WorkspaceID: workspaceID, Slug: slug, Name: name}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO projects (id, workspace_id, slug, name) VALUES ($1,$2,$3,$4) RETURNING created_at`,
			p.ID, workspaceID, slug, name).Scan(&p.CreatedAt); err != nil {
			return mapErr(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO environments (id, workspace_id, project_id, name) VALUES ($1,$2,$3,'production')`, newID(), workspaceID, p.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO streams (id, workspace_id, project_id, name) VALUES ($1,$2,$3,'default')`, newID(), workspaceID, p.ID)
		return err
	})
	return p, err
}

func (s *Store) ListProjects(ctx context.Context, workspaceID uuid.UUID) ([]Project, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, workspace_id, slug, name, created_at FROM projects WHERE workspace_id=$1 ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.Slug, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProject(ctx context.Context, id uuid.UUID) (Project, error) {
	var p Project
	err := s.pool.QueryRow(ctx, `SELECT id, workspace_id, slug, name, created_at FROM projects WHERE id=$1`, id).
		Scan(&p.ID, &p.WorkspaceID, &p.Slug, &p.Name, &p.CreatedAt)
	return p, mapErr(err)
}

func (s *Store) RenameProject(ctx context.Context, workspaceID, id uuid.UUID, name string) error {
	return affected(s.pool.Exec(ctx, `UPDATE projects SET name=$3 WHERE id=$2 AND workspace_id=$1`, workspaceID, id, name))
}

func (s *Store) DeleteProject(ctx context.Context, workspaceID, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM projects WHERE id=$2 AND workspace_id=$1`, workspaceID, id))
}

// Meta is the display metadata of an environment or stream. Empty strings mean unset.
type Meta struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

// MetaPatch changes the fields that are not nil.
type MetaPatch struct {
	DisplayName, Description, Color *string
}

// Meta returns the patch as metadata for a new row, with absent fields unset.
func (p MetaPatch) Meta() Meta {
	deref := func(v *string) string {
		if v == nil {
			return ""
		}
		return *v
	}
	return Meta{DisplayName: deref(p.DisplayName), Description: deref(p.Description), Color: deref(p.Color)}
}

type Environment struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Meta
	CreatedAt time.Time `json:"created_at"`
}

type Stream struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Meta
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateEnvironment(ctx context.Context, workspaceID, projectID uuid.UUID, name string, m Meta) (Environment, error) {
	e := Environment{ID: newID(), ProjectID: projectID, Name: name, Meta: m}
	err := s.pool.QueryRow(ctx, `INSERT INTO environments (id, workspace_id, project_id, name, display_name, description, color)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`,
		e.ID, workspaceID, projectID, name, m.DisplayName, m.Description, m.Color).Scan(&e.CreatedAt)
	return e, mapErr(err)
}

func (s *Store) ListEnvironments(ctx context.Context, workspaceID, projectID uuid.UUID) ([]Environment, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, project_id, name, display_name, description, color, created_at
		FROM environments WHERE workspace_id=$1 AND project_id=$2 ORDER BY created_at, id`, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Environment{}
	for rows.Next() {
		var e Environment
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Name, &e.DisplayName, &e.Description, &e.Color, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) UpdateEnvironment(ctx context.Context, workspaceID, projectID, id uuid.UUID, p MetaPatch) (Environment, error) {
	var e Environment
	err := s.pool.QueryRow(ctx, `UPDATE environments SET display_name=COALESCE($4, display_name),
		description=COALESCE($5, description), color=COALESCE($6, color)
		WHERE id=$3 AND project_id=$2 AND workspace_id=$1
		RETURNING id, project_id, name, display_name, description, color, created_at`,
		workspaceID, projectID, id, p.DisplayName, p.Description, p.Color).
		Scan(&e.ID, &e.ProjectID, &e.Name, &e.DisplayName, &e.Description, &e.Color, &e.CreatedAt)
	return e, mapErr(err)
}

func (s *Store) DeleteEnvironment(ctx context.Context, workspaceID, projectID, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM environments WHERE id=$3 AND project_id=$2 AND workspace_id=$1`, workspaceID, projectID, id))
}

func (s *Store) CreateStream(ctx context.Context, workspaceID, projectID uuid.UUID, name string, m Meta) (Stream, error) {
	st := Stream{ID: newID(), ProjectID: projectID, Name: name, Type: "logs", Meta: m}
	err := s.pool.QueryRow(ctx, `INSERT INTO streams (id, workspace_id, project_id, name, display_name, description, color)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`,
		st.ID, workspaceID, projectID, name, m.DisplayName, m.Description, m.Color).Scan(&st.CreatedAt)
	return st, mapErr(err)
}

func (s *Store) ListStreams(ctx context.Context, workspaceID, projectID uuid.UUID) ([]Stream, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, project_id, name, type, display_name, description, color, created_at
		FROM streams WHERE workspace_id=$1 AND project_id=$2 ORDER BY created_at, id`, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Stream{}
	for rows.Next() {
		var st Stream
		if err := rows.Scan(&st.ID, &st.ProjectID, &st.Name, &st.Type, &st.DisplayName, &st.Description, &st.Color, &st.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) UpdateStream(ctx context.Context, workspaceID, projectID, id uuid.UUID, p MetaPatch) (Stream, error) {
	var st Stream
	err := s.pool.QueryRow(ctx, `UPDATE streams SET display_name=COALESCE($4, display_name),
		description=COALESCE($5, description), color=COALESCE($6, color)
		WHERE id=$3 AND project_id=$2 AND workspace_id=$1
		RETURNING id, project_id, name, type, display_name, description, color, created_at`,
		workspaceID, projectID, id, p.DisplayName, p.Description, p.Color).
		Scan(&st.ID, &st.ProjectID, &st.Name, &st.Type, &st.DisplayName, &st.Description, &st.Color, &st.CreatedAt)
	return st, mapErr(err)
}

func (s *Store) DeleteStream(ctx context.Context, workspaceID, projectID, id uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM streams WHERE id=$3 AND project_id=$2 AND workspace_id=$1`, workspaceID, projectID, id))
}
