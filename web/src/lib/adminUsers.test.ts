import { adminUsers, validAdminPassword } from './adminUsers';
import { ApiError, setCsrfToken } from './api';

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

test('uses the F2 mutation methods with same-origin cookies and CSRF through the shared transport', async () => {
  const fetcher = vi.fn().mockResolvedValue({ ok: true, status: 204 });
  vi.stubGlobal('fetch', fetcher);
  setCsrfToken('csrf-test');
  await adminUsers.update('u/1', { status: 'disabled' });
  await adminUsers.resetPassword('u/1', 'a-long-password');
  await adminUsers.revokeSession('u/1', 's/2');
  await adminUsers.remove('u/1');
  expect(fetcher.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
    ['/api/v1/admin/users/u%2F1', 'PATCH'], ['/api/v1/admin/users/u%2F1/password', 'PUT'],
    ['/api/v1/admin/users/u%2F1/sessions/s%2F2', 'DELETE'], ['/api/v1/admin/users/u%2F1', 'DELETE'],
  ]);
  for (const [, init] of fetcher.mock.calls) {
    expect(init.credentials).toBe('same-origin');
    expect(init.headers['X-CSRF-Token']).toBe('csrf-test');
  }
  expect(JSON.parse(fetcher.mock.calls[1][1].body)).toEqual({ password: 'a-long-password' });
});

test('preserves backend conflicts and propagates unauthorized responses to the caller', async () => {
  const fetcher = vi.fn().mockResolvedValueOnce({ ok: false, status: 409, headers: new Headers(), json: async () => ({ detail: 'the instance needs at least one active administrator' }) })
    .mockResolvedValueOnce({ ok: false, status: 401, headers: new Headers(), json: async () => ({ detail: 'unauthorized' }) })
    .mockResolvedValue({ ok: true, status: 204 });
  vi.stubGlobal('fetch', fetcher); setCsrfToken('csrf-test');
  await expect(adminUsers.update('u1', { is_instance_admin: false })).rejects.toMatchObject({ status: 409, message: 'the instance needs at least one active administrator' });
  await expect(adminUsers.remove('u1')).rejects.toBeInstanceOf(ApiError);
});

test('normalizes empty pages and encodes the backend cursor and search without client-side status filtering', async () => {
  const fetcher = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ items: null, next_cursor: null }) });
  vi.stubGlobal('fetch', fetcher);
  expect(await adminUsers.list('ada@example.com', 'cursor/+')).toEqual({ items: [], next_cursor: null });
  const url = new URL(fetcher.mock.calls[0][0], 'https://example.com');
  expect(url.searchParams.get('q')).toBe('ada@example.com');
  expect(url.searchParams.get('cursor')).toBe('cursor/+');
  expect(await adminUsers.sessions('u1')).toEqual([]);
});

test('validates password character and byte limits with the same identity comparison as F2', () => {
  const identity = { email: 'ada@example.com', username: 'administrator' };
  expect(validAdminPassword('界'.repeat(10), identity)).toBe(true);
  expect(validAdminPassword('界'.repeat(43), identity)).toBe(false);
  expect(validAdminPassword('ADMINISTRATOR', identity)).toBe(false);
  expect(validAdminPassword('ADA@EXAMPLE.COM', identity)).toBe(false);
});
