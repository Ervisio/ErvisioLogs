ALTER TABLE notification_channels DROP CONSTRAINT notification_channels_type_check;
ALTER TABLE notification_channels ADD CONSTRAINT notification_channels_type_check CHECK (type IN ('webhook','email','telegram'));
CREATE TABLE notification_deliveries (
 id uuid PRIMARY KEY,
 channel_id uuid NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
 status text NOT NULL CHECK (status IN ('ok','error')),
 error text,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_deliveries_channel ON notification_deliveries(channel_id,created_at DESC,id DESC);
CREATE TABLE dashboard_panels (
 id uuid PRIMARY KEY,
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 saved_query_id uuid NOT NULL REFERENCES saved_queries(id) ON DELETE CASCADE,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 100),
 type text NOT NULL CHECK(type IN ('count','timeseries','top')),
 field text NOT NULL DEFAULT 'level',
 interval_seconds integer NOT NULL DEFAULT 3600 CHECK(interval_seconds BETWEEN 60 AND 86400),
 limit_values integer NOT NULL DEFAULT 10 CHECK(limit_values BETWEEN 1 AND 100),
 created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(project_id,name)
);
CREATE INDEX dashboard_panels_project ON dashboard_panels(workspace_id,project_id);
CREATE TABLE user_totp (
 user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 secret bytea NOT NULL,
 enabled boolean NOT NULL DEFAULT false,
 last_counter bigint NOT NULL DEFAULT -1,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE auth_challenges (
 hash bytea PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 purpose text NOT NULL CHECK(purpose IN ('mfa','passkey_register','passkey_login')),
 data jsonb NOT NULL DEFAULT '{}',
 expires_at timestamptz NOT NULL,
 attempts integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_challenges_expiry ON auth_challenges(expires_at);
CREATE TABLE user_passkeys (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 credential_id bytea NOT NULL UNIQUE,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 100),
 credential jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 last_used_at timestamptz
);
CREATE INDEX user_passkeys_user ON user_passkeys(user_id);
CREATE TABLE system_alert_subscriptions (
 workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
 channel_ids uuid[] NOT NULL DEFAULT '{}',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE system_alert_states (
 name text PRIMARY KEY,
 firing boolean NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE user_recovery_codes (
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 hash bytea NOT NULL,
 PRIMARY KEY(user_id,hash)
);
