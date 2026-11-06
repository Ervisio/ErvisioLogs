import assert from 'node:assert/strict';
import { test } from 'node:test';
import { AdminError, createAdminClient, createFetchRequest, decodeResponse, type Request } from '../src/index.ts';

test('settings writes escape identifiers and keep typed values', async () => {
  const calls: unknown[] = [];
  const request: Request = async <T>(...args: unknown[]) => {
    calls.push(args);
    return {} as T;
  };
  await createAdminClient(request).setSetting('/admin', 'a/b', false);
  assert.deepEqual(calls, [['PUT', '/admin/settings/a%2Fb', { value: false }]]);
});
test('workspace deletion sends its typed confirmation body', async () => {
  const calls: unknown[] = [];
  const request: Request = async <T>(...args: unknown[]) => {
    calls.push(args);
    return undefined as T;
  };
  await createAdminClient(request).deleteWorkspace('abc', 'game-server');
  assert.deepEqual(calls, [['DELETE', '/workspaces/abc', { confirm: 'game-server' }]]);
});
test('empty list envelopes become arrays and pagination preserves cursor', async () => {
  const paths: string[] = [];
  const request: Request = async <T>(_method: string, path: string) => {
    paths.push(path);
    return { items: null, next_cursor: 'next' } as T;
  };
  assert.deepEqual(await createAdminClient(request).workspaces(), []);
  assert.equal((await createAdminClient(request).users('a+b@example.org', 'a/b')).next_cursor, 'next');
  assert.equal(paths[1], '/admin/users?q=a%2Bb%40example.org&cursor=a%2Fb&limit=100');
});
test('response errors preserve RFC7807 detail and status without exposing body text', () => {
  assert.throws(
    () => decodeResponse(409, '{"detail":"Last owner"}'),
    (e: unknown) => e instanceof AdminError && e.status === 409 && e.message === 'Last owner',
  );
  assert.throws(() => decodeResponse(502, 'secret upstream text'), /Invalid JSON response \(502\)/);
  assert.equal(decodeResponse(204, ''), undefined);
});
test('fetch transport rejects credential-bearing and non-HTTP URLs before requesting', () => {
  for (const baseUrl of ['file:///tmp/a', 'https://user:password@example.org', 'https://example.org/#token'])
    assert.throws(() => createFetchRequest({ baseUrl }));
});
test('Bearer transport omits cookies, prefixes API once and sends JSON', async () => {
  const original = globalThis.fetch;
  let url: unknown, options: RequestInit | undefined;
  globalThis.fetch = async (input, init) => {
    url = input;
    options = init;
    return new Response('{"ok":true}', { status: 200 });
  };
  try {
    await createFetchRequest({ baseUrl: 'https://logs.example.org/', token: 'evl_adm_test' })(
      'PUT',
      '/admin/settings/log.level',
      { value: 'warn' },
    );
    assert.equal(url, 'https://logs.example.org/api/v1/admin/settings/log.level');
    assert.equal(options?.credentials, 'omit');
    assert.deepEqual(options?.headers, {
      Accept: 'application/json',
      'Content-Type': 'application/json',
      Authorization: 'Bearer evl_adm_test',
    });
    assert.equal(options?.body, '{"value":"warn"}');
  } finally {
    globalThis.fetch = original;
  }
});
test('session transport sends CSRF only on unsafe requests', async () => {
  const original = globalThis.fetch;
  const headers: HeadersInit[] = [];
  globalThis.fetch = async (_input, init) => {
    headers.push(init?.headers ?? {});
    return new Response(null, { status: 204 });
  };
  try {
    const request = createFetchRequest({ baseUrl: 'https://logs.example.org', csrfToken: () => 'csrf' });
    await request('GET', '/admin/health');
    await request('DELETE', '/admin/users/abc');
    assert.equal(new Headers(headers[0]).has('X-CSRF-Token'), false);
    assert.equal(new Headers(headers[1]).get('X-CSRF-Token'), 'csrf');
  } finally {
    globalThis.fetch = original;
  }
});
