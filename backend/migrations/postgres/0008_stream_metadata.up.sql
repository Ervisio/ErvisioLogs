-- Display metadata for the UI; name stays the immutable identifier used by ingest and search.
ALTER TABLE environments
    ADD COLUMN display_name text NOT NULL DEFAULT '' CHECK (char_length(display_name) <= 100),
    ADD COLUMN description  text NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    ADD COLUMN color        text NOT NULL DEFAULT '' CHECK (color = '' OR color ~ '^#[0-9a-f]{6}$');

ALTER TABLE streams
    ADD COLUMN display_name text NOT NULL DEFAULT '' CHECK (char_length(display_name) <= 100),
    ADD COLUMN description  text NOT NULL DEFAULT '' CHECK (char_length(description) <= 500),
    ADD COLUMN color        text NOT NULL DEFAULT '' CHECK (color = '' OR color ~ '^#[0-9a-f]{6}$');
