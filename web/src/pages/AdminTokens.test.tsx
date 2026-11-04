import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom';
import type { ReactNode } from 'react';
import AdminTokens from './AdminTokens';
import { adminTokens, type AdminToken } from '../lib/adminTokens';
import { ApiError } from '../lib/api';
import i18n from '../i18n';

const { self, retry } = vi.hoisted(() => ({ self: { id: 'admin', is_instance_admin: true }, retry: vi.fn() }));
vi.mock('../lib/auth', () => ({ useSession: () => ({ user: self }), useAuth: () => ({ retry }) }));
const token: AdminToken = { id: 't1', name: 'Deploy', prefix: 'evl_adm_fixture', scopes: ['admin:read'], created_by: 'creator', created_at: '2026-01-01T00:00:00Z', expires_at: new Date(Date.now() + 10 * 86400_000).toISOString(), last_used_at: null, revoked_at: null };
const secret = 'evl_adm_NEW_SECRET_ONCE';
function Context() { return <Outlet context={{ topbar: (title: ReactNode) => <header>{title}</header> }} />; }
const element = <MemoryRouter><Routes><Route element={<Context />}><Route index element={<AdminTokens />} /></Route></Routes></MemoryRouter>;
const renderPage = () => render(element);
function futureLocal() { const date = new Date(Date.now() + 86400_000); return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16); }
async function fillCreate(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Create admin token' }));
  const dialog = screen.getByRole('dialog');
  await user.type(within(dialog).getByRole('textbox', { name: 'Token name' }), 'Deploy');
  await user.click(within(dialog).getByRole('checkbox', { name: /Read administration/ }));
  fireEvent.change(within(dialog).getByLabelText(/^Expiry/), { target: { value: futureLocal() } });
  return dialog;
}

beforeEach(async () => {
  self.is_instance_admin = true; self.id = 'admin'; retry.mockClear(); localStorage.clear(); sessionStorage.clear(); await i18n.changeLanguage('en');
  vi.spyOn(adminTokens, 'list').mockResolvedValue([token]);
  vi.spyOn(adminTokens, 'create').mockResolvedValue({ token: secret, admin_token: token });
  vi.spyOn(adminTokens, 'revoke').mockResolvedValue(undefined);
});
afterEach(() => vi.restoreAllMocks());

test('lists only metadata and does not claim creator eligibility is known', async () => {
  renderPage();
  expect(await screen.findByText('evl_adm_fixture…')).toBeInTheDocument();
  expect(screen.getByText('Not revoked')).toBeInTheDocument();
  expect(screen.queryByText('Active')).not.toBeInTheDocument();
  expect(screen.queryByText(secret)).not.toBeInTheDocument();
  expect(screen.queryByLabelText('New admin token')).not.toBeInTheDocument();
});

test('non-admins cannot fetch, create or revoke admin tokens', async () => {
  self.is_instance_admin = false;
  renderPage();
  expect(await screen.findByRole('alert')).toHaveTextContent('Only instance administrators');
  expect(adminTokens.list).not.toHaveBeenCalled();
  expect(screen.queryByRole('button', { name: 'Create admin token' })).not.toBeInTheDocument();
  expect(adminTokens.create).not.toHaveBeenCalled();
});

test('validates before creating, reveals a masked one-time token, copies only on request, and clears on close', async () => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
  renderPage();
  await screen.findByText('Deploy');
  await user.click(screen.getByRole('button', { name: 'Create admin token' }));
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create admin token' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Enter a name');
  expect(adminTokens.create).not.toHaveBeenCalled();
  await user.click(screen.getByRole('button', { name: 'Close dialog' }));
  const dialog = await fillCreate(user);
  await user.click(within(dialog).getByRole('button', { name: 'Create admin token' }));
  const field = await screen.findByLabelText('New admin token');
  expect(field).toHaveAttribute('type', 'password');
  expect(field).toHaveValue(secret);
  expect(write).not.toHaveBeenCalled();
  await user.click(screen.getByRole('button', { name: 'Copy token' }));
  expect(write).toHaveBeenCalledWith(secret);
  await user.click(screen.getByRole('button', { name: 'Show token' }));
  expect(field).toHaveAttribute('type', 'text');
  expect(JSON.stringify(localStorage)).not.toContain(secret);
  expect(JSON.stringify(sessionStorage)).not.toContain(secret);
  await user.click(screen.getByRole('button', { name: 'Close and clear token' }));
  expect(screen.queryByLabelText('New admin token')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Show token' })).not.toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: 'Create admin token' }));
  expect(screen.queryByDisplayValue(secret)).not.toBeInTheDocument();
  expect(vi.mocked(adminTokens.create).mock.calls[0][0]).toMatchObject({ name: 'Deploy', scopes: ['admin:read'] });
});

test('revocation requires confirmation, preserves server conflicts, then hides the revoke action after success', async () => {
  const user = userEvent.setup();
  vi.mocked(adminTokens.revoke).mockRejectedValueOnce(new ApiError(409, 'Cannot revoke this token now', null)).mockResolvedValue(undefined);
  renderPage();
  await user.click(await screen.findByRole('button', { name: 'Revoke Deploy' }));
  const dialog = screen.getByRole('dialog');
  const confirm = within(dialog).getByRole('textbox');
  await user.type(confirm, 'wrong');
  expect(within(dialog).getByRole('button', { name: 'Revoke token' })).toBeDisabled();
  expect(adminTokens.revoke).not.toHaveBeenCalled();
  await user.clear(confirm); await user.type(confirm, 'Deploy');
  await user.click(within(dialog).getByRole('button', { name: 'Revoke token' }));
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Cannot revoke this token now');
  await user.click(within(dialog).getByRole('button', { name: 'Revoke token' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  expect(await screen.findByText('Revoked')).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Revoke Deploy' })).not.toBeInTheDocument();
  expect(adminTokens.revoke).toHaveBeenLastCalledWith('t1', expect.any(AbortSignal));
});

test('aborts a pending create when leaving and never reveals its late secret on another page', async () => {
  let finish: ((value: { token: string; admin_token: AdminToken }) => void) | undefined;
  vi.mocked(adminTokens.create).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const user = userEvent.setup();
  const first = renderPage();
  await screen.findByText('Deploy');
  const dialog = await fillCreate(user);
  await user.click(within(dialog).getByRole('button', { name: 'Create admin token' }));
  const signal = vi.mocked(adminTokens.create).mock.calls[0][1]!;
  first.unmount();
  expect(signal.aborted).toBe(true);
  renderPage(); await screen.findByText('Deploy');
  await act(async () => { finish?.({ token: secret, admin_token: token }); });
  expect(screen.queryByDisplayValue(secret)).not.toBeInTheDocument();
});

test('unauthorized token reads refresh authentication', async () => {
  vi.mocked(adminTokens.list).mockRejectedValue(new ApiError(401, 'unauthorized', null));
  renderPage();
  await waitFor(() => expect(retry).toHaveBeenCalledOnce());
  expect(screen.queryByText('No admin tokens have been created.')).not.toBeInTheDocument();
});
