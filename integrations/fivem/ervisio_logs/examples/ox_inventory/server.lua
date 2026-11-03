-- EXAMPLE (not loaded by the ervisio_logs manifest).
-- Copy this file into your own server-side resource and add it to its `server_scripts`.
--
-- ox_inventory item hooks forwarded to Ervisio Logs. Nothing here has been tested in game.
--
-- Hook API: exports.ox_inventory:registerHook(eventName, callback, options) -> hookId
-- ox_inventory runs hooks synchronously inside the inventory operation, so the callback
-- must be fast, must not error and must NOT return false (returning false cancels the
-- action). Everything below only enqueues a log event and returns nothing.
--
-- Hook names this example uses:
--   swapItems    known (documented by ox_inventory)
--   buyItem      known
--   createItem   known
--   craftItem    known
--   usingItem    UNVERIFIED, depends on the ox_inventory version; check its docs
-- Payload field names (source, action, fromInventory, toInventory, fromType, toType,
-- fromSlot, toSlot, count, shopType, itemName, ...) are written from memory and are
-- UNVERIFIED: print one payload on a dev server (options.print = true) before relying
-- on them. Item metadata is deliberately not logged (it may hold sensitive data).

local function elog(level, event, message, attributes)
    pcall(function()
        exports.ervisio_logs:Log(nil, level, event, message, attributes)
    end)
end

local function slotName(slot)
    return type(slot) == 'table' and slot.name or nil
end

-- Add / remove between inventories: moves between a player and a stash, drop, trunk,
-- glovebox, ... Moves inside the same inventory are skipped to keep the volume down.
exports.ox_inventory:registerHook('swapItems', function(payload)
    if payload.fromInventory == payload.toInventory then return end

    local item = slotName(payload.fromSlot) or slotName(payload.toSlot)
    local fromPlayer = payload.fromType == 'player'
    local toPlayer = payload.toType == 'player'

    local action = 'move'
    if fromPlayer and not toPlayer then action = 'remove'
    elseif toPlayer and not fromPlayer then action = 'add' end

    elog('INFO', 'inventory.' .. action, ('%s x%s'):format(tostring(item), tostring(payload.count)), {
        player_id = payload.source,
        action = action,
        item = item,
        count = payload.count,
        from_type = payload.fromType,
        from_inventory = tostring(payload.fromInventory),
        to_type = payload.toType,
        to_inventory = tostring(payload.toInventory),
    })
end, {
    -- options.itemFilter / options.inventoryFilter can narrow this further, for example
    -- itemFilter = { money = true, black_money = true }
    -- UNVERIFIED filter formats; see the ox_inventory hook documentation.
})

-- Shop purchase.
exports.ox_inventory:registerHook('buyItem', function(payload)
    elog('INFO', 'inventory.buy', ('bought %s x%s'):format(tostring(payload.itemName), tostring(payload.count)), {
        player_id = payload.source,
        action = 'buy',
        item = payload.itemName,
        count = payload.count,
        shop = payload.shopType,
        price = payload.totalPrice,
        currency = payload.currency,
    })
end, {})

-- Item created (crafting results, shop stock, ...). The callback may return metadata
-- in ox_inventory; this one returns nothing, so it never changes the item.
exports.ox_inventory:registerHook('createItem', function(payload)
    elog('DEBUG', 'inventory.create', ('created %s'):format(tostring(payload.item and payload.item.name)), {
        action = 'create',
        item = payload.item and payload.item.name,
        count = payload.count,
        inventory = tostring(payload.inventoryId),
    })
end, {})

-- Items added or removed by other scripts through the exports (AddItem / RemoveItem)
-- do not necessarily pass through the hooks above (UNVERIFIED). Log those explicitly:
function LoggedAddItem(source, item, count, metadata)
    local ok = exports.ox_inventory:AddItem(source, item, count, metadata)
    elog('INFO', 'inventory.add', ('%s x%s'):format(item, tostring(count)), {
        player_id = source, action = 'add', item = item, count = count, success = ok and true or false,
    })
    return ok
end

function LoggedRemoveItem(source, item, count, metadata)
    local ok = exports.ox_inventory:RemoveItem(source, item, count, metadata)
    elog('INFO', 'inventory.remove', ('%s x%s'):format(item, tostring(count)), {
        player_id = source, action = 'remove', item = item, count = count, success = ok and true or false,
    })
    return ok
end
