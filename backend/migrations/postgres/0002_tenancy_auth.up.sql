-- P1: identities, sessions, tenancy, RBAC, API tokens, audit.
-- Tenant scoping is enforced by the schema as well as by the service layer: child tables carry
-- workspace_id and reference their parent with a composite foreign key, so a row can never point
-- at a parent of another workspace.

CREATE TABLE users (
    id                uuid PRIMARY KEY,
    email             text        NOT NULL,
    username          text        NOT NULL,
    password_hash     text,                       -- NULL for accounts that only use external providers
    email_verified    boolean     NOT NULL DEFAULT false,
    is_instance_admin boolean     NOT NULL DEFAULT false,
    status            text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_format CHECK (email = lower(email) AND position('@' in email) > 1),
    CONSTRAINT users_username_format CHECK (username ~ '^[a-z0-9][a-z0-9_.-]{2,31}$')
);
CREATE UNIQUE INDEX users_email_key ON users (email);
CREATE UNIQUE INDEX users_username_key ON users (username);

-- External identity (Google, Cognito, Discord). (provider, subject) is globally unique.
CREATE TABLE identities (
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider   text        NOT NULL CHECK (provider IN ('google', 'cognito', 'discord')),
    subject    text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject),
    CONSTRAINT identities_user_provider_key UNIQUE (user_id, provider)  -- one identity per provider per account
);

-- Only a SHA-256 of the session secret is stored; a database leak does not yield usable sessions.
CREATE TABLE sessions (
    id          uuid PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    secret_hash bytea       NOT NULL UNIQUE,
    csrf_token  text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    user_agent  text        NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- Short-lived state for the OAuth/OIDC authorisation code flow (state, nonce, PKCE verifier).
CREATE TABLE oauth_states (
    state_hash bytea PRIMARY KEY,
    provider   text        NOT NULL,
    nonce      text        NOT NULL,
    verifier   text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX oauth_states_expires_idx ON oauth_states (expires_at);

CREATE TABLE workspaces (
    id         uuid PRIMARY KEY,
    slug       text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,38}$'),
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    id           uuid PRIMARY KEY,
    workspace_id uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text        NOT NULL CHECK (length(name) BETWEEN 1 AND 50),
    is_system    boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name),
    UNIQUE (id, workspace_id)
);

CREATE TABLE role_permissions (
    role_id    uuid NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission text NOT NULL,
    PRIMARY KEY (role_id, permission)
);

CREATE TABLE workspace_members (
    workspace_id uuid        NOT NULL,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id      uuid        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id),
    FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    FOREIGN KEY (role_id, workspace_id) REFERENCES roles (id, workspace_id)
);
CREATE INDEX workspace_members_user_idx ON workspace_members (user_id);

CREATE TABLE projects (
    id           uuid PRIMARY KEY,
    workspace_id uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    slug         text        NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,38}$'),
    name         text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, slug),
    UNIQUE (id, workspace_id)
);

CREATE TABLE environments (
    id           uuid PRIMARY KEY,
    workspace_id uuid        NOT NULL,
    project_id   uuid        NOT NULL,
    name         text        NOT NULL CHECK (name ~ '^[a-z0-9][a-z0-9-]{0,38}$'),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, name),
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE
);

CREATE TABLE streams (
    id           uuid PRIMARY KEY,
    workspace_id uuid        NOT NULL,
    project_id   uuid        NOT NULL,
    name         text        NOT NULL CHECK (name ~ '^[a-z0-9][a-z0-9._-]{0,62}$'),
    type         text        NOT NULL DEFAULT 'logs' CHECK (type IN ('logs')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, name),
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE
);

-- API tokens are shown once. Only an HMAC-SHA256 keyed with the instance secret is stored.
CREATE TABLE api_tokens (
    id           uuid PRIMARY KEY,
    workspace_id uuid        NOT NULL,
    project_id   uuid        NOT NULL,
    name         text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    prefix       text        NOT NULL,            -- first characters, safe to display
    hash         bytea       NOT NULL UNIQUE,
    scopes       text[]      NOT NULL CHECK (cardinality(scopes) > 0),
    created_by   uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE
);
CREATE INDEX api_tokens_project_idx ON api_tokens (workspace_id, project_id);

CREATE TABLE audit_events (
    id           uuid PRIMARY KEY,
    workspace_id uuid REFERENCES workspaces (id) ON DELETE CASCADE,  -- NULL: instance-level event
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    actor_token_id uuid,
    action       text        NOT NULL,
    target_type  text        NOT NULL DEFAULT '',
    target_id    text        NOT NULL DEFAULT '',
    request_id   text        NOT NULL DEFAULT '',
    ip           text        NOT NULL DEFAULT '',
    detail       jsonb       NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_ws_time_idx ON audit_events (workspace_id, created_at DESC, id DESC);
CREATE INDEX audit_events_instance_time_idx ON audit_events (created_at DESC, id DESC) WHERE workspace_id IS NULL;
