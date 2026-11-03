-- Bounded event queue, batching and retry logic for the Ervisio Logs ingest API.
--
-- Pure Lua: no FiveM globals. Everything environment specific is injected so the
-- module can be unit-tested with a fake clock and a mocked HTTP function.
--
--   local q = Queue.new(opts, deps)
--
-- deps (all functions unless noted):
--   now()                         monotonic clock in milliseconds
--   http(url, method, body, headers, cb)   cb(status, body, headers); status 0 = network error
--   json_encode(v) -> string      json_decode(s) -> table|nil
--   random()                      float in [0, 1)
--   redact                        object from redact.lua (required)
--   token                         string, the project token (kept only inside the request headers)
--   utc_ms()                      optional, epoch milliseconds (default: os.time() * 1000)
--   instance_id                   optional string prefix that makes event ids unique per run
--   get_identifiers(player_id)    optional -> { license = '...', discord = '...', ip = '...' }
--   warn(message)                 optional, console warning sink (never receives the token)
--
-- opts (flat table, see config.lua for meaning): url, environment, service, host,
--   default_stream, streams = {{prefix=, stream=}}, identifiers = {enabled, types, include_ip},
--   max_queue, batch_max_events, batch_max_bytes, flush_interval_ms, http_timeout_ms,
--   max_attempts, backoff_base_ms, backoff_cap_ms, retry_after_cap_ms, max_message_bytes,
--   max_string_bytes, max_event_bytes, max_attr_depth, max_attr_keys, max_attr_nodes,
--   warn_interval_ms.
--
-- Send discipline: one request in flight at a time, batches are sent in order, a failed
-- batch is retried with the same event ids (the server de-duplicates on id).

local Queue = {}
Queue.__index = Queue

local LEVELS = { TRACE = true, DEBUG = true, INFO = true, NOTICE = true, WARN = true, ERROR = true, FATAL = true }
local LEVEL_ALIAS = { WARNING = 'WARN', CRITICAL = 'FATAL' }

local DEFAULTS = {
    url = '',
    environment = '',
    service = 'fivem',
    host = 'fivem',
    default_stream = 'fivem',
    max_queue = 5000,
    batch_max_events = 200,
    batch_max_bytes = 800000,
    flush_interval_ms = 2000,
    http_timeout_ms = 30000,
    max_attempts = 6,
    backoff_base_ms = 1000,
    backoff_cap_ms = 60000,
    retry_after_cap_ms = 300000,
    max_message_bytes = 8192,
    max_string_bytes = 4096,
    max_event_bytes = 60000,
    max_attr_depth = 6,
    max_attr_keys = 100,
    max_attr_nodes = 1000,
    warn_interval_ms = 60000,
}

local function trunc(s, n)
    if #s <= n then return s end
    local i = n
    while i > 0 do
        local b = s:byte(i + 1)
        if b and b >= 0x80 and b < 0xC0 then i = i - 1 else break end
    end
    return s:sub(1, i)
end

local function header_get(headers, name)
    if type(headers) ~= 'table' then return nil end
    for k, v in pairs(headers) do
        if type(k) == 'string' and k:lower() == name then return v end
    end
    return nil
end

local function is_array(t)
    local n = #t
    if n == 0 then return false end
    local c = 0
    for _ in pairs(t) do c = c + 1 end
    return c == n
end

local function sanitize_key(k)
    local s = type(k) == 'string' and k or tostring(k)
    s = s:gsub('[^A-Za-z0-9_%-]', '_')
    if s == '' then s = '_' end
    return trunc(s, 128)
end

local function clamp(v, lo, hi)
    v = tonumber(v) or lo
    if v < lo then return lo end
    if v > hi then return hi end
    return v
end

-- ---------------------------------------------------------------------------
-- construction
-- ---------------------------------------------------------------------------

function Queue.new(opts, deps)
    opts = opts or {}
    deps = deps or {}
    assert(deps.now and deps.http and deps.json_encode and deps.redact, 'ervisio_logs: missing queue dependencies')

    local cfg = {}
    for k, v in pairs(DEFAULTS) do cfg[k] = v end
    for k, v in pairs(opts) do
        if v ~= nil then cfg[k] = v end
    end
    cfg.batch_max_events = clamp(cfg.batch_max_events, 1, 1000)
    cfg.batch_max_bytes = clamp(cfg.batch_max_bytes, 1000, 900000)
    cfg.max_queue = clamp(cfg.max_queue, 1, 1000000)
    cfg.max_attempts = clamp(cfg.max_attempts, 1, 1000)

    local identifiers = { enabled = false, types = { 'license' }, include_ip = false }
    for k, v in pairs(opts.identifiers or {}) do identifiers[k] = v end
    cfg.identifiers = identifiers

    -- Longest prefix first so "inventory.shop." beats "inventory.".
    local streams = {}
    for _, m in ipairs(opts.streams or {}) do
        if type(m.prefix) == 'string' and type(m.stream) == 'string' then
            streams[#streams + 1] = { prefix = m.prefix, stream = m.stream }
        end
    end
    table.sort(streams, function(a, b) return #a.prefix > #b.prefix end)
    cfg.streams = streams

    local self = setmetatable({
        cfg = cfg,
        now = deps.now,
        http = deps.http,
        json_encode = deps.json_encode,
        json_decode = deps.json_decode,
        random = deps.random or math.random,
        redact = deps.redact,
        utc_ms = deps.utc_ms or function() return os.time() * 1000 end,
        get_identifiers = deps.get_identifiers,
        warn_sink = deps.warn,
        instance_id = deps.instance_id or 'run',
        seq = 0,
        gen = 0,
        items = {},
        first = 1,
        last = 0,
        count = 0,
        bytes = 0,
        retry = {},
        inflight = nil,
        next_at = 0,
        force = false,
        disabled = false,
        warned_at = {},
        warned_once = {},
        stats = {
            accepted = 0, rejected = 0, requests = 0, retries = 0,
            dropped_overflow = 0, dropped_retries = 0, dropped_http = 0,
            dropped_auth = 0, dropped_disabled = 0, dropped_invalid = 0,
        },
    }, Queue)

    local base = tostring(cfg.url or ''):gsub('/+$', '')
    self.endpoint = base .. '/api/v1/ingest/batch'
    self.headers = {
        ['Authorization'] = 'Bearer ' .. tostring(deps.token or ''),
        ['Content-Type'] = 'application/json',
    }
    return self
end

-- ---------------------------------------------------------------------------
-- diagnostics
-- ---------------------------------------------------------------------------

function Queue:_warn(key, message)
    if not self.warn_sink then return end
    local now = self.now()
    local last = self.warned_at[key]
    if last and now - last < self.cfg.warn_interval_ms then return end
    self.warned_at[key] = now
    self.warn_sink(message)
end

function Queue:_warn_once(key, message)
    if self.warned_once[key] or not self.warn_sink then return end
    self.warned_once[key] = true
    self.warn_sink(message)
end

function Queue:get_stats()
    local out = {}
    for k, v in pairs(self.stats) do out[k] = v end
    out.queued = self.count
    out.retry_batches = #self.retry
    out.inflight = self.inflight ~= nil
    out.disabled = self.disabled
    return out
end

-- ---------------------------------------------------------------------------
-- event building
-- ---------------------------------------------------------------------------

function Queue:_stream_for(event)
    for i = 1, #self.cfg.streams do
        local m = self.cfg.streams[i]
        if event:sub(1, #m.prefix) == m.prefix then return m.stream end
    end
    return self.cfg.default_stream
end

local function clean(self, v, depth, st)
    local cfg = self.cfg
    local tv = type(v)
    if tv == 'string' then
        return trunc(v, cfg.max_string_bytes)
    elseif tv == 'number' then
        if v ~= v or v == math.huge or v == -math.huge then return tostring(v) end
        return v
    elseif tv == 'boolean' then
        return v
    elseif tv == 'vector3' then
        return { x = v.x, y = v.y, z = v.z }
    elseif tv == 'vector2' then
        return { x = v.x, y = v.y }
    elseif tv == 'vector4' then
        return { x = v.x, y = v.y, z = v.z, w = v.w }
    elseif tv == 'table' then
        if depth > cfg.max_attr_depth then return '[max depth]' end
        st.nodes = st.nodes + 1
        if st.nodes > cfg.max_attr_nodes then return '[truncated]' end

        local out, n = {}, 0
        if is_array(v) then
            for i = 1, #v do
                n = n + 1
                if n > cfg.max_attr_keys then break end
                local c = clean(self, v[i], depth + 1, st)
                if c ~= nil then out[#out + 1] = c end
            end
        else
            for k, val in pairs(v) do
                n = n + 1
                if n > cfg.max_attr_keys then
                    out['_truncated'] = true
                    break
                end
                local key = sanitize_key(k)
                if self.redact.is_sensitive(k) then
                    out[key] = self.redact.mask
                else
                    local c = clean(self, val, depth + 1, st)
                    if c ~= nil then out[key] = c end
                end
            end
        end
        if next(out) == nil then return nil end -- empty tables encode ambiguously: omit
        return out
    elseif tv == 'nil' then
        return nil
    end
    return '<' .. tv .. '>'
end

function Queue:_attributes(raw)
    if type(raw) ~= 'table' then return nil end
    local st = { nodes = 0 }
    if is_array(raw) then
        local c = clean(self, raw, 1, st)
        if c == nil then return nil end
        return { values = c }
    end
    local out = clean(self, raw, 1, st)
    if type(out) ~= 'table' then return nil end
    return out
end

function Queue:_identifiers(raw)
    local idcfg = self.cfg.identifiers
    if not idcfg.enabled or not self.get_identifiers or type(raw) ~= 'table' then return nil end
    local pid = tonumber(raw.player_id)
    if not pid then return nil end
    local ok, ids = pcall(self.get_identifiers, pid)
    if not ok or type(ids) ~= 'table' then return nil end
    local out
    for _, t in ipairs(idcfg.types or {}) do
        if t ~= 'ip' or idcfg.include_ip then
            local v = ids[t]
            if v ~= nil then
                out = out or {}
                out[t] = trunc(tostring(v), 256)
            end
        end
    end
    return out
end

function Queue:_build(spec)
    local cfg = self.cfg
    local event = trunc(tostring(spec.event or 'unknown'), 128)

    local stream = spec.stream
    if type(stream) ~= 'string' or stream == '' then stream = self:_stream_for(event) end

    local level = type(spec.level) == 'string' and spec.level:upper() or 'INFO'
    level = LEVEL_ALIAS[level] or level
    if not LEVELS[level] then level = 'INFO' end

    local message = spec.message
    if message ~= nil then
        if type(message) ~= 'string' then message = tostring(message) end
        message = self.redact.scrub_text(trunc(message, cfg.max_message_bytes))
    end

    local attrs = self:_attributes(spec.attributes)
    local ids = self:_identifiers(spec.attributes)
    if ids then
        attrs = attrs or {}
        attrs.identifiers = ids
    end

    local ms = math.floor(self.utc_ms())
    local sec = ms // 1000
    local timestamp = os.date('!%Y-%m-%dT%H:%M:%S', sec) .. string.format('.%03dZ', ms % 1000)

    self.seq = self.seq + 1
    local ev = {
        id = self.instance_id .. '-' .. self.seq,
        stream = trunc(stream, 128),
        timestamp = timestamp,
        level = level,
        event = event,
        message = message,
        service = cfg.service,
        host = cfg.host,
        resource = type(spec.resource) == 'string' and trunc(spec.resource, 256) or nil,
        attributes = attrs,
    }

    local encoded = self.json_encode(ev)
    if #encoded > cfg.max_event_bytes then
        ev.attributes = { _truncated = true, _original_bytes = #encoded }
        if ev.message then ev.message = trunc(ev.message, 1024) end
        encoded = self.json_encode(ev)
    end
    return { s = encoded, n = #encoded, t = self.now() }
end

-- ---------------------------------------------------------------------------
-- queue
-- ---------------------------------------------------------------------------

function Queue:_clear()
    self.items, self.first, self.last, self.count, self.bytes = {}, 1, 0, 0, 0
    self.retry = {}
end

function Queue:_push(entry)
    if self.count >= self.cfg.max_queue then
        local old = self.items[self.first]
        self.items[self.first] = nil
        self.first = self.first + 1
        self.count = self.count - 1
        self.bytes = self.bytes - old.n - 1
        self.stats.dropped_overflow = self.stats.dropped_overflow + 1
        self:_warn('overflow', 'ervisio_logs: queue is full, dropping the oldest events (the ingest API is slow or unreachable)')
    end
    self.last = self.last + 1
    self.items[self.last] = entry
    self.count = self.count + 1
    self.bytes = self.bytes + entry.n + 1
end

-- spec: { stream?, level?, event, message?, attributes?, resource? }. Never raises.
function Queue:log(spec)
    if self.disabled then
        self.stats.dropped_disabled = self.stats.dropped_disabled + 1
        return false
    end
    if type(spec) ~= 'table' then
        self.stats.dropped_invalid = self.stats.dropped_invalid + 1
        return false
    end
    local ok, entry = pcall(self._build, self, spec)
    if not ok or not entry then
        self.stats.dropped_invalid = self.stats.dropped_invalid + 1
        return false
    end
    self:_push(entry)
    return true
end

function Queue:log_batch(events, resource)
    if type(events) ~= 'table' then return 0 end
    local accepted = 0
    for i = 1, math.min(#events, 1000) do
        local e = events[i]
        if type(e) == 'table' then
            if self:log({
                stream = e.stream, level = e.level, event = e.event, message = e.message,
                attributes = e.attributes, resource = resource,
            }) then
                accepted = accepted + 1
            end
        else
            self.stats.dropped_invalid = self.stats.dropped_invalid + 1
        end
    end
    return accepted
end

function Queue:_take_batch()
    local cfg = self.cfg
    local entries, bytes = {}, 0
    while self.count > 0 and #entries < cfg.batch_max_events do
        local e = self.items[self.first]
        if #entries > 0 and bytes + e.n + 1 > cfg.batch_max_bytes then break end
        self.items[self.first] = nil
        self.first = self.first + 1
        self.count = self.count - 1
        self.bytes = self.bytes - e.n - 1
        entries[#entries + 1] = e
        bytes = bytes + e.n + 1
    end
    if self.count == 0 then self.first, self.last = 1, 0 end
    return { entries = entries, bytes = bytes, attempts = 0, fails = 0 }
end

function Queue:_body(batch)
    local parts = {}
    for i = 1, #batch.entries do parts[i] = batch.entries[i].s end
    local head = '{'
    local env = self.cfg.environment
    if type(env) == 'string' and env ~= '' then
        head = head .. '"environment":' .. self.json_encode(env) .. ','
    end
    return head .. '"events":[' .. table.concat(parts, ',') .. ']}'
end

-- ---------------------------------------------------------------------------
-- sending
-- ---------------------------------------------------------------------------

function Queue:_send(batch, now)
    local f = { batch = batch, started = now }
    self.inflight = f
    batch.attempts = batch.attempts + 1
    self.stats.requests = self.stats.requests + 1
    local ok = pcall(self.http, self.endpoint, 'POST', self:_body(batch), self.headers,
        function(status, body, headers)
            self:_on_response(f, status, body, headers)
        end)
    if not ok and self.inflight == f then
        self.inflight = nil
        self:_on_transient(batch, 0, self.now())
    end
end

-- Call this from a short timer (FiveM glue uses ~250 ms). Cheap when idle.
function Queue:tick()
    if self.disabled then return end
    local now = self.now()
    local f = self.inflight
    if f then
        if now - f.started >= self.cfg.http_timeout_ms then
            self.inflight = nil -- a late callback for f is ignored
            self:_on_transient(f.batch, 0, now)
        end
        return
    end
    if now < self.next_at then return end

    local batch = table.remove(self.retry, 1)
    if not batch then
        if self.count == 0 then
            self.force = false
            return
        end
        local oldest = self.items[self.first]
        local due = self.force
            or self.count >= self.cfg.batch_max_events
            or self.bytes >= self.cfg.batch_max_bytes
            or now - oldest.t >= self.cfg.flush_interval_ms
        if not due then return end
        batch = self:_take_batch()
    end
    self:_send(batch, now)
end

-- Send everything as soon as possible (ignores the flush interval, not a backoff).
function Queue:flush()
    self.force = true
    self:tick()
end

function Queue:_on_response(f, status, body, headers)
    if self.inflight ~= f then return end
    self.inflight = nil
    local now = self.now()
    status = tonumber(status) or 0
    local batch = f.batch

    if status == 200 or status == 202 then
        self:_on_success(batch, body)
    elseif status == 401 or status == 403 then
        self:_on_auth_failure(status)
    elseif status == 413 then
        self:_on_too_large(batch, now)
    elseif status == 429 or status == 503 then
        self:_on_throttled(batch, status, headers, now)
    elseif status == 0 or status == 408 or status >= 500 then
        self:_on_transient(batch, status, now)
    else
        -- 400, 404, 415, 422, ...: the same request would fail again.
        self.stats.dropped_http = self.stats.dropped_http + #batch.entries
        self:_warn('http' .. status, string.format(
            'ervisio_logs: dropped a batch of %d event(s), the ingest API answered HTTP %d (not retried)',
            #batch.entries, status))
    end
end

function Queue:_on_success(batch, body)
    local accepted, rejected = #batch.entries, 0
    local res
    if self.json_decode and body ~= nil and body ~= '' then
        local ok, decoded = pcall(self.json_decode, body)
        if ok and type(decoded) == 'table' then res = decoded end
    end
    if res then
        accepted = tonumber(res.accepted) or accepted
        rejected = tonumber(res.rejected) or 0
        if rejected > 0 then
            local first = type(res.errors) == 'table' and res.errors[1] or nil
            local reason = type(first) == 'table' and tostring(first.reason) or 'unknown reason'
            self:_warn('rejected', string.format(
                'ervisio_logs: the ingest API rejected %d event(s), e.g. "%s" (rejected events are not retried)',
                rejected, reason))
        end
    end
    self.stats.accepted = self.stats.accepted + accepted
    self.stats.rejected = self.stats.rejected + rejected
    self.next_at = 0
    self:tick() -- keep draining a backlog without waiting for the next timer tick
end

function Queue:_on_auth_failure(status)
    local dropped = self.count
    for i = 1, #self.retry do dropped = dropped + #self.retry[i].entries end
    self.stats.dropped_auth = self.stats.dropped_auth + dropped
    self:_clear()
    self.disabled = true
    self:_warn_once('auth', string.format(
        'ervisio_logs: the ingest API refused the project token (HTTP %d). Log sending is STOPPED. '
        .. 'Check the ervisio_logs_token convar (the token needs the logs:ingest scope), '
        .. 'then run "ervisio_logs_resume" or restart the resource.', status))
end

function Queue:_on_too_large(batch, now)
    local n = #batch.entries
    if n <= 1 then
        self.stats.dropped_http = self.stats.dropped_http + n
        self:_warn('413', 'ervisio_logs: dropped an event that is larger than the server body limit (HTTP 413)')
        return
    end
    local mid = n // 2
    local a = { entries = {}, bytes = 0, attempts = 0, fails = 0 }
    local b = { entries = {}, bytes = 0, attempts = 0, fails = 0 }
    for i = 1, n do
        local target = i <= mid and a or b
        target.entries[#target.entries + 1] = batch.entries[i]
        target.bytes = target.bytes + batch.entries[i].n + 1
    end
    table.insert(self.retry, 1, b)
    table.insert(self.retry, 1, a)
    self.next_at = now
    self:tick()
end

function Queue:_backoff(fails)
    local cfg = self.cfg
    local d = math.min(cfg.backoff_cap_ms, cfg.backoff_base_ms * 2 ^ (fails - 1))
    -- "equal jitter": between 50% and 100% of the exponential delay
    return math.floor(d * (0.5 + 0.5 * self.random()))
end

function Queue:_requeue(batch, now, delay_ms)
    table.insert(self.retry, 1, batch)
    self.next_at = now + delay_ms
    self.stats.retries = self.stats.retries + 1
end

function Queue:_on_throttled(batch, status, headers, now)
    batch.fails = batch.fails + 1 -- throttling does not consume `attempts`
    local delay
    local seconds = tonumber(header_get(headers, 'retry-after'))
    if seconds and seconds >= 0 then
        local ms = math.min(seconds * 1000, self.cfg.retry_after_cap_ms)
        delay = math.floor(ms + self.random() * math.min(1000, ms * 0.1))
    else
        delay = self:_backoff(batch.fails)
    end
    self:_warn('throttle' .. status, string.format(
        'ervisio_logs: the ingest API asked to slow down (HTTP %d), retrying in %.1f s', status, delay / 1000))
    self:_requeue(batch, now, delay)
end

function Queue:_on_transient(batch, status, now)
    batch.fails = batch.fails + 1
    if batch.attempts >= self.cfg.max_attempts then
        self.stats.dropped_retries = self.stats.dropped_retries + #batch.entries
        self:_warn('giveup', string.format(
            'ervisio_logs: dropped a batch of %d event(s) after %d failed attempts (last status %d)',
            #batch.entries, batch.attempts, status))
        return
    end
    self:_warn('transient', string.format(
        'ervisio_logs: ingest request failed (status %d), will retry', status))
    self:_requeue(batch, now, self:_backoff(batch.fails))
end

-- Re-enable sending after a 401/403 (optionally with a new token).
function Queue:resume(token)
    if token ~= nil then
        self.headers = {
            ['Authorization'] = 'Bearer ' .. tostring(token),
            ['Content-Type'] = 'application/json',
        }
    end
    self.disabled = false
    self.warned_once.auth = nil
    self.next_at = 0
end

-- Stop accepting events (e.g. missing configuration).
function Queue:disable()
    self.disabled = true
end

ErvisioQueue = Queue
return Queue
