-- EXAMPLE (not loaded by the ervisio_logs manifest).
-- Copy this file into your own server-side resource and add it to its `server_scripts`.
--
-- Moderation audit trail: kick / ban / warn. Nothing here has been tested in game.

local function elog(level, event, message, attributes)
    pcall(function()
        if attributes.admin_id then attributes.actor = { type = 'admin', id = attributes.admin_id } end
        if attributes.player_id then attributes.target = { type = 'player', id = attributes.player_id } end
        exports.ervisio_logs:Log(nil, level, event, message, attributes, { 'moderation' })
    end)
end

-- ---------------------------------------------------------------------------
-- 1. Wrappers for your own admin commands or menu.
--    adminSource is a server id, or 0 for the console.
-- ---------------------------------------------------------------------------

function ModKick(adminSource, targetSource, reason)
    elog('WARN', 'moderation.kick', 'player kicked', {
        player_id = targetSource,   -- identifiers of the target are attached only if the
        admin_id = adminSource,     -- operator opted in (config.lua > privacy.identifiers)
        action = 'kick',
        reason = reason,
    })
    DropPlayer(targetSource, reason or 'Kicked')
end

-- Call this from YOUR ban system after it stored the ban. `expires` is whatever your
-- system uses (unix time, ISO string); nil means permanent.
function ModBan(adminSource, targetSource, reason, expires)
    elog('WARN', 'moderation.ban', 'player banned', {
        player_id = targetSource,
        admin_id = adminSource,
        action = 'ban',
        reason = reason,
        expires = expires,
        permanent = expires == nil,
    })
end

-- ---------------------------------------------------------------------------
-- 2. txAdmin events. These are server-local events (AddEventHandler without
--    RegisterNetEvent, so clients cannot trigger them). Field names are written from
--    memory of the txAdmin docs and are UNVERIFIED: check them against your txAdmin
--    version. The target's identifiers (targetIds) are intentionally NOT logged here.
-- ---------------------------------------------------------------------------

AddEventHandler('txAdmin:events:playerKicked', function(data)
    if type(data) ~= 'table' then return end
    elog('WARN', 'moderation.kick', 'player kicked (txAdmin)', {
        player_id = data.target,
        action = 'kick',
        admin = data.author,
        reason = data.reason,
        via = 'txadmin',
    })
end)

AddEventHandler('txAdmin:events:playerBanned', function(data)
    if type(data) ~= 'table' then return end
    elog('WARN', 'moderation.ban', 'player banned (txAdmin)', {
        player_id = data.targetNetId,
        action = 'ban',
        admin = data.author,
        reason = data.reason,
        action_id = data.actionId,
        expiration = data.expiration,
        duration = data.durationTranslated,
        via = 'txadmin',
    })
end)

AddEventHandler('txAdmin:events:playerWarned', function(data)
    if type(data) ~= 'table' then return end
    elog('NOTICE', 'moderation.warn', 'player warned (txAdmin)', {
        player_id = data.targetNetId,
        action = 'warn',
        admin = data.author,
        reason = data.reason,
        action_id = data.actionId,
        via = 'txadmin',
    })
end)
