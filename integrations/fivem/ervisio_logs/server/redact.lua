-- Redaction helper. Pure Lua, no FiveM globals.
--
--   local R = Redact.new({ keys = {...}, allow = {...}, mask = '[REDACTED]', scrub_message = true })
--   R.is_sensitive('api_token')   --> true
--   R.scrub_text('password=abc')  --> 'password=[REDACTED]'
--   R.mask
--
-- Matching is by substring on the key lowercased with every non-alphanumeric character
-- removed, so "Api-Token", "api_token" and "apiToken" are all caught by 'token'.

local Redact = {}

Redact.DEFAULT_KEYS = {
    'password', 'passwd', 'token', 'secret', 'card', 'cvv', 'iban',
    'authorization', 'apikey', 'privatekey',
}

local function normalize(s)
    return (tostring(s):lower():gsub('[^a-z0-9]', ''))
end

local function escape_pattern(s)
    return (s:gsub('[%^%$%(%)%%%.%[%]%*%+%-%?]', '%%%0'))
end

local function case_insensitive(s)
    return (s:gsub('%a', function(c) return '[' .. c:lower() .. c:upper() .. ']' end))
end

function Redact.new(opts)
    opts = opts or {}
    local keys, allow, text_patterns = {}, {}, {}

    for _, k in ipairs(opts.keys or Redact.DEFAULT_KEYS) do
        local n = normalize(k)
        if n ~= '' then
            keys[#keys + 1] = n
            -- key=value / "key":"value" / key: value, for free text such as messages
            text_patterns[#text_patterns + 1] = '(' .. case_insensitive(escape_pattern(tostring(k))) .. '[%w_%-]*)'
                .. '(["\']?%s*[=:]%s*["\']?)([^%s,;&"\'}]+)'
        end
    end
    for _, k in ipairs(opts.allow or {}) do
        allow[normalize(k)] = true
    end

    local self = {
        mask = opts.mask or '[REDACTED]',
        scrub_message = opts.scrub_message ~= false,
    }

    -- Small bounded memo: keys come from server scripts but may be dynamic.
    local memo, memo_size = {}, 0

    function self.is_sensitive(key)
        local hit = memo[key]
        if hit ~= nil then return hit end
        local n = normalize(key)
        local result = false
        if n ~= '' and not allow[n] then
            for i = 1, #keys do
                if n:find(keys[i], 1, true) then
                    result = true
                    break
                end
            end
        end
        if memo_size >= 2000 then
            memo, memo_size = {}, 0
        end
        if type(key) == 'string' then
            memo[key] = result
            memo_size = memo_size + 1
        end
        return result
    end

    function self.scrub_text(s)
        if not self.scrub_message or type(s) ~= 'string' or s == '' then return s end
        local mask = self.mask
        for i = 1, #text_patterns do
            s = s:gsub(text_patterns[i], function(k, sep) return k .. sep .. mask end)
        end
        return s
    end

    return self
end

ErvisioRedact = Redact
return Redact
