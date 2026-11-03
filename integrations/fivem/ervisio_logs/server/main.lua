-- FiveM glue for ervisio_logs. All queue / batch / retry logic lives in queue.lua.
-- Server side only. The project token is read from a convar here and handed to the
-- queue, which keeps it in the request headers. It is never logged, printed or sent
-- to a client (this resource registers no net events and has no client scripts).

local RES = GetCurrentResourceName()
local cfg = ErvisioConfig or {}
local features = cfg.features or {}
local privacy = cfg.privacy or {}

local function warn(message)
    print(('^3[%s]^7 %s'):format(RES, message))
end

-- ---------------------------------------------------------------------------
-- settings: convars win over config.lua
-- ---------------------------------------------------------------------------

local url = GetConvar('ervisio_logs_url', cfg.url or ''):gsub('%s+', '')
local token = GetConvar('ervisio_logs_token', '')
local environment = GetConvar('ervisio_logs_environment', cfg.environment or '')
local host = GetConvar('ervisio_logs_host', '')
if host == '' then host = GetConvar('sv_hostname', '') end
if host == '' then host = 'fivem-server' end
host = host:sub(1, 256)

local configError
if url == '' then
    configError = 'convar ervisio_logs_url is not set'
elseif not url:match('^https?://') then
    configError = 'ervisio_logs_url must start with http:// or https://'
elseif token == '' then
    configError = 'convar ervisio_logs_token is not set (use `set ervisio_logs_token "..."` in server.cfg)'
end

if not configError and url:match('^http://') then
    local h = url:match('^http://([^/:]+)')
    if h ~= 'localhost' and not (h and h:match('^127%.')) then
        warn('the token is sent over plain HTTP; use https:// or a private network')
    end
end

-- ---------------------------------------------------------------------------
-- queue
-- ---------------------------------------------------------------------------

local qopts = {
    url = url,
    environment = environment,
    service = cfg.service,
    host = host,
    default_stream = cfg.default_stream,
    streams = cfg.streams,
    identifiers = privacy.identifiers,
}
for k, v in pairs(cfg.queue or {}) do qopts[k] = v end
for k, v in pairs(cfg.limits or {}) do qopts[k] = v end

-- Epoch milliseconds that advance with the server timer (os.time only has 1 s resolution).
local epoch_base_ms = os.time() * 1000
local timer_base = GetGameTimer()

local function get_identifiers(src)
    local out = {}
    for _, id in ipairs(GetPlayerIdentifiers(src)) do
        local t, v = id:match('^(%w+):(.+)$')
        if t then out[t] = v end
    end
    return out
end

math.randomseed(os.time() + GetGameTimer())

local q = ErvisioQueue.new(qopts, {
    now = GetGameTimer,
    utc_ms = function() return epoch_base_ms + (GetGameTimer() - timer_base) end,
    http = function(requestUrl, method, body, headers, cb)
        PerformHttpRequest(requestUrl, function(status, resp, respHeaders)
            cb(status, resp, respHeaders)
        end, method, body, headers)
    end,
    json_encode = json.encode,
    json_decode = json.decode,
    random = math.random,
    redact = ErvisioRedact.new(cfg.redact),
    token = token,
    instance_id = ('%x%x'):format(os.time(), math.random(0, 0xFFFFFFF)),
    get_identifiers = get_identifiers,
    warn = warn,
})
token = nil -- the queue owns the only remaining reference

if configError then
    q:disable()
    warn(('log forwarding is OFF: %s'):format(configError))
end

-- ---------------------------------------------------------------------------
-- exports (server side only; other server resources call these)
-- ---------------------------------------------------------------------------

-- exports.ervisio_logs:Log(stream, level, event, message, attributes) -> boolean queued
exports('Log', function(stream, level, event, message, attributes)
    if features.exports == false then return false end
    return q:log({
        stream = stream, level = level, event = event, message = message,
        attributes = attributes, resource = GetInvokingResource() or RES,
    })
end)

-- exports.ervisio_logs:LogBatch({ { stream=, level=, event=, message=, attributes= }, ... }) -> count queued
exports('LogBatch', function(events)
    if features.exports == false then return 0 end
    return q:log_batch(events, GetInvokingResource() or RES)
end)

exports('GetStats', function()
    return q:get_stats()
end)

-- ---------------------------------------------------------------------------
-- sender loop: never blocks, PerformHttpRequest is asynchronous
-- ---------------------------------------------------------------------------

local running = true
local tickMs = (cfg.queue and cfg.queue.tick_ms) or 250

CreateThread(function()
    while running do
        Wait(tickMs)
        q:tick()
    end
end)

AddEventHandler('onResourceStop', function(stopped)
    if stopped ~= RES then return end
    running = false
    q:flush() -- best effort: one more request may still go out while the server shuts down
end)

-- ---------------------------------------------------------------------------
-- console commands (server console / RCON only, source 0)
-- ---------------------------------------------------------------------------

RegisterCommand('ervisio_logs_status', function(source)
    if source ~= 0 then return end
    local s = q:get_stats()
    print(('[%s] queued=%d accepted=%d rejected=%d requests=%d retries=%d disabled=%s'):format(
        RES, s.queued, s.accepted, s.rejected, s.requests, s.retries, tostring(s.disabled)))
    print(('[%s] dropped: overflow=%d retries=%d http=%d auth=%d disabled=%d invalid=%d'):format(
        RES, s.dropped_overflow, s.dropped_retries, s.dropped_http, s.dropped_auth,
        s.dropped_disabled, s.dropped_invalid))
end, true)

RegisterCommand('ervisio_logs_resume', function(source)
    if source ~= 0 then return end
    local newToken = GetConvar('ervisio_logs_token', '')
    if url == '' or newToken == '' then
        warn('cannot resume: ervisio_logs_url / ervisio_logs_token are not set')
        return
    end
    q:resume(newToken)
    print(('[%s] sending resumed'):format(RES))
end, true)

-- ---------------------------------------------------------------------------
-- built-in hooks (feature toggles in config.lua)
-- ---------------------------------------------------------------------------

if features.player_connections then
    -- Plain AddEventHandler (no RegisterNetEvent): clients cannot trigger these.
    AddEventHandler('playerJoining', function()
        local src = source
        local attributes = { player_id = src, action = 'join' }
        if privacy.player_name then attributes.player_name = GetPlayerName(src) end
        q:log({
            level = 'INFO', event = 'player.join', message = 'Player joined',
            attributes = attributes, resource = RES,
        })
    end)

    AddEventHandler('playerDropped', function(reason)
        local src = source
        local attributes = {
            player_id = src, action = 'leave',
            reason = type(reason) == 'string' and reason:sub(1, 256) or nil,
        }
        if privacy.player_name then attributes.player_name = GetPlayerName(src) end
        q:log({
            level = 'INFO', event = 'player.leave', message = 'Player left',
            attributes = attributes, resource = RES,
        })
    end)
end

if features.startup_event ~= false and not configError then
    q:log({
        level = 'INFO', event = 'ervisio_logs.started', message = 'Log forwarding started',
        attributes = { version = GetResourceMetadata(RES, 'version', 0), environment = environment },
        resource = RES,
    })
end
