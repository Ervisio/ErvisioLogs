import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom';
import Account from './Account';
import { account } from '../lib/account';
import i18n from '../i18n';

const { retry, session } = vi.hoisted(() => ({ retry: vi.fn(), session: { user: { id: 'self', email: 'ada@example.com', username: 'ada', email_verified: false, is_instance_admin: true, status: 'active', created_at: '2026-10-01T12:00:00Z' } } }));
vi.mock('../lib/auth', () => ({ useAuth: () => ({ retry }), useSession: () => session }));
function Context() { return <Outlet context={{ topbar: (title: React.ReactNode) => <header>{title}</header> }} />; }
const renderPage = () => render(<MemoryRouter><Routes><Route element={<Context />}><Route index element={<Account />} /></Route></Routes></MemoryRouter>);
beforeEach(async () => { await i18n.changeLanguage('en'); });
afterEach(() => { vi.restoreAllMocks(); retry.mockClear(); });

test('changing a password requires the current password, clears both inputs and keeps authentication active', async () => {
  const user = userEvent.setup();
  const change = vi.spyOn(account, 'changePassword').mockResolvedValue(undefined);
  renderPage();
  await user.click(screen.getByRole('button', { name: 'Password' }));
  await user.type(screen.getByLabelText('Current password'), 'current-private');
  await user.type(screen.getByLabelText('New password', { exact: false }), 'next-private-password');
  await user.click(screen.getByRole('button', { name: 'Change password' }));
  expect(await screen.findByRole('status')).toHaveTextContent('Your current session remains active');
  expect(change).toHaveBeenCalledWith('current-private', 'next-private-password', expect.any(AbortSignal));
  expect(screen.getByLabelText('Current password')).toHaveValue('');
  expect(screen.getByLabelText('New password', { exact: false })).toHaveValue('');
  expect(retry).not.toHaveBeenCalled();
});

test('revoking others keeps the current session and revoking the current one refreshes authentication', async () => {
  const user = userEvent.setup();
  const current = { id: 'current', current: true, created_at: '2026-11-04T12:00:00Z', expires_at: '2026-11-05T12:00:00Z', last_seen_at: null, ip: '192.0.2.1', user_agent: 'Current browser' };
  vi.spyOn(account, 'sessions').mockResolvedValue([current, { ...current, id: 'other', current: false, user_agent: 'Other browser' }]);
  const revokeOthers = vi.spyOn(account, 'revokeOthers').mockResolvedValue({ revoked: 1 });
  const revokeCurrent = vi.spyOn(account, 'revokeSession').mockResolvedValue(undefined);
  renderPage();
  await user.click(screen.getByRole('button', { name: 'Sessions' }));
  await screen.findByText('Other browser');
  await user.click(screen.getByRole('button', { name: 'Revoke other sessions' }));
  await waitFor(() => expect(screen.queryByText('Other browser')).not.toBeInTheDocument());
  expect(screen.getByText('Current browser')).toBeInTheDocument();
  expect(revokeOthers).toHaveBeenCalledWith(expect.any(AbortSignal));
  await user.click(screen.getByRole('button', { name: 'Revoke current session' }));
  await waitFor(() => expect(retry).toHaveBeenCalledOnce());
  expect(revokeCurrent).toHaveBeenCalledWith('current', expect.any(AbortSignal));
});

test('resending verification does not claim the email is verified', async () => {
  const user = userEvent.setup();
  const resend = vi.spyOn(account, 'resendVerification').mockResolvedValue(undefined);
  renderPage();
  await user.click(screen.getByRole('button', { name: 'Send verification email' }));
  expect(await screen.findByRole('status')).toHaveTextContent('Verification email requested');
  expect(screen.getByText('Email not verified')).toBeInTheDocument();
  expect(resend).toHaveBeenCalledWith(expect.any(AbortSignal));
});
