-- Single-use links sent by email: password reset and address verification. Only the SHA-256 of
-- the token is stored; a used token is deleted.
CREATE TABLE account_tokens (
    token_hash bytea       PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text        NOT NULL CHECK (purpose IN ('password_reset', 'email_verify')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX account_tokens_user ON account_tokens (user_id, purpose);

-- Pending invitations to a workspace, also for addresses with no account yet. Accepting one
-- deletes it. emailed records whether the link went only to the invited address: then accepting
-- it proves the address.
CREATE TABLE invitations (
    id           uuid PRIMARY KEY,
    workspace_id uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        text        NOT NULL,
    role         text        NOT NULL,
    token_hash   bytea       NOT NULL UNIQUE,
    invited_by   uuid        REFERENCES users (id) ON DELETE SET NULL,
    emailed      boolean     NOT NULL DEFAULT false,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, email)
);
