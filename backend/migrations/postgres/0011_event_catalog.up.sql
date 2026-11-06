CREATE TABLE event_types (
 project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
 title text NOT NULL DEFAULT '', template text NOT NULL DEFAULT '',
 icon text NOT NULL DEFAULT '', category text NOT NULL DEFAULT '', color text NOT NULL DEFAULT '',
 featured_fields text[] NOT NULL DEFAULT '{}', discovered boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(project_id,name)
);
CREATE TABLE project_field_rules (
 project_id uuid PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
 rules jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(rules) = 'array')
);
