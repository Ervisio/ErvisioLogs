import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import Invitation from '../pages/Invitation';
import i18n from '../i18n';
import { clearEmailToken } from './account';
import { AuthProvider, useAuth } from './auth';
import { api, ApiError, setCsrfToken, type Session } from './api';

const session: Session = {
  user: { id: 'new-user', email: 'new@example.com', username: 'new-user', email_verified: true, is_instance_admin: false, status: 'active', created_at: '' },
  workspaces: [], csrf_token: 'new-csrf',
};

function Consumer() {
  const { state, establish } = useAuth();
  return <><output>{state.status === 'authenticated' ? state.session.user.id : state.status}</output><button onClick={() => establish(session)}>Sign in</button></>;
}

afterEach(() => { vi.restoreAllMocks(); setCsrfToken(null); });

test('a cold invitation link waits for session bootstrap before its CSRF-protected preview', async () => {
  await i18n.changeLanguage('en');
  clearEmailToken('invitation');
  let resolve!: (value: Session) => void;
  vi.spyOn(api, 'me').mockReturnValue(new Promise<Session>(yes => { resolve = yes; }));
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
    workspace_name: 'Invited workspace', email: session.user.email, role: 'viewer', expires_at: '2026-12-01T12:00:00Z', has_account: true,
  }), { status: 200 }));
  render(<MemoryRouter initialEntries={['/invite#token=private-invite']}><AuthProvider><Invitation /></AuthProvider></MemoryRouter>);
  expect(fetch).not.toHaveBeenCalled();
  await act(async () => { resolve(session); });
  expect(await screen.findByText('Invited workspace')).toBeInTheDocument();
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(fetch.mock.calls[0][1]?.headers).toMatchObject({ 'X-CSRF-Token': 'new-csrf' });
  clearEmailToken('invitation');
});

test.each(['expired', 'old-user'])('a late bootstrap response (%s) cannot replace a new sign-in or its CSRF token', async (outcome) => {
  let resolve!: (value: Session) => void;
  let reject!: (reason: Error) => void;
  vi.spyOn(api, 'me').mockReturnValue(new Promise<Session>((yes, no) => { resolve = yes; reject = no; }));
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ id: 'w1' }), { status: 201 }));
  render(<AuthProvider><Consumer /></AuthProvider>);
  await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
  await act(async () => {
    if (outcome === 'expired') reject(new ApiError(401, 'expired', null));
    else resolve({ ...session, user: { ...session.user, id: 'old-user' }, csrf_token: 'old-csrf' });
  });
  expect(screen.getByText('new-user')).toBeInTheDocument();
  await api.createWorkspace('Example');
  expect(fetch.mock.calls.at(-1)?.[1]?.headers).toMatchObject({ 'X-CSRF-Token': 'new-csrf' });
});
