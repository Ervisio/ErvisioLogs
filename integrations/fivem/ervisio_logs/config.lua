-- Central configuration for ervisio_logs. Server side only (loaded as a server script).
--
-- Do NOT put the project token in this file. It comes from the server convar
--   set ervisio_logs_token "evl_..."        (server.cfg; use `set`, never `setr` or `sets`)
-- The API URL and environment can also be set from convars, which win over this file:
--   set ervisio_logs_url "https://logs.example.com"
--   set ervisio_logs_environment "production"
--   set ervisio_logs_host "my-fivem-eu-1"   (optional, defaults to sv_hostname)

ErvisioConfig = {
    url = '',                      -- fallback only; prefer the ervisio_logs_url convar
    environment = 'production',    -- fallback only; prefer the ervisio_logs_environment convar
    service = 'fivem',             -- InEvent.service
    default_stream = 'fivem',      -- stream used when no explicit stream and no prefix matches

    -- Stream per event prefix (longest prefix wins). An explicit `stream` argument to
    -- Log() always wins over this table.
    streams = {
        { prefix = 'player.',     stream = 'players' },
        { prefix = 'inventory.',  stream = 'inventory' },
        { prefix = 'money.',      stream = 'economy' },
        { prefix = 'economy.',    stream = 'economy' },
        { prefix = 'job.',        stream = 'jobs' },
        { prefix = 'moderation.', stream = 'moderation' },
    },

    features = {
        exports = true,             -- allow other resources to call the Log / LogBatch exports
        startup_event = true,       -- send one `ervisio_logs.started` event when the resource starts
        player_connections = false, -- log playerJoining / playerDropped (with the drop reason)
    },

    privacy = {
        player_name = false,        -- include GetPlayerName() in the join/leave events
        identifiers = {
            enabled = false,        -- master switch: no identifier is read or sent while false
            -- Which identifier types may be attached (as attributes.identifiers) to events
            -- that carry attributes.player_id. Values are what GetPlayerIdentifiers() reports.
            types = { 'license', 'discord', 'steam' },
            -- 'ip' is sent only if BOTH `include_ip = true` AND 'ip' is listed in `types`.
            include_ip = false,
        },
    },

    -- Sensitive-data redaction. A key is redacted when its lowercased, alphanumeric-only
    -- form CONTAINS one of these entries ("api_token" and "cardNumber" both match).
    -- Over-redaction is intended; use `allow` for exact keys that must stay visible.
    redact = {
        keys = {
            'password', 'passwd', 'token', 'secret', 'card', 'cvv', 'iban',
            'authorization', 'apikey', 'privatekey',
        },
        allow = {},                 -- exact keys (normalized the same way) never redacted
        mask = '[REDACTED]',
        scrub_message = true,       -- also mask "password=xyz" style fragments inside message text
    },

    -- Queue, batching and retry. All durations in milliseconds.
    queue = {
        max_queue = 5000,           -- events kept in memory; when full the OLDEST is dropped
        batch_max_events = 200,     -- API accepts up to 1000
        batch_max_bytes = 800000,   -- request body budget; the server limit is 1 MiB
        flush_interval_ms = 2000,   -- send a partial batch after this long
        http_timeout_ms = 30000,    -- give up waiting for a response (counts as a failure)
        max_attempts = 6,           -- transient failures (network error / 5xx) before dropping a batch
        backoff_base_ms = 1000,     -- exponential backoff: base * 2^(n-1), capped, with jitter
        backoff_cap_ms = 60000,
        retry_after_cap_ms = 300000, -- upper bound honoured for a Retry-After header
        tick_ms = 250,              -- how often the sender loop wakes up
    },

    -- Per-event size limits (the API allows 32 KiB messages and 64 KiB attributes).
    limits = {
        max_message_bytes = 8192,
        max_string_bytes = 4096,    -- any single string attribute value
        max_event_bytes = 60000,    -- encoded event; bigger ones lose their attributes
        max_attr_depth = 6,         -- API limit is 8 levels
        max_attr_keys = 100,        -- per object / array
        max_attr_nodes = 1000,      -- per event
    },
}
