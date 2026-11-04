import { adminTokens, adminTokenIssue } from './adminTokens';
import { adminHealth } from './adminHealth';
import { setCsrfToken } from './api';

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

test('session-only token endpoints use cookies and CSRF without Bearer authorization', async () => {
  const fetcher = vi.fn(async (_path: string, init: RequestInit) => ({ ok: true, status: init.method === 'DELETE' ? 204 : 200, json: async () => ({ items: null }) }));
  vi.stubGlobal('fetch', fetcher);
  setCsrfToken('csrf-session');
  const signal = new AbortController().signal;
  expect(await adminTokens.list(signal)).toEqual([]);
  await adminTokens.create({ name: 'Deploy', scopes: ['admin:read'], expires_at: '2027-01-01T00:00:00Z' }, signal);
  await adminTokens.revoke('a/b', signal);
  await adminHealth.get(signal);
  expect(fetcher.mock.calls.map(([path, init]) => [path, init.method])).toEqual([
    ['/api/v1/admin/tokens', 'GET'], ['/api/v1/admin/tokens', 'POST'], ['/api/v1/admin/tokens/a%2Fb', 'DELETE'], ['/api/v1/admin/health', 'GET'],
  ]);
  for (const [, init] of fetcher.mock.calls) {
    expect(init.credentials).toBe('same-origin');
    expect(init.signal).toBe(signal);
    expect(init.headers).not.toHaveProperty('Authorization');
  }
  expect(fetcher.mock.calls[1][1].headers).toHaveProperty('X-CSRF-Token', 'csrf-session');
  expect(fetcher.mock.calls[2][1].headers).toHaveProperty('X-CSRF-Token', 'csrf-session');
  expect(JSON.parse(fetcher.mock.calls[1][1].body as string)).toEqual({ name: 'Deploy', scopes: ['admin:read'], expires_at: '2027-01-01T00:00:00Z' });
});

test('requires explicit known scopes and a future expiry within exactly 365 days', () => {
  const now = Date.UTC(2026, 10, 4);
  const expiry = (days: number) => new Date(now + days * 86400_000).toISOString();
  expect(adminTokenIssue('Deploy', ['admin:read'], expiry(365), now)).toBeNull();
  expect(adminTokenIssue('Deploy', [], expiry(1), now)).toContain('invalidScopes');
  expect(adminTokenIssue('Deploy', ['admin:read', 'admin:read'], expiry(1), now)).toContain('invalidScopes');
  for (const value of ['', expiry(0), expiry(-1), expiry(366)]) expect(adminTokenIssue('Deploy', ['admin:read'], value, now)).toContain('invalidExpiry');
  expect(adminTokenIssue('🛰'.repeat(100), ['admin:read'], expiry(1), now)).toBeNull();
  expect(adminTokenIssue('a\0b', ['admin:read'], expiry(1), now)).toContain('invalidName');
});
