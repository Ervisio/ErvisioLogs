-- P5 integrations (ADR 0010): saved views, notification channels, alert rules and their history.

-- A saved view is a search filter with a relative time range, shared with every member of the
-- project's workspace who can read logs.
CREATE TABLE saved_queries (
    id           uuid PRIMARY KEY,
    workspace_id uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    project_id   uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name         text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    query        jsonb       NOT NULL,
    created_by   uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);
CREATE INDEX saved_queries_project ON saved_queries (workspace_id, project_id);

-- A webhook target. The signing key is derived from the server secret and the channel id plus
-- secret_version (rotation bumps it); it is never stored. The URL may itself carry a token
-- (chat webhooks do), so the API never returns it in full.
CREATE TABLE notification_channels (
    id             uuid PRIMARY KEY,
    workspace_id   uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name           text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    type           text        NOT NULL CHECK (type IN ('webhook')),
    -- ervisio: signed JSON document; slack and discord: their incoming-webhook body shape.
    format         text        NOT NULL DEFAULT 'ervisio' CHECK (format IN ('ervisio', 'slack', 'discord')),
    url            text        NOT NULL CHECK (length(url) BETWEEN 8 AND 2048),
    secret_version integer     NOT NULL DEFAULT 1,
    enabled        boolean     NOT NULL DEFAULT true,
    last_status    text        CHECK (last_status IS NULL OR last_status IN ('ok', 'error')),
    last_error     text,
    last_sent_at   timestamptz,
    created_by     uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

-- Threshold rule: fires when the events matching query in the last window_seconds exceed
-- threshold, resolves when they fall back to or below it.
CREATE TABLE alert_rules (
    id                uuid PRIMARY KEY,
    workspace_id      uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    project_id        uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name              text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    query             jsonb       NOT NULL,
    window_seconds    integer     NOT NULL CHECK (window_seconds BETWEEN 60 AND 86400),
    threshold         bigint      NOT NULL CHECK (threshold >= 0),
    interval_seconds  integer     NOT NULL DEFAULT 60 CHECK (interval_seconds BETWEEN 30 AND 86400),
    channel_ids       uuid[]      NOT NULL DEFAULT '{}',
    enabled           boolean     NOT NULL DEFAULT true,
    state             text        NOT NULL DEFAULT 'ok' CHECK (state IN ('ok', 'firing', 'error')),
    last_value        bigint,
    last_error        text,
    last_evaluated_at timestamptz,
    next_eval_at      timestamptz NOT NULL DEFAULT now(),
    created_by        uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);
CREATE INDEX alert_rules_due ON alert_rules (next_eval_at) WHERE enabled;

-- State changes of a rule, newest first in the UI. Pruned by age by the evaluator.
CREATE TABLE alert_events (
    id           uuid PRIMARY KEY,
    workspace_id uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    rule_id      uuid        NOT NULL REFERENCES alert_rules (id) ON DELETE CASCADE,
    status       text        NOT NULL CHECK (status IN ('firing', 'resolved', 'error')),
    value        bigint,
    detail       text,
    deliveries   jsonb       NOT NULL DEFAULT '[]'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alert_events_rule ON alert_events (rule_id, created_at DESC);
