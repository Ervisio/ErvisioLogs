fx_version 'cerulean'
game 'gta5'
lua54 'yes'

name 'ervisio_logs'
description 'Server-side log forwarding to the Ervisio Logs ingest API'
author 'Ervisio'
version '0.1.0'

-- Server only: no client scripts, no shared scripts, no `files`. Nothing from this
-- resource (config included) is downloaded by players, and the project token never
-- leaves the server.
server_only 'yes'

server_scripts {
    'config.lua',
    'server/redact.lua',
    'server/queue.lua',
    'server/main.lua',
}

-- Server exports (documented in README.md):
--   exports.ervisio_logs:Log(stream, level, event, message, attributes)
--   exports.ervisio_logs:LogBatch(events)
--   exports.ervisio_logs:GetStats()
