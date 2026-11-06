-- busted-style specs for ervisio_logs/server/queue.lua and redact.lua.
--
-- Run from integrations/fivem with:  busted tests/queue_spec.lua
-- These specs were WRITTEN BUT NOT EXECUTED by their author. Expect to fix small
-- mistakes the first time they run.
--
-- The queue is driven with a fake clock, a mocked http function and a tiny JSON
-- encoder (keys sorted, no spaces, empty table -> []), so no FiveM runtime is needed.

local Redact = dofile('ervisio_logs/server/redact.lua')
local Queue = dofile('ervisio_logs/server/queue.lua')

-- ---------------------------------------------------------------------------
-- helpers
-- ---------------------------------------------------------------------------

local function encode(v)
    local t = type(v)
    if t == 'string' then
        local s = v:gsub('[%c"\\]', function(c)
            if c == '"' then return '\\"' end
            if c == '\\' then return '\\\\' end
            return string.format('\\u%04x', c:byte())
        end)
        return '"' .. s .. '"'
    elseif t == 'number' then
        if math.type(v) == 'integer' then return string.format('%d', v) end
        return string.format('%.14g', v)
    elseif t == 'boolean' then
        return tostring(v)
    elseif t == 'table' then
        if next(v) == nil then return '[]' end
        if #v > 0 then
            local out = {}
            for i = 1, #v do out[i] = encode(v[i]) end
            return '[' .. table.concat(out, ',') .. ']'
        end
        local keys = {}
        for k in pairs(v) do keys[#keys + 1] = k end
        table.sort(keys)
        local out = {}
        for _, k in ipairs(keys) do out[#out + 1] = encode(k) .. ':' .. encode(v[k]) end
        return '{' .. table.concat(out, ',') .. '}'
    end
    return 'null'
end

-- Response bodies in these specs are already Lua tables, so "decoding" is the identity.
local function decode(s)
    if type(s) == 'table' then return s end
    return nil
end

local function count_events(body)
    local n = 0
    for _ in body:gmatch('"id":"') do n = n + 1 end
    return n
end

local TOKEN = 'evl_test_token_value'

-- Builds a queue wired to a fake clock and a scripted http mock.
-- env.responses: list consumed in order; when empty, a 202 is returned.
local function new_env(opts, extra_deps)
    local env = { t = 0, calls = {}, responses = {}, warnings = {}, rand = 0, ids = {} }

    env.http = function(url, method, body, headers, cb)
        env.calls[#env.calls + 1] = { url = url, method = method, body = body, headers = headers }
        local r = table.remove(env.responses, 1)
        if not r then
            r = { status = 202, body = { accepted = count_events(body), rejected = 0, errors = {} } }
        end
        cb(r.status, r.body, r.headers)
    end

    local deps = {
        now = function() return env.t end,
        http = env.http,
        json_encode = encode,
        json_decode = decode,
        random = function() return env.rand end,
        redact = Redact.new({}),
        token = TOKEN,
        utc_ms = function() return 1700000000000 + env.t end,
        instance_id = 'test',
        warn = function(m) env.warnings[#env.warnings + 1] = m end,
    }
    for k, v in pairs(extra_deps or {}) do deps[k] = v end

    local o = {
        url = 'https://logs.example.com/',
        environment = 'test',
        flush_interval_ms = 2000,
        batch_max_events = 200,
        max_attempts = 3,
        streams = { { prefix = 'inventory.', stream = 'inventory' }, { prefix = 'inventory.shop.', stream = 'shops' } },
    }
    for k, v in pairs(opts or {}) do o[k] = v end

    env.q = Queue.new(o, deps)
    return env
end

local function log_n(q, n, prefix)
    for i = 1, n do
        q:log({ level = 'INFO', event = 'test.event', message = (prefix or 'm') .. i })
    end
end

local function advance(env, ms)
    env.t = env.t + ms
    env.q:tick()
end

-- ---------------------------------------------------------------------------
-- batching
-- ---------------------------------------------------------------------------

describe('batching', function()
    it('does not send a partial batch before the flush interval', function()
        local env = new_env()
        log_n(env.q, 1)
        env.q:tick()
        assert.equals(0, #env.calls)
        advance(env, 1999)
        assert.equals(0, #env.calls)
        advance(env, 1)
        assert.equals(1, #env.calls)
    end)

    it('sends full batches immediately and keeps the remainder for the interval', function()
        local env = new_env({ batch_max_events = 200 })
        log_n(env.q, 450)
        env.q:tick()
        assert.equals(2, #env.calls)
        assert.equals(200, count_events(env.calls[1].body))
        assert.equals(200, count_events(env.calls[2].body))
        assert.equals(50, env.q:get_stats().queued)
        advance(env, 2000)
        assert.equals(3, #env.calls)
        assert.equals(50, count_events(env.calls[3].body))
    end)

    it('splits by byte budget', function()
        local env = new_env({ batch_max_events = 1000, batch_max_bytes = 1500 })
        for _ = 1, 20 do
            env.q:log({ event = 'big', message = string.rep('x', 300) })
        end
        env.q:tick()
        assert.is_true(#env.calls >= 4)
        for _, c in ipairs(env.calls) do
            assert.is_true(#c.body < 1500 + 200) -- budget for events + the small envelope
        end
    end)

    it('builds the request as documented', function()
        local env = new_env()
        env.q:log({ event = 'a.b', message = 'hello', level = 'info' })
        advance(env, 2000)
        local c = env.calls[1]
        assert.equals('https://logs.example.com/api/v1/ingest/batch', c.url)
        assert.equals('POST', c.method)
        assert.equals('Bearer ' .. TOKEN, c.headers['Authorization'])
        assert.equals('application/json', c.headers['Content-Type'])
        assert.truthy(c.body:find('"environment":"test"', 1, true))
        assert.truthy(c.body:find('"level":"INFO"', 1, true))
        assert.truthy(c.body:find('"id":"test-1"', 1, true))
    end)

    it('uses an RFC 3339 UTC timestamp and a unique id per event', function()
        local env = new_env()
        log_n(env.q, 2)
        env.q:flush()
        local body = env.calls[1].body
        local ts = body:match('"timestamp":"([^"]+)"')
        assert.truthy(ts:match('^%d%d%d%d%-%d%d%-%d%dT%d%d:%d%d:%d%d%.%d%d%dZ$'))
        assert.truthy(body:find('"id":"test-1"', 1, true))
        assert.truthy(body:find('"id":"test-2"', 1, true))
    end)

    it('maps streams by longest event prefix and lets an explicit stream win', function()
        local env = new_env()
        env.q:log({ event = 'inventory.add', message = 'a' })
        env.q:log({ event = 'inventory.shop.buy', message = 'b' })
        env.q:log({ event = 'other', message = 'c' })
        env.q:log({ event = 'inventory.add', stream = 'custom', message = 'd' })
        env.q:flush()
        local body = env.calls[1].body
        assert.truthy(body:find('"stream":"inventory"', 1, true))
        assert.truthy(body:find('"stream":"shops"', 1, true))
        assert.truthy(body:find('"stream":"fivem"', 1, true))
        assert.truthy(body:find('"stream":"custom"', 1, true))
    end)

    it('normalizes levels', function()
        local env = new_env()
        env.q:log({ event = 'a', level = 'warning' })
        env.q:log({ event = 'b', level = 'critical' })
        env.q:log({ event = 'c', level = 'nonsense' })
        env.q:log({ event = 'd' })
        env.q:flush()
        local body = env.calls[1].body
        assert.truthy(body:find('"level":"WARN"', 1, true))
        assert.truthy(body:find('"level":"FATAL"', 1, true))
        assert.is_nil(body:find('nonsense', 1, true))
    end)

    it('omits empty attributes and sanitizes attribute keys', function()
        local env = new_env()
        env.q:log({ event = 'a', attributes = {} })
        env.q:log({ event = 'b', attributes = { empty = {}, nested = { also_empty = {} } } })
        env.q:log({ event = 'c', attributes = { ['a.b'] = 1, ['x y'] = 'z' } })
        env.q:flush()
        local body = env.calls[1].body
        -- exactly one event carries attributes
        local _, n = body:gsub('"attributes":', '')
        assert.equals(1, n)
        assert.truthy(body:find('"a_b":1', 1, true))
        assert.truthy(body:find('"x_y":"z"', 1, true))
    end)
end)

-- ---------------------------------------------------------------------------
-- retry behavior
-- ---------------------------------------------------------------------------

describe('retry', function()
    it('counts rejected events from a 202 and does not retry them', function()
        local env = new_env()
        env.responses = { { status = 202, body = { accepted = 0, rejected = 1, errors = { { index = 0, reason = 'bad level' } } } } }
        log_n(env.q, 1)
        advance(env, 2000)
        advance(env, 120000)
        assert.equals(1, #env.calls)
        assert.equals(1, env.q:get_stats().rejected)
        assert.equals(1, #env.warnings)
    end)

    it('honors Retry-After on 429 and resends the same ids', function()
        local env = new_env()
        env.responses = { { status = 429, headers = { ['Retry-After'] = '7' } } }
        log_n(env.q, 2)
        advance(env, 2000)                 -- first attempt at t=2000, answered 429
        assert.equals(1, #env.calls)
        advance(env, 6999)                 -- t=8999 (< 2000 + 7000)
        assert.equals(1, #env.calls)
        advance(env, 1)                    -- t=9000
        assert.equals(2, #env.calls)
        assert.equals(env.calls[1].body, env.calls[2].body)
        assert.equals(2, env.q:get_stats().accepted)
    end)

    it('reads Retry-After case-insensitively', function()
        local env = new_env()
        env.responses = { { status = 429, headers = { ['retry-after'] = '3' } } }
        log_n(env.q, 1)
        advance(env, 2000)
        advance(env, 2999)
        assert.equals(1, #env.calls)
        advance(env, 1)
        assert.equals(2, #env.calls)
    end)

    it('retries a 503 with exponential backoff (random()=1 gives the full delay)', function()
        local env = new_env()
        env.rand = 1
        env.responses = { { status = 503 }, { status = 503 } }
        log_n(env.q, 1)
        advance(env, 2000)                 -- fail 1 -> wait 1000 ms
        assert.equals(1, #env.calls)
        advance(env, 999)
        assert.equals(1, #env.calls)
        advance(env, 1)                    -- t=3000, fail 2 -> wait 2000 ms
        assert.equals(2, #env.calls)
        advance(env, 1999)
        assert.equals(2, #env.calls)
        advance(env, 1)                    -- t=5000, answered 202
        assert.equals(3, #env.calls)
        assert.equals(1, env.q:get_stats().accepted)
    end)

    it('applies jitter (random()=0 halves the delay)', function()
        local env = new_env()
        env.rand = 0
        env.responses = { { status = 503 } }
        log_n(env.q, 1)
        advance(env, 2000)
        advance(env, 499)
        assert.equals(1, #env.calls)
        advance(env, 1)
        assert.equals(2, #env.calls)
    end)

    it('drops a batch after max_attempts network failures and counts it', function()
        local env = new_env({ max_attempts = 3 })
        env.responses = { { status = 0 }, { status = 0 }, { status = 0 } }
        log_n(env.q, 2)
        advance(env, 2000)
        for _ = 1, 5 do advance(env, 70000) end
        assert.equals(3, #env.calls)
        local s = env.q:get_stats()
        assert.equals(2, s.dropped_retries)
        assert.equals(0, s.retry_batches)
    end)

    it('splits a batch on 413 and resends the halves', function()
        local env = new_env()
        env.responses = { { status = 413 } }
        log_n(env.q, 4)
        advance(env, 2000)
        assert.equals(3, #env.calls)
        assert.equals(4, count_events(env.calls[1].body))
        assert.equals(2, count_events(env.calls[2].body))
        assert.equals(2, count_events(env.calls[3].body))
        assert.equals(4, env.q:get_stats().accepted)
    end)

    it('drops a single event that still gets 413', function()
        local env = new_env()
        env.responses = { { status = 413 } }
        log_n(env.q, 1)
        advance(env, 2000)
        assert.equals(1, #env.calls)
        assert.equals(1, env.q:get_stats().dropped_http)
    end)

    it('drops on 422 without retrying', function()
        local env = new_env()
        env.responses = { { status = 422, body = { accepted = 0, rejected = 1, errors = {} } } }
        log_n(env.q, 1)
        advance(env, 2000)
        advance(env, 120000)
        assert.equals(1, #env.calls)
        assert.equals(1, env.q:get_stats().dropped_http)
    end)

    it('stops sending after 401, warns once and never prints the token', function()
        local env = new_env()
        env.responses = { { status = 401 } }
        log_n(env.q, 3)
        advance(env, 2000)
        assert.is_true(env.q:get_stats().disabled)
        log_n(env.q, 3)
        advance(env, 120000)
        assert.equals(1, #env.calls)
        assert.equals(1, #env.warnings)
        for _, w in ipairs(env.warnings) do
            assert.is_nil(w:find(TOKEN, 1, true))
        end
        -- resume works
        env.q:resume()
        log_n(env.q, 1)
        advance(env, 2000)
        assert.equals(2, #env.calls)
    end)

    it('treats a hanging request as a failure after http_timeout_ms', function()
        local env = new_env({ http_timeout_ms = 5000 })
        local pending
        env.q.http = function(url, method, body, headers, cb)
            env.calls[#env.calls + 1] = { body = body }
            pending = pending or cb -- the first request is never answered in time
        end
        log_n(env.q, 1)
        advance(env, 2000)
        assert.equals(1, #env.calls)
        advance(env, 4999)
        assert.equals(1, #env.calls)
        advance(env, 1)                    -- timeout -> requeued with backoff
        advance(env, 70000)
        assert.equals(2, #env.calls)
        pending(202, { accepted = 1, rejected = 0, errors = {} }) -- late callback is ignored
        assert.equals(0, env.q:get_stats().accepted)
    end)
end)

-- ---------------------------------------------------------------------------
-- bounded queue
-- ---------------------------------------------------------------------------

describe('overflow', function()
    it('drops the oldest events when the queue is full', function()
        local env = new_env({ max_queue = 3 })
        for i = 1, 5 do
            env.q:log({ event = 'e', message = 'msg' .. i })
        end
        local s = env.q:get_stats()
        assert.equals(3, s.queued)
        assert.equals(2, s.dropped_overflow)
        env.q:flush()
        local body = env.calls[1].body
        assert.is_nil(body:find('msg1"', 1, true))
        assert.is_nil(body:find('msg2"', 1, true))
        assert.truthy(body:find('msg3"', 1, true))
        assert.truthy(body:find('msg5"', 1, true))
    end)
end)

-- ---------------------------------------------------------------------------
-- redaction
-- ---------------------------------------------------------------------------

describe('redaction', function()
    it('masks sensitive keys at any depth', function()
        local env = new_env()
        env.q:log({
            event = 'login',
            attributes = {
                password = 'hunter2',
                nested = { api_token = 'abc123', ok = 1 },
                cardNumber = '4111111111111111',
                IBAN = 'IT60X0542811101000000123456',
                amount = 5,
            },
        })
        env.q:flush()
        local body = env.calls[1].body
        for _, secret in ipairs({ 'hunter2', 'abc123', '4111111111111111', 'IT60X0542811101000000123456' }) do
            assert.is_nil(body:find(secret, 1, true), secret)
        end
        assert.truthy(body:find('"password":"[REDACTED]"', 1, true))
        assert.truthy(body:find('"api_token":"[REDACTED]"', 1, true))
        assert.truthy(body:find('"amount":5', 1, true))
        assert.truthy(body:find('"ok":1', 1, true))
    end)

    it('honors a custom key list and an allow list', function()
        local env = new_env(nil, { redact = Redact.new({ keys = { 'pin', 'token' }, allow = { 'token_id' } }) })
        env.q:log({ event = 'x', attributes = { pin = '1234', token_id = 'visible', password = 'not-in-custom-list' } })
        env.q:flush()
        local body = env.calls[1].body
        assert.is_nil(body:find('1234', 1, true))
        assert.truthy(body:find('"token_id":"visible"', 1, true))
        assert.truthy(body:find('not-in-custom-list', 1, true))
    end)

    it('scrubs key=value fragments inside the message', function()
        local env = new_env()
        env.q:log({ event = 'x', message = 'login failed password=hunter2 for user' })
        env.q:flush()
        local body = env.calls[1].body
        assert.is_nil(body:find('hunter2', 1, true))
        assert.truthy(body:find('password=[REDACTED]', 1, true))
    end)
end)

-- ---------------------------------------------------------------------------
-- identifiers
-- ---------------------------------------------------------------------------

describe('identifiers', function()
    local function ids_source(calls)
        return function(pid)
            calls[#calls + 1] = pid
            return { license = 'license-AAA', discord = 'discord-BBB', steam = 'steam-CCC', ip = '203.0.113.9' }
        end
    end

    it('are off by default: the resolver is never even called', function()
        local calls = {}
        local env = new_env(nil, { get_identifiers = ids_source(calls) })
        env.q:log({ event = 'x', attributes = { player_id = 7 } })
        env.q:flush()
        assert.equals(0, #calls)
        local body = env.calls[1].body
        assert.is_nil(body:find('license-AAA', 1, true))
        assert.is_nil(body:find('203.0.113.9', 1, true))
        assert.truthy(body:find('"player_id":7', 1, true))
    end)

    it('are attached when opted in, and never include the IP unless include_ip is set', function()
        local calls = {}
        local env = new_env(
            { identifiers = { enabled = true, types = { 'license', 'discord', 'ip' } } },
            { get_identifiers = ids_source(calls) })
        env.q:log({ event = 'x', attributes = { player_id = 7 } })
        env.q:flush()
        local body = env.calls[1].body
        assert.truthy(body:find('license-AAA', 1, true))
        assert.truthy(body:find('discord-BBB', 1, true))
        assert.is_nil(body:find('steam-CCC', 1, true)) -- not in types
        assert.is_nil(body:find('203.0.113.9', 1, true)) -- 'ip' listed but include_ip is false
    end)

    it('include the IP only with both include_ip and the ip type', function()
        local calls = {}
        local env = new_env(
            { identifiers = { enabled = true, types = { 'license', 'ip' }, include_ip = true } },
            { get_identifiers = ids_source(calls) })
        env.q:log({ event = 'x', attributes = { player_id = 7 } })
        env.q:flush()
        assert.truthy(env.calls[1].body:find('203.0.113.9', 1, true))
    end)

    it('are not resolved for events without a player_id', function()
        local calls = {}
        local env = new_env({ identifiers = { enabled = true } }, { get_identifiers = ids_source(calls) })
        env.q:log({ event = 'x', attributes = { something = 1 } })
        env.q:flush()
        assert.equals(0, #calls)
    end)
end)

describe('log v2 tags', function()
    it('normalizes and validates tags without changing actor attributes', function()
        local env = new_env()
        env.q:log({ event = 'trade', tags = { 'Trade', 'trade', 'bad tag', string.rep('x', 41) }, attributes = { actor = { type = 'player', id = 7 } } })
        env.q:flush()
        local body = env.calls[1].body
        assert.truthy(body:find('"tags":["trade"]', 1, true))
        assert.truthy(body:find('"actor":', 1, true))
    end)
end)
