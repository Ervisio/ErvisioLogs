import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom';
import type { ReactNode } from 'react';
import AdminOverview from './AdminOverview';
import { adminHealth, type AdminHealth } from '../lib/adminHealth';
import { ApiError } from '../lib/api';
import i18n from '../i18n';

const { self, retry } = vi.hoisted(() => ({ self: { id: 'admin', is_instance_admin: true }, retry: vi.fn() }));
vi.mock('../lib/auth', () => ({ useSession: () => ({ user: self }), useAuth: () => ({ retry }) }));
const health: AdminHealth = {
  status: 'degraded', wal: null,
  postgres: { ok: true, error: null }, clickhouse: { ok: false, error: 'private-driver-password' },
  upload: { waiting: { count: 2, bytes: 100 }, failed: { count: 1, bytes: 50 }, last_verified_at: null, oldest_unarchived_event_at: null },
  archive: { enabled: true, provider: { name: 'Vault', type: 'sftp', status: 'error', last_error: 'private-archive-password', last_checked_at: null } },
};
function Context() { return <Outlet context={{ topbar: (title: ReactNode) => <header>{title}</header> }} />; }
const element = <MemoryRouter><Routes><Route element={<Context />}><Route index element={<AdminOverview />} /></Route></Routes></MemoryRouter>;
const renderPage = () => render(element);

beforeEach(async () => { self.id = 'admin'; self.is_instance_admin = true; retry.mockClear(); await i18n.changeLanguage('en'); vi.spyOn(adminHealth, 'get').mockResolvedValue(health); });
afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); });

test('shows reported degradation and unavailable data without fabricating zeroes or exposing connection secrets', async () => {
  renderPage();
  await screen.findByText('Instance reports degraded operation');
  expect(screen.getByText('Check failed')).toBeInTheDocument();
  expect(screen.getByText('The instance did not return queue statistics.')).toBeInTheDocument();
  expect(screen.getByText('Queue size').parentElement).toHaveTextContent('Unavailable');
  expect(screen.getByText('Waiting uploads').parentElement).toHaveTextContent('2');
  expect(screen.getByText('Failed uploads').parentElement).toHaveTextContent('1');
  expect(screen.getByText('Vault · sftp')).toBeInTheDocument();
  expect(screen.queryByText(/private-driver-password|private-archive-password/)).not.toBeInTheDocument();
});

test('never fetches instance health for a non-admin', async () => {
  self.is_instance_admin = false;
  renderPage();
  expect(await screen.findByRole('alert')).toHaveTextContent('Only instance administrators');
  expect(adminHealth.get).not.toHaveBeenCalled();
});

test('polls, marks retained values stale after an error, and supports manual retry', async () => {
  vi.useFakeTimers();
  vi.mocked(adminHealth.get).mockResolvedValueOnce(health).mockRejectedValueOnce(new ApiError(503, 'private connection error', null)).mockResolvedValue({ ...health, status: 'ok', upload: { ...health.upload, waiting: { count: 7, bytes: 350 } } });
  const view = renderPage();
  await act(async () => { await Promise.resolve(); });
  expect(adminHealth.get).toHaveBeenCalledTimes(1);
  await act(async () => { vi.advanceTimersByTime(30000); await Promise.resolve(); });
  expect(adminHealth.get).toHaveBeenCalledTimes(2);
  expect(screen.getByText('Refresh failed. The values below are from the last successful response.')).toBeInTheDocument();
  expect(screen.getByText('Waiting uploads').parentElement).toHaveTextContent('2');
  expect(screen.queryByText('private connection error')).not.toBeInTheDocument();
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Refresh' })); await Promise.resolve(); });
  expect(adminHealth.get).toHaveBeenCalledTimes(3);
  expect(screen.getByText('Waiting uploads').parentElement).toHaveTextContent('7');
  const signal = vi.mocked(adminHealth.get).mock.calls.at(-1)![0]!;
  view.unmount();
  expect(signal.aborted).toBe(true);
  await act(async () => { vi.advanceTimersByTime(60000); });
  expect(adminHealth.get).toHaveBeenCalledTimes(3);
});

test('aborts on unmount and never consumes a late response in the next page', async () => {
  let finish: ((value: AdminHealth) => void) | undefined;
  vi.mocked(adminHealth.get).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; })).mockResolvedValue({ ...health, status: 'ok' });
  const first = renderPage();
  expect(screen.getByRole('status')).toHaveTextContent('Loading instance data');
  const signal = vi.mocked(adminHealth.get).mock.calls[0][0]!;
  first.unmount();
  expect(signal.aborted).toBe(true);
  renderPage();
  await screen.findByText('Instance reports normal operation');
  await act(async () => { finish?.(health); });
  expect(screen.queryByText('Instance reports degraded operation')).not.toBeInTheDocument();
});

test('requests an auth refresh on 401 and does not show an empty healthy dashboard', async () => {
  vi.mocked(adminHealth.get).mockRejectedValue(new ApiError(401, 'unauthorized', null));
  renderPage();
  await act(async () => { await Promise.resolve(); });
  expect(retry).toHaveBeenCalledOnce();
  expect(within(document.body).queryByText('Instance reports normal operation')).not.toBeInTheDocument();
});
