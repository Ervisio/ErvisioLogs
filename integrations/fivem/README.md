# ervisio_logs: FiveM resource for Ervisio Logs

A server-side FiveM resource that forwards events from your server (and from your other
resources) to the Ervisio Logs ingest API (`POST /api/v1/ingest/batch`).

> **Status: not tested in game.** The code was written and syntax-checked only. The unit
> specs in `tests/` were written but have not been run. Test on a development server
> first, and treat everything here as unverified until a human operator has done so.

## What it does

- Exports `Log` and `LogBatch` for other server resources.
- Keeps events in a bounded in-memory queue and sends them in batches
  (default: 200 events, about 800 KB or 2 s, whichever comes first) with
  `PerformHttpRequest`, which is asynchronous. The game thread is never blocked.
- Retries with exponential backoff and jitter. Honors `Retry-After` on 429 and 503.
  Splits a batch on 413. Drops a batch on 422 and other non-retryable 4xx.
  Stops sending and prints one console warning on 401/403.
- Gives every event a unique `id`, so a retried batch does not create duplicates.
- Redacts sensitive keys and adds player identifiers only if you opt in.
- Has no client scripts and registers no net events. The token never reaches a client.

## Install

1. Copy the `ervisio_logs` folder into your server `resources` directory.
2. In the Ervisio Logs UI, create a **project API token with only the `logs:ingest`
   scope**. Do not reuse a token that can read logs or manage the project.
3. In `server.cfg`, set the convars **before** `ensure ervisio_logs`:

   ```cfg
   set ervisio_logs_url "https://logs.example.com"
   set ervisio_logs_token "<project token>"
   set ervisio_logs_environment "production"
   # optional: set ervisio_logs_host "my-fivem-eu-1"   (defaults to sv_hostname)

   ensure ervisio_logs
   ```

   Use `set`, never `setr` (replicated to clients) or `sets` (published in server info).
   Keep `server.cfg` out of public repositories. The token is never written to a file by
   this resource and is never printed.
4. Make sure `ervisio_logs` starts before the resources that call it. If it is not
   started, calls to its exports raise an error in the calling resource (the examples
   wrap them in `pcall`).

The resource prints a console warning and stays off if the URL or token is missing.
Use `http://` only on localhost or a private network: the token travels in a header.

### Server console commands

Only usable from the server console or RCON.

| Command | Effect |
|---|---|
| `ervisio_logs_status` | Print queue counters (queued, accepted, rejected, retries, drops). |
| `ervisio_logs_resume` | Re-read `ervisio_logs_token` and resume sending after a 401/403. |

## Using the exports

```lua
-- exports.ervisio_logs:Log(stream, level, event, message, attributes)
-- Returns true if the event was queued.
exports.ervisio_logs:Log(nil, 'INFO', 'inventory.add', 'bread x2', {
    player_id = source,
    action = 'add',
    item = 'bread',
    count = 2,
})

-- exports.ervisio_logs:LogBatch(events) -> number queued
exports.ervisio_logs:LogBatch({
    { level = 'INFO', event = 'money.add', message = 'cash +500', attributes = { player_id = 3, amount = 500 } },
    { stream = 'moderation', level = 'WARN', event = 'moderation.kick', message = 'kicked', attributes = { player_id = 9 } },
})
```

| Argument | Notes |
|---|---|
| `stream` | Optional. If `nil`, the stream is chosen from the event prefix in `config.lua`, then `default_stream`. |
| `level` | `TRACE`, `DEBUG`, `INFO`, `NOTICE`, `WARN`, `ERROR`, `FATAL` (any case; `WARNING` and `CRITICAL` are mapped; anything else becomes `INFO`). |
| `event` | Up to 128 bytes, for example `inventory.add`. Use dotted names; the prefix selects the stream. |
| `message` | Truncated to `max_message_bytes` (8192 by default). |
| `attributes` | Table. Keys are rewritten to `[A-Za-z0-9_-]` (a dot becomes `_`), nesting is limited, empty tables are omitted. Functions and userdata are replaced by `<type>`. |

Set automatically on every event: `id`, `timestamp` (UTC, RFC 3339 with milliseconds),
`service`, `host`, and `resource` (the calling resource, from `GetInvokingResource()`).
By convention, put `player_id` (server id) and `action` in `attributes`.
`GetStats` returns the queue counters.

Both exports never raise on bad input: invalid events are counted in `dropped_invalid`.

### Examples (not loaded by the manifest)

Copy a file into one of your own resources and add it to that resource's `server_scripts`.

| File | Shows |
|---|---|
| `ervisio_logs/examples/esx/server.lua` | ESX money add/remove/set (wrapping the xPlayer methods), transfers, job change (`esx:setJob`). |
| `ervisio_logs/examples/ox_inventory/server.lua` | `registerHook` for `swapItems`, `buyItem`, `createItem`, plus add/remove wrappers. Hook and payload names that could not be verified are marked as such in comments. |
| `ervisio_logs/examples/moderation.lua` | Kick and ban wrappers and txAdmin kick/ban/warn events. |
| built in | Player join/leave with the drop reason (`features.player_connections = true`). |

## Configuration (`ervisio_logs/config.lua`)

Convars override the matching values. Durations are in milliseconds.

| Key | Default | Meaning |
|---|---|---|
| `url`, `environment` | empty, `production` | Fallbacks for the convars `ervisio_logs_url`, `ervisio_logs_environment`. |
| `service` | `fivem` | Sent as `service` on each event. |
| `default_stream` | `fivem` | Stream when no explicit stream and no prefix matches. |
| `streams` | list of `{ prefix, stream }` | Stream per event prefix; longest prefix wins. |
| `features.exports` | `true` | Allow other resources to call `Log`/`LogBatch`. |
| `features.startup_event` | `true` | Send `ervisio_logs.started` once. |
| `features.player_connections` | `false` | Log `player.join` / `player.leave` (with drop reason). |
| `privacy.player_name` | `false` | Add `GetPlayerName()` to the join/leave events. |
| `privacy.identifiers.enabled` | `false` | Master switch for player identifiers. |
| `privacy.identifiers.types` | `license`, `discord`, `steam` | Identifier types that may be attached. |
| `privacy.identifiers.include_ip` | `false` | The IP is sent only if this is `true` **and** `'ip'` is in `types`. |
| `redact.keys` | password, passwd, token, secret, card, cvv, iban, authorization, apikey, privatekey | A key containing one of these (case and punctuation ignored) is replaced by the mask. |
| `redact.allow` | empty | Exact keys that are never redacted. |
| `redact.scrub_message` | `true` | Also mask `password=...` style fragments inside `message`. |
| `queue.max_queue` | 5000 | In-memory limit; when full the **oldest** event is dropped. |
| `queue.batch_max_events` | 200 | At most 1000 (API limit). |
| `queue.batch_max_bytes` | 800000 | Body budget; the server limit is 1 MiB. |
| `queue.flush_interval_ms` | 2000 | Partial batches are sent after this long. |
| `queue.http_timeout_ms` | 30000 | A request with no answer is treated as a failure. |
| `queue.max_attempts` | 6 | Failed attempts (network error, 5xx) before a batch is dropped. |
| `queue.backoff_base_ms`, `queue.backoff_cap_ms` | 1000, 60000 | `base * 2^(n-1)`, capped, then 50 to 100 percent jitter. |
| `queue.retry_after_cap_ms` | 300000 | Upper bound honoured for `Retry-After`. |
| `queue.tick_ms` | 250 | Sender loop period. |
| `limits.*` | see file | Per-string, per-event, depth, key-count limits. |

## Behaviour on errors

| Response | Action |
|---|---|
| 202 | Done. `rejected > 0` is counted and warned about (throttled); rejected events are not retried. |
| 429, 503 | Wait `Retry-After` seconds (or backoff if absent), then resend the same batch. Does not consume `max_attempts`. |
| network error, timeout, other 5xx, 408 | Backoff with jitter; after `max_attempts` the batch is dropped and counted (`dropped_retries`). |
| 413 | Batch is split in two and resent; a single oversized event is dropped. |
| 400, 404, 415, 422, other 4xx | Batch dropped, counted in `dropped_http`, not retried. |
| 401, 403 | Sending stops, queued events are discarded, one console warning is printed. Fix the token, then `ervisio_logs_resume`. |

Console warnings are rate limited (about one per minute per kind).

## Privacy notes

- Nothing identifying a person is sent by default. Events carry the numeric server
  `player_id` only, and join/leave events carry no name.
- `license`, `discord`, `steam` identifiers are attached only when
  `privacy.identifiers.enabled = true`, and only for events whose attributes contain a
  `player_id`. The IP needs two explicit settings (`include_ip` and `'ip'` in `types`).
  Check your own legal basis and retention policy before enabling any of them.
- Redaction works on attribute **keys** (and `key=value` fragments in the message). It
  cannot recognise a secret stored under an innocent key, such as `{ note = 'pw: abc' }`.
  This resource does not inspect other attribute values: whatever your scripts pass in is
  sent, so do not log chat text, inventory metadata or free-form player input unless you
  mean to.
- Anything a calling resource puts into `attributes`, including its own identifier fields,
  is sent as given. The identifier switch only controls what this resource adds.
- The token is read from a convar on the server, held only inside the HTTP headers, never
  logged, and not accessible to clients. Anyone who can read `server.cfg` or run
  `GetConvar` in a server resource can still read it: only run server resources you trust.

## Limits and known gaps

- **Not tested in game.** Not run against a live Ervisio Logs server either. The ESX,
  ox_inventory and txAdmin examples use event, hook and payload names written from memory;
  the uncertain ones are marked `UNVERIFIED` in the comments. Check them on a dev server.
- The queue is in memory only. Events still queued when the server stops or crashes are
  lost (a best-effort flush is attempted when the resource stops). There is no disk queue.
- One request is in flight at a time, so sustained throughput is bounded by the round trip
  time of the API (about 200 events per request).
- The server timestamp has millisecond precision derived from `os.time()` plus the server
  timer, so it can drift slightly from wall-clock time on long-running servers.
- Redaction is substring based and intentionally eager: a key like `discard` matches
  `card`. Use `redact.allow` for false positives.

## Tests

`tests/queue_spec.lua` contains busted-style specs for the queue (batching, 429
`Retry-After`, 503 retry, 413 split, 422 drop, 401 stop, overflow, redaction, identifiers
off by default) using a fake clock and a mocked HTTP function. They were **written but not
executed**. To run them yourself, install Lua 5.4 and busted and, from this folder, run
`busted tests/queue_spec.lua`.

## Tags, actors and privacy

`Log(stream, level, event, message, attributes, tags)` accepts an optional sixth argument. `LogBatch` entries accept `tags` too. Tags are lowercased and deduplicated; invalid tokens are dropped with a rate-limited warning (maximum 20 tokens, 40 characters each; letters, digits, `. _ : / -`).

```lua
exports.ervisio_logs:Log('economy', 'INFO', 'money.transfer', 'Player 7 transferred 100 to player 8', {
    actor = { type = 'player', id = 7 },
    target = { type = 'player', id = 8 },
    amount = 100, account = 'bank',
}, { 'economy', 'transfer' })
```

Built-in connection events include `attributes.actor = {type='player', id=serverId}`. `actor.name` and the legacy `player_name` are sent only when `privacy.player_name = true` (default false). Examples use numeric server IDs and omit player names. Identifiers and IP addresses remain opt-in. Custom attributes are your responsibility: avoid copying player names or identifiers into messages and tags, which are not removed by the player-name toggle.

Apply the FiveM event preset in the project's event catalog; it supplies titles, templates and featured fields for all built-in events and the ESX, inventory and moderation examples. Existing customized catalog entries are preserved. Missing streams are created by the server when the project's automatic stream creation setting permits it; otherwise ingest rejects that event, so create the configured streams first.

Project field rules with `redact: true` replace matching attribute values with `[REDACTED]` before WAL persistence. They affect newly ingested events only.
