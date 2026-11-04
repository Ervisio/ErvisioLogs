import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import ForgotPassword from './ForgotPassword';
import ResetPassword from './ResetPassword';
import VerifyEmail from './VerifyEmail';
import Invitation from './Invitation';
import { account, captureEmailToken, clearEmailToken } from '../lib/account';
import { api, ApiError, type Session } from '../lib/api';
import i18n from '../i18n';

const { auth } = vi.hoisted(() => ({ auth: { state: { status: 'anonymous', session: undefined as Session | undefined }, establish: vi.fn(), logout: vi.fn().mockResolvedValue(undefined) } }));
vi.mock('../lib/auth', () => ({ useAuth: () => auth }));

beforeEach(async () => { await i18n.changeLanguage('en'); for (const kind of ['reset', 'verification', 'invitation'] as const) clearEmailToken(kind); auth.state = { status: 'anonymous', session: undefined }; window.history.replaceState(null, '', '/'); });
afterEach(() => { vi.restoreAllMocks(); auth.establish.mockClear(); });

test('forgot-password keeps the accepted response neutral and clears the email form', async () => {
  const user = userEvent.setup();
  const forgot = vi.spyOn(account, 'forgotPassword').mockResolvedValue(undefined);
  render(<MemoryRouter><ForgotPassword /></MemoryRouter>);
  await user.type(screen.getByRole('textbox', { name: 'Email' }), 'missing@example.com');
  await user.click(screen.getByRole('button', { name: 'Request reset link' }));
  expect(await screen.findByRole('status')).toHaveTextContent('If this address belongs to an active account');
  expect(forgot).toHaveBeenCalledWith('missing@example.com', expect.any(AbortSignal));
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
});

test('reset consumes the fragment token after success and removes password inputs', async () => {
  const user = userEvent.setup();
  const reset = vi.spyOn(account, 'resetPassword').mockResolvedValue(undefined);
  window.history.replaceState(null, '', '/reset-password#token=reset-secret');
  render(<MemoryRouter initialEntries={['/reset-password#token=reset-secret']}><ResetPassword /></MemoryRouter>);
  expect(window.location.hash).toBe('');
  await user.type(screen.getByLabelText('New password', { exact: false }), 'new-private-password');
  await user.click(screen.getByRole('button', { name: 'Set new password' }));
  expect(await screen.findByRole('status')).toHaveTextContent('Password reset.');
  expect(reset).toHaveBeenCalledWith('reset-secret', 'new-private-password', expect.any(AbortSignal));
  expect(captureEmailToken('reset', '')).toBe('');
  expect(screen.queryByLabelText('New password', { exact: false })).not.toBeInTheDocument();
});

test('expired verification links show a specific recoverable error without consuming a GET', async () => {
  const user = userEvent.setup();
  const verify = vi.spyOn(account, 'verifyEmail').mockRejectedValue(new ApiError(410, 'expired', null));
  render(<MemoryRouter initialEntries={['/verify-email#token=old-secret']}><VerifyEmail /></MemoryRouter>);
  expect(verify).not.toHaveBeenCalled();
  await user.click(screen.getByRole('button', { name: 'Verify email' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('invalid, used or expired');
});

test('invitation login links carry no token and keep continuity only inside the SPA', async () => {
  vi.spyOn(account, 'previewInvitation').mockResolvedValue({ workspace_name: 'Workspace', email: 'ada@example.com', role: 'viewer', expires_at: '2026-11-11T12:00:00Z', has_account: true });
  render(<MemoryRouter initialEntries={['/invite#token=invite-secret']}><Invitation /></MemoryRouter>);
  await screen.findByText('Workspace');
  const links = screen.getAllByRole('link', { name: 'Sign in' });
  const continuation = links.find(link => link.getAttribute('href')?.includes('next='));
  expect(continuation).toHaveAttribute('href', '/login?next=%2Faccept-invitation');
  expect(continuation?.getAttribute('href')).not.toContain('invite-secret');
  expect(captureEmailToken('invitation', '')).toBe('invite-secret');
});

test('an invited new account uses the invitation endpoint without public registration or provider checks', async () => {
  const user = userEvent.setup();
  vi.spyOn(account, 'previewInvitation').mockResolvedValue({ workspace_name: 'Workspace', email: 'new@example.com', role: 'viewer', expires_at: '2026-11-11T12:00:00Z', has_account: false });
  const session = { user: { id: 'new', email: 'new@example.com', username: 'new_user', email_verified: true, is_instance_admin: false, status: 'active', created_at: '' }, csrf_token: 'csrf-new', workspaces: [] };
  const register = vi.spyOn(account, 'registerInvitation').mockResolvedValue(session);
  render(<MemoryRouter initialEntries={['/invite#token=new-invite']}><Invitation /></MemoryRouter>);
  await user.type(await screen.findByRole('textbox', { name: /^Username/ }), 'new_user');
  await user.type(screen.getByLabelText('New password', { exact: false }), 'new-private-password');
  await user.click(screen.getByRole('button', { name: 'Create account and accept' }));
  await waitFor(() => expect(auth.establish).toHaveBeenCalledWith(session));
  expect(register).toHaveBeenCalledWith('new-invite', 'new_user', 'new-private-password', expect.any(AbortSignal));
  expect(captureEmailToken('invitation', '')).toBe('');
});

test('returns from token-free sign-in and accepts an existing user invitation held in SPA memory', async () => {
  const user = userEvent.setup();
  const session: Session = { user: { id: 'ada', email: 'ada@example.com', username: 'ada', email_verified: true, is_instance_admin: false, status: 'active', created_at: '' }, workspaces: [], csrf_token: 'csrf' };
  vi.spyOn(account, 'previewInvitation').mockResolvedValue({ workspace_name: 'Workspace', email: 'ada@example.com', role: 'viewer', expires_at: '2026-11-11T12:00:00Z', has_account: true });
  const accept = vi.spyOn(account, 'acceptInvitation').mockResolvedValue({ id: 'w1', slug: 'workspace', name: 'Workspace', role: 'viewer', created_at: '' });
  vi.spyOn(api, 'me').mockResolvedValue(session);
  function SignInStep() {
    const navigate = useNavigate();
    return <button onClick={() => { auth.state = { status: 'authenticated', session }; navigate('/accept-invitation'); }}>Complete sign-in</button>;
  }
  render(<MemoryRouter initialEntries={['/invite#token=existing-invite']}><Routes><Route path="/invite" element={<Invitation />} /><Route path="/login" element={<SignInStep />} /><Route path="/accept-invitation" element={<Invitation />} /><Route path="/w/w1/projects" element={<p>Workspace opened</p>} /></Routes></MemoryRouter>);
  await screen.findByText('Workspace');
  await user.click(screen.getAllByRole('link', { name: 'Sign in' })[0]);
  await user.click(screen.getByRole('button', { name: 'Complete sign-in' }));
  await user.click(await screen.findByRole('button', { name: 'Accept invitation' }));
  await screen.findByText('Workspace opened');
  expect(accept).toHaveBeenCalledWith('existing-invite', expect.any(AbortSignal));
  expect(captureEmailToken('invitation', '')).toBe('');
});
