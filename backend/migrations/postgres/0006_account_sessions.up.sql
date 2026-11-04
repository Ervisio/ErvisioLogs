-- Where and when a session was last used, for the "active sessions" lists. last_seen_at is refreshed
-- at most once a minute.
ALTER TABLE sessions ADD COLUMN ip text NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN last_seen_at timestamptz;
