-- Manual purges of a project's logs (ADR 0020). A purge deletes hot rows and archived segments, which
-- can take long, so the request only queues a job and a worker runs it. stream_id and environment_id
-- have no foreign key: a stream deleted later must not erase the record of what was purged.
CREATE TABLE purge_jobs (
    id               uuid PRIMARY KEY,
    workspace_id     uuid        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    project_id       uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    stream_id        uuid,
    environment_id   uuid,
    time_from        timestamptz,
    time_to          timestamptz,
    state            text        NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'done', 'failed')),
    rows_deleted     bigint      NOT NULL DEFAULT 0,
    segments_removed bigint      NOT NULL DEFAULT 0,
    error            text,
    created_by       uuid        REFERENCES users (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    started_at       timestamptz,
    finished_at      timestamptz
);
CREATE INDEX purge_jobs_project ON purge_jobs (project_id, created_at DESC);
CREATE INDEX purge_jobs_open ON purge_jobs (created_at) WHERE state IN ('queued', 'running');
