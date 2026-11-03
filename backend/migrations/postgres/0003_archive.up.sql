-- P4 archive catalog (ADR 0008). Remote provider settings, retention policies and the segment
-- catalog. Credentials are never stored here: secret_file names a mounted file the app reads.

CREATE TABLE storage_providers (
    id              uuid PRIMARY KEY,
    name            text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    type            text        NOT NULL CHECK (type IN ('s3', 'sftp', 'local')),
    config          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    secret_file     text        CHECK (secret_file IS NULL OR secret_file ~ '^[A-Za-z0-9._-]{1,100}$'),
    active          boolean     NOT NULL DEFAULT false,
    status          text        NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'ok', 'error')),
    last_error      text,
    last_checked_at timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
-- The spec allows one selected archive provider per instance.
CREATE UNIQUE INDEX storage_providers_one_active ON storage_providers ((true)) WHERE active;

-- One row per scope. NULL fields inherit from the next wider scope; the instance row has every
-- field set. Cache and watermark fields only apply to the instance row.
CREATE TABLE retention_policies (
    scope_type       text        NOT NULL CHECK (scope_type IN ('instance', 'workspace', 'project', 'stream')),
    scope_id         uuid        NOT NULL,
    local_age_hours  integer     CHECK (local_age_hours > 0),
    global_age_hours integer     CHECK (global_age_hours > 0),
    cache_max_bytes  bigint      CHECK (cache_max_bytes > 0),
    high_pct         smallint    CHECK (high_pct BETWEEN 1 AND 100),
    low_pct          smallint    CHECK (low_pct BETWEEN 1 AND 100),
    updated_by       uuid        REFERENCES users (id) ON DELETE SET NULL,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (scope_type, scope_id),
    CHECK (local_age_hours IS NULL OR global_age_hours IS NULL OR global_age_hours >= local_age_hours),
    CHECK (high_pct IS NULL OR low_pct IS NULL OR low_pct < high_pct),
    CHECK (scope_type = 'instance' OR (cache_max_bytes IS NULL AND high_pct IS NULL AND low_pct IS NULL)),
    CHECK (scope_type <> 'instance' OR scope_id = '00000000-0000-0000-0000-000000000000')
);

-- Segment catalog. Rows are never deleted: a DELETED row is the tombstone that stops a retry from
-- archiving an expired day again. No foreign keys to tenant tables on purpose: deleting a
-- project must not drop the rows that still point at remote objects.
CREATE TABLE archive_segments (
    id              uuid PRIMARY KEY,
    workspace_id    uuid        NOT NULL,
    project_id      uuid        NOT NULL,
    stream_id       uuid        NOT NULL,
    day             date        NOT NULL,
    generation      integer     NOT NULL CHECK (generation >= 1),
    provider_id     uuid        NOT NULL REFERENCES storage_providers (id),
    object_key      text        NOT NULL,
    state           text        NOT NULL CHECK (state IN ('STAGING', 'UPLOADING', 'VERIFIED', 'CATALOGED',
                        'HOT_EVICTABLE', 'REMOTE_EXPIRED', 'DELETING', 'DELETED', 'FAILED')),
    cutoff_from     timestamptz NOT NULL,
    cutoff_to       timestamptz NOT NULL,
    min_event_time  timestamptz,
    max_event_time  timestamptz,
    checksum_sha256 bytea       CHECK (checksum_sha256 IS NULL OR length(checksum_sha256) = 32),
    bytes           bigint      CHECK (bytes >= 0),
    row_count       bigint      CHECK (row_count >= 0),
    schema_version  integer     NOT NULL,
    attempts        integer     NOT NULL DEFAULT 0,
    next_attempt_at timestamptz,
    last_error      text,
    cataloged_at    timestamptz,
    hot_evicted_at  timestamptz,
    deleted_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_id, stream_id, day, generation),
    UNIQUE (provider_id, object_key),
    CHECK (cutoff_from < cutoff_to)
);
CREATE INDEX archive_segments_read_idx ON archive_segments (workspace_id, project_id, day)
    WHERE state IN ('CATALOGED', 'HOT_EVICTABLE');
CREATE INDEX archive_segments_work_idx ON archive_segments (state, next_attempt_at)
    WHERE state NOT IN ('CATALOGED', 'HOT_EVICTABLE', 'DELETED', 'FAILED');

-- Instance defaults from STORAGE_AND_RETENTION.md: 3 days local, 14 days global, 20 GB cache,
-- watermarks 90/75. Admins change them in the storage settings.
INSERT INTO retention_policies (scope_type, scope_id, local_age_hours, global_age_hours, cache_max_bytes, high_pct, low_pct)
VALUES ('instance', '00000000-0000-0000-0000-000000000000', 72, 336, 21474836480, 90, 75);
