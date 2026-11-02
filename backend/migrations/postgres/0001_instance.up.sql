-- Instance-level settings. Tenant/auth tables arrive with P1.
CREATE TABLE instance_settings (
    key        text        PRIMARY KEY,
    value      text        NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Stable identifier of this installation (used later by the Ervisio adapter and diagnostics).
INSERT INTO instance_settings (key, value) VALUES ('instance_id', gen_random_uuid()::text);
