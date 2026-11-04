-- Runtime settings (ADR 0018). A row holds one value for the instance (no workspace), a workspace
-- (no project) or a project. Deleting the workspace or the project deletes its rows.
CREATE TABLE settings (
    workspace_id uuid        REFERENCES workspaces (id) ON DELETE CASCADE,
    project_id   uuid        REFERENCES projects (id) ON DELETE CASCADE,
    key          text        NOT NULL,
    value        jsonb       NOT NULL,
    updated_by   uuid        REFERENCES users (id) ON DELETE SET NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (project_id IS NULL OR workspace_id IS NOT NULL)
);

CREATE UNIQUE INDEX settings_scope_key ON settings (key,
    COALESCE(workspace_id, '00000000-0000-0000-0000-000000000000'),
    COALESCE(project_id, '00000000-0000-0000-0000-000000000000'));
