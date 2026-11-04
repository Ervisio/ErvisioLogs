ALTER TABLE api_tokens DROP COLUMN issued_by_operator;
ALTER TABLE audit_events DROP COLUMN actor_name;
DROP TABLE admin_tokens;
