import { account, captureEmailToken, clearEmailToken } from './account';
import { setCsrfToken } from './api';

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); for (const kind of ['reset', 'verification', 'invitation'] as const) clearEmailToken(kind); window.history.replaceState(null, '', '/'); });

test('posts email-link tokens only in request bodies and never includes them in API URLs', async () => {
  const fetcher = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({}) });
  vi.stubGlobal('fetch', fetcher);
  const token = 'private-fragment-secret';
  await account.resetPassword(token, 'a-new-password');
  await account.verifyEmail(token);
  await account.previewInvitation(token);
  await account.acceptInvitation(token);
  await account.registerInvitation(token, 'invited_user', 'a-new-password');
  expect(fetcher.mock.calls.map(([path]) => path)).toEqual(['/api/v1/auth/password/reset', '/api/v1/auth/email/verify', '/api/v1/invitations/preview', '/api/v1/invitations/accept', '/api/v1/invitations/register']);
  for (const [path, init] of fetcher.mock.calls) {
    expect(path).not.toContain(token);
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body).token).toBe(token);
  }
});

test('strips the fragment and retains invitation continuity only until it is consumed', () => {
  window.history.replaceState(null, '', '/invite#token=invitation-secret');
  expect(captureEmailToken('invitation', window.location.hash)).toBe('invitation-secret');
  expect(window.location.pathname).toBe('/invite');
  expect(window.location.hash).toBe('');
  expect(window.location.search).toBe('');
  expect(captureEmailToken('invitation', '')).toBe('invitation-secret');
  clearEmailToken('invitation');
  expect(captureEmailToken('invitation', '')).toBe('');
});

test('uses authenticated password and own-session endpoints with the shared CSRF transport', async () => {
  const fetcher = vi.fn().mockResolvedValue({ ok: true, status: 204 });
  vi.stubGlobal('fetch', fetcher); setCsrfToken('csrf-account');
  await account.changePassword('current-private', 'new-private-password');
  await account.revokeSession('session/1');
  await account.revokeOthers();
  await account.resendVerification();
  expect(fetcher.mock.calls.map(([path, init]) => [path, init.method])).toEqual([['/api/v1/auth/password', 'PUT'], ['/api/v1/auth/sessions/session%2F1', 'DELETE'], ['/api/v1/auth/sessions', 'DELETE'], ['/api/v1/auth/email/verification', 'POST']]);
  expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual({ current_password: 'current-private', new_password: 'new-private-password' });
  for (const [, init] of fetcher.mock.calls) { expect(init.headers['X-CSRF-Token']).toBe('csrf-account'); expect(init.credentials).toBe('same-origin'); }
});
