-- Instance admin tokens (ADR 0019): admin:* scopes only, never tenant log access.
CREATE TABLE admin_tokens (
    id           uuid PRIMARY KEY,
    name         text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    prefix       text        NOT NULL,
    hash         bytea       NOT NULL UNIQUE,
    scopes       text[]      NOT NULL CHECK (cardinality(scopes) > 0),
    created_by   uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

-- Operators (admin socket, admin tokens) are not users: the trail keeps their name.
ALTER TABLE audit_events ADD COLUMN actor_name text NOT NULL DEFAULT '';

-- An API token issued by an operator has no member behind it and may only ingest.
ALTER TABLE api_tokens ADD COLUMN issued_by_operator boolean NOT NULL DEFAULT false;
