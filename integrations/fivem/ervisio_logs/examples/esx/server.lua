-- EXAMPLE (not loaded by the ervisio_logs manifest).
-- Copy this file into your own server-side resource and add it to its `server_scripts`.
--
-- Shows how to send ESX money and job events to Ervisio Logs through the ervisio_logs
-- server exports. Written against ESX Legacy; verify the names marked UNVERIFIED against
-- the es_extended version you run. Nothing here has been tested in game.

local ESX = exports['es_extended']:getSharedObject()

-- The export call is wrapped in pcall: a logging problem (or ervisio_logs not started)
-- must never break your gameplay code.
local function elog(level, event, message, attributes)
    pcall(function()
        if attributes.player_id then attributes.actor = { type = 'player', id = attributes.player_id } end
        if attributes.target_id then attributes.target = { type = 'player', id = attributes.target_id } end
        if attributes.from_player then attributes.actor = { type = 'player', id = attributes.from_player } end
        if attributes.to_player then attributes.target = { type = 'player', id = attributes.to_player } end
        exports.ervisio_logs:Log(nil, level, event, message, attributes, { 'economy' })
    end)
end

-- ---------------------------------------------------------------------------
-- Money: wrap the xPlayer methods when a player loads.
--
-- ESX Legacy defines the xPlayer methods as closures (xPlayer.addMoney(money, reason)),
-- so the wrapper receives the same arguments as the original. UNVERIFIED: if your ESX
-- version defines them with a colon (xPlayer:addMoney) the first argument will be the
-- player object and the argument positions below shift by one.
-- ESX emits no server-side "money changed" event by default (UNVERIFIED for newer
-- versions), hence the wrapper. Alternative: call logMoney() from your own scripts.
-- ---------------------------------------------------------------------------

local function logMoney(xPlayer, action, account, amount, reason)
    local balance
    pcall(function()
        local acc = xPlayer.getAccount(account)
        balance = acc and acc.money
    end)
    elog('INFO', 'money.' .. action, ('%s %s %s'):format(action, tostring(amount), account), {
        player_id = xPlayer.source,   -- server id only; no identifier unless you opt in
        action = action,
        account = account,
        amount = amount,
        reason = reason,
        balance_after = balance,
    })
end

local function wrap(xPlayer, name, handler)
    local original = xPlayer[name]
    if type(original) ~= 'function' then return end
    xPlayer[name] = function(...)
        local result = table.pack(original(...))
        pcall(handler, ...)
        return table.unpack(result, 1, result.n)
    end
end

local wrapped = setmetatable({}, { __mode = 'k' })

local function instrument(xPlayer)
    if not xPlayer or wrapped[xPlayer] then return end
    wrapped[xPlayer] = true

    wrap(xPlayer, 'addMoney',    function(amount, reason) logMoney(xPlayer, 'add',    'money', amount, reason) end)
    wrap(xPlayer, 'removeMoney', function(amount, reason) logMoney(xPlayer, 'remove', 'money', amount, reason) end)
    wrap(xPlayer, 'setMoney',    function(amount, reason) logMoney(xPlayer, 'set',    'money', amount, reason) end)

    wrap(xPlayer, 'addAccountMoney',    function(account, amount, reason) logMoney(xPlayer, 'add',    account, amount, reason) end)
    wrap(xPlayer, 'removeAccountMoney', function(account, amount, reason) logMoney(xPlayer, 'remove', account, amount, reason) end)
    wrap(xPlayer, 'setAccountMoney',    function(account, amount, reason) logMoney(xPlayer, 'set',    account, amount, reason) end)
end

-- esx:playerLoaded (playerId, xPlayer, isNew) is the server event of ESX Legacy.
AddEventHandler('esx:playerLoaded', function(_, xPlayer)
    instrument(xPlayer)
end)

-- Players already loaded when this resource (re)starts. UNVERIFIED: ESX.GetExtendedPlayers
-- exists in ESX Legacy 1.6+.
CreateThread(function()
    if ESX.GetExtendedPlayers then
        for _, xPlayer in pairs(ESX.GetExtendedPlayers()) do
            instrument(xPlayer)
        end
    end
end)

-- ---------------------------------------------------------------------------
-- Player to player transfer: call this from your own pay/give-money code.
-- ---------------------------------------------------------------------------

function LogMoneyTransfer(fromSource, toSource, account, amount, reason)
    elog('INFO', 'money.transfer', ('transfer of %s (%s)'):format(tostring(amount), account), {
        player_id = fromSource,
        target_id = toSource,
        action = 'transfer',
        account = account,
        amount = amount,
        reason = reason,
    })
end

-- ---------------------------------------------------------------------------
-- Job change. ESX Legacy triggers this server event from xPlayer.setJob:
--   TriggerEvent('esx:setJob', source, job, lastJob)
-- job / lastJob are tables with name, grade, label, grade_label (UNVERIFIED fields).
-- ---------------------------------------------------------------------------

AddEventHandler('esx:setJob', function(source, job, lastJob)
    if type(job) ~= 'table' then return end
    elog('INFO', 'job.change', ('job changed to %s'):format(tostring(job.name)), {
        player_id = source,
        action = 'set_job',
        job = job.name,
        job_grade = job.grade,
        previous_job = type(lastJob) == 'table' and lastJob.name or nil,
        previous_grade = type(lastJob) == 'table' and lastJob.grade or nil,
    })
end)
