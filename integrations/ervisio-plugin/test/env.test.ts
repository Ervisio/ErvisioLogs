import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ensureSecrets, envGet, envKeys, envSet, oneLine, unpassedKeys } from '../src/api/env.ts';

test('envSet replaces an active line in place', () => {
  assert.equal(envSet('A=1\nB=2\n', 'A', '9'), 'A=9\nB=2\n');
});

test('envSet replaces the last active line when the key repeats', () => {
  assert.equal(envGet(envSet('A=1\nA=2\n', 'A', '9'), 'A'), '9');
});

test('envSet uncomments a commented key instead of appending a duplicate', () => {
  for (const c of ['# A=1', '#A=1', '#   A=1', '#\tA=1', '# export A=1']) {
    assert.equal(envSet(`x=1\n${c}\ny=2\n`, 'A', '9'), 'x=1\nA=9\ny=2\n', c);
  }
});

test('envSet recognises the export prefix without duplicating the key', () => {
  for (const l of ['export A=1', 'export   A=1']) {
    assert.equal(envSet(`${l}\nB=2\n`, 'A', '9'), 'A=9\nB=2\n', l);
  }
});

test('envSet prefers an active line over a commented one', () => {
  assert.equal(envSet('# A=1\nA=2\n', 'A', '9'), '# A=1\nA=9\n');
});

test('envSet leaves keys that share a prefix and prose comments alone', () => {
  assert.equal(envSet('# A_B=1\nA_B=2\n# A is a key\n', 'A', '9'), '# A_B=1\nA_B=2\n# A is a key\nA=9\n');
});

test('envSet appends a new key before the trailing newline, or at the end', () => {
  assert.equal(envSet('A=1\n', 'B', '2'), 'A=1\nB=2\n');
  assert.equal(envSet('A=1', 'B', '2'), 'A=1\nB=2');
  assert.equal(envSet('', 'B', '2'), 'B=2\n');
});

test('envSet keeps a value on one line and replaces a quoted line', () => {
  assert.equal(envSet('A="old value"\n', 'A', 'new\nB=evil'), 'A=newB=evil\n');
  assert.equal(oneLine('  x\r\ny  '), 'xy');
});

test('envGet reads plain, quoted, exported and CRLF lines, and the last one wins', () => {
  assert.equal(envGet('A="x y"\n', 'A'), 'x y');
  assert.equal(envGet("A='x'\n", 'A'), 'x');
  assert.equal(envGet('export A=1\r\n', 'A'), '1');
  assert.equal(envGet('A=1\nA=2\n', 'A'), '2');
  assert.equal(envGet('# A=1\n', 'A'), null);
  assert.equal(envGet(null, 'A'), null);
});

test('envKeys lists active keys with a value', () => {
  assert.deepEqual(envKeys('A=1\n# B=2\nexport C="3"\nD=\nE=""\nA=4\n'), ['A', 'C']);
});

test('unpassedKeys reports ERVISIO_LOGS_ values that compose.yaml does not pass', () => {
  const compose = [
    'x-app-env: &app-env',
    '  ERVISIO_LOGS_ENV: ${ERVISIO_LOGS_ENV:-development}',
    '  ERVISIO_LOGS_HTTP_ADDR: ":8080"',
    '  POSTGRES_X: ${POSTGRES_PASSWORD:?run}',
  ].join('\n');
  const env = 'ERVISIO_LOGS_ENV=production\nERVISIO_LOGS_HTTP_ADDR=:9000\nERVISIO_LOGS_ARCHIVE_INTERVAL=1m\n# ERVISIO_LOGS_SPOOL_DIR=/x\nPOSTGRES_PASSWORD=p\nOTHER=1\n';
  assert.deepEqual(unpassedKeys(env, compose), ['ERVISIO_LOGS_HTTP_ADDR', 'ERVISIO_LOGS_ARCHIVE_INTERVAL']);
  assert.deepEqual(unpassedKeys(env, null), []);
});

test('ensureSecrets never replaces a secret that is already set, so an update keeps SECRET_KEY', () => {
  const key = 'k'.repeat(48);
  for (const line of [`ERVISIO_LOGS_SECRET_KEY=${key}`, `export ERVISIO_LOGS_SECRET_KEY=${key}`, `ERVISIO_LOGS_SECRET_KEY="${key}"`, 'ERVISIO_LOGS_SECRET_KEY=short']) {
    const env = `POSTGRES_PASSWORD=p\nCLICKHOUSE_PASSWORD=c\n${line}\n`;
    const logged: string[] = [];
    assert.equal(ensureSecrets(env, (l) => logged.push(l)), env, line);
    assert.deepEqual(logged, []);
  }
});

test('ensureSecrets fills missing and change_me secrets with random values', () => {
  const out = ensureSecrets('POSTGRES_PASSWORD=change_me\n# ERVISIO_LOGS_SECRET_KEY=\nOTHER=1\n', () => {});
  assert.match(envGet(out, 'POSTGRES_PASSWORD') ?? '', /^[A-Za-z0-9]{32}$/);
  assert.match(envGet(out, 'CLICKHOUSE_PASSWORD') ?? '', /^[A-Za-z0-9]{32}$/);
  assert.match(envGet(out, 'ERVISIO_LOGS_SECRET_KEY') ?? '', /^[A-Za-z0-9]{48}$/);
  assert.equal(envGet(out, 'OTHER'), '1');
});

test('server editor covers the documented deployment environment and masks secret values', async () => {
  const { readFile } = await import('node:fs/promises');
  const { SERVER_ENV_KEYS, secretEnvKey } = await import('../src/api/server-env.ts');
  const example = await readFile(new URL('../../../.env.example', import.meta.url), 'utf8');
  const keys = [...example.matchAll(/^\s*(?:#\s*)?((?:ERVISIO_LOGS_|POSTGRES_|CLICKHOUSE_)[A-Z0-9_]+)=/gm)].map((m) => m[1]);
  for (const key of keys) assert.ok(SERVER_ENV_KEYS.includes(key), key);
  for (const key of ['ERVISIO_LOGS_SECRET_KEY', 'ERVISIO_LOGS_POSTGRES_DSN', 'POSTGRES_PASSWORD', 'ERVISIO_LOGS_SMTP_PASSWORD']) assert.equal(secretEnvKey(key), true);
  assert.equal(secretEnvKey('ERVISIO_LOGS_SECRET_KEY_FILE'), false);
});
test('admin socket capabilities authorize management routes and never log data or session endpoints', async () => {
  const { readFile } = await import('node:fs/promises');
  const manifest = JSON.parse(await readFile(new URL('../plugin/manifest.json', import.meta.url), 'utf8'));
  for (const name of ['logs-admin-docker', 'logs-admin-standalone']) {
    const capability = manifest.capabilities.http.find((c) => c.name === name);
    const pattern = new RegExp(`^(?:${capability.rules[0].path})$`);
    for (const path of ['/admin/health', '/admin/settings/log.level', '/admin/users/aabb/password', '/admin/users/aabb/sessions/ccdd', '/workspaces', '/workspaces/aabb/members/ccdd', '/projects/aabb/settings/streams.auto_create', '/projects/aabb/streams/ccdd', '/tokens', '/admin/storage/secrets/s3.json', '/admin/retention/instance/aabb']) assert.equal(pattern.test(`/api/v1${path}`), true, `${name} ${path}`);
    for (const path of ['/logs/search', '/logs/export', '/auth/me', '/admin/tokens', '/ingest', '/admin/users/aabb/password/anything']) assert.equal(pattern.test(`/api/v1${path}`), false, path);
    assert.equal(capability.admin, true);
    assert.ok(manifest.capabilities.sockets.includes(capability.socket));
  }
  assert.equal(manifest.capabilities.network.userHosts, true);
});
