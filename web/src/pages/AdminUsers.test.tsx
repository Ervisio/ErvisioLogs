import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom';
import AdminUsers from './AdminUsers';
import { adminUsers, type AdminUserSession } from '../lib/adminUsers';
import { ApiError, type User } from '../lib/api';
import i18n from '../i18n';

const { retry, self } = vi.hoisted(() => ({ retry: vi.fn(), self: { id: 'ada', email: 'ada@example.com', username: 'ada', email_verified: true, is_instance_admin: true, status: 'active', created_at: '2026-10-01T00:00:00Z' } }));
vi.mock('../lib/auth', () => ({ useAuth: () => ({ retry }), useSession: () => ({ user: self }) }));
const marco: User = { ...self, id: 'marco', email: 'marco@example.com', username: 'marco', is_instance_admin: false };
const nico: User = { ...marco, id: 'nico', email: 'nico@example.com', username: 'nico' };
const sessions: AdminUserSession[] = [{ id: 's1', created_at: '2026-11-04T10:00:00Z', expires_at: '2026-11-05T10:00:00Z', last_seen_at: null, user_agent: 'Browser A', ip: '192.0.2.1', current: false }];
function Context() { return <Outlet context={{ topbar: (title: React.ReactNode) => <header>{title}</header> }} />; }
const renderPage = () => render(<MemoryRouter><Routes><Route element={<Context />}><Route index element={<AdminUsers />} /></Route></Routes></MemoryRouter>);

beforeEach(async () => { await i18n.changeLanguage('en'); vi.spyOn(adminUsers, 'list').mockResolvedValue({ items: [self, marco, nico], next_cursor: null }); });
afterEach(() => { vi.restoreAllMocks(); retry.mockClear(); });

test('refreshes authentication after a 401 rather than treating it as an empty user list', async () => {
  vi.mocked(adminUsers.list).mockRejectedValue(new ApiError(401, 'unauthorized', null));
  renderPage();
  await waitFor(() => expect(retry).toHaveBeenCalledOnce());
  expect(screen.queryByText('No accounts match')).not.toBeInTheDocument();
});

test('blocks self account mutations and leaves membership access separate', async () => {
  const user = userEvent.setup();
  renderPage();
  await user.click(await screen.findByRole('button', { name: 'Manage ada' }));
  const panel = screen.getByRole('complementary', { name: 'User details' });
  for (const label of ['Disable account', 'Revoke instance admin', 'Reset password', 'Delete account']) expect(within(panel).getByRole('button', { name: label })).toBeDisabled();
  expect(screen.getByText(/Instance administration does not grant access/)).toBeInTheDocument();
});

test('requires typed deletion confirmation and displays the actual last-owner conflict without removing the user', async () => {
  const user = userEvent.setup();
  const detail = 'the user is the only owner of a workspace; make another member owner first';
  const remove = vi.spyOn(adminUsers, 'remove').mockRejectedValue(new ApiError(409, detail, null));
  renderPage();
  await user.click(await screen.findByRole('button', { name: 'Manage marco' }));
  await user.click(screen.getByRole('button', { name: 'Delete account' }));
  const dialog = screen.getByRole('dialog');
  await user.type(within(dialog).getByRole('textbox'), 'wrong');
  await user.click(within(dialog).getByRole('button', { name: 'Delete account' }));
  expect(remove).not.toHaveBeenCalled();
  await user.clear(within(dialog).getByRole('textbox'));
  await user.type(within(dialog).getByRole('textbox'), 'marco');
  await user.click(within(dialog).getByRole('button', { name: 'Delete account' }));
  expect(await within(dialog).findByRole('alert')).toHaveTextContent(detail);
  expect(remove).toHaveBeenCalledWith('marco', expect.any(AbortSignal));
  expect(screen.getByRole('button', { name: 'Manage marco' })).toBeInTheDocument();
});

test('ignores an aborted session response after changing the selected user and revokes the new user session', async () => {
  const user = userEvent.setup();
  let complete: (items: AdminUserSession[]) => void = () => {};
  const fetchSessions = vi.spyOn(adminUsers, 'sessions').mockImplementationOnce(() => new Promise(resolve => { complete = resolve; })).mockResolvedValue(sessions);
  const revoke = vi.spyOn(adminUsers, 'revokeSession').mockResolvedValue(undefined);
  renderPage();
  await user.click(await screen.findByRole('button', { name: 'Manage marco' }));
  await user.click(screen.getByRole('button', { name: 'View sessions' }));
  await user.click(screen.getByRole('button', { name: 'Close dialog' }));
  await user.click(screen.getByRole('button', { name: 'Manage nico' }));
  await user.click(screen.getByRole('button', { name: 'View sessions' }));
  await screen.findByText('Browser A');
  await act(async () => complete([{ ...sessions[0], user_agent: 'Stale browser' }]));
  expect(screen.queryByText('Stale browser')).not.toBeInTheDocument();
  expect(fetchSessions.mock.calls[0][1]?.aborted).toBe(true);
  await user.click(screen.getByRole('button', { name: 'Revoke' }));
  await waitFor(() => expect(revoke).toHaveBeenCalledWith('nico', 's1', expect.any(AbortSignal)));
  expect(await screen.findByText('No active sessions.')).toBeInTheDocument();
});

test('refreshes a changed admin role from the server without losing the current cursor page', async () => {
  const user = userEvent.setup();
  vi.mocked(adminUsers.list).mockResolvedValueOnce({ items: [self], next_cursor: 'page-two' }).mockResolvedValueOnce({ items: [marco], next_cursor: null });
  const update = vi.spyOn(adminUsers, 'update').mockResolvedValue(undefined);
  vi.spyOn(adminUsers, 'get').mockResolvedValue({ ...marco, is_instance_admin: true });
  renderPage();
  await screen.findByRole('button', { name: 'Manage ada' });
  await user.click(screen.getByRole('button', { name: 'Next' }));
  await user.click(await screen.findByRole('button', { name: 'Manage marco' }));
  await user.click(screen.getByRole('button', { name: 'Make instance admin' }));
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Confirm change' }));
  await screen.findByText('Instance administrator role updated.');
  expect(update).toHaveBeenCalledWith('marco', { is_instance_admin: true }, expect.any(AbortSignal));
  expect(screen.getByText('Page 2')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Manage marco' })).toHaveTextContent('Instance admin');
  expect(adminUsers.list).toHaveBeenLastCalledWith('', 'page-two', expect.any(AbortSignal));
});

test('clears a reset password when the successful action closes and does not show it in the inspector', async () => {
  const user = userEvent.setup();
  const reset = vi.spyOn(adminUsers, 'resetPassword').mockResolvedValue(undefined);
  renderPage();
  await user.click(await screen.findByRole('button', { name: 'Manage marco' }));
  await user.click(screen.getByRole('button', { name: 'Reset password' }));
  const password = 'private-phrase-42';
  await user.type(screen.getByLabelText('New password', { exact: false }), password);
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Reset password' }));
  await screen.findByText('Password reset and user sessions revoked.');
  expect(reset).toHaveBeenCalledWith('marco', password, expect.any(AbortSignal));
  expect(screen.queryByText(password)).not.toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: 'Reset password' }));
  expect(screen.getByLabelText('New password', { exact: false })).toHaveValue('');
});
