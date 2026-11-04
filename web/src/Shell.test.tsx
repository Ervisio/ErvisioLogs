import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useLocation } from 'react-router-dom';
import App from './App';
import i18n from './i18n';
import { api, ApiError, type Project, type Session } from './lib/api';

const w1 = { id: 'w1', slug: 'acme', name: 'Acme', role: 'owner', created_at: '' };
const w2 = { ...w1, id: 'w2', name: 'Beta', role: 'viewer' };
const p1 = { id: 'p1', workspace_id: 'w1', slug: 'shop', name: 'Shop', created_at: '' };
const p2 = { ...p1, id: 'p2', workspace_id: 'w2', name: 'Other' };
const session: Session = {
  user: { id: 'u1', username: 'ada', email: 'ada@example.com', email_verified: true, is_instance_admin: false, status: 'active', created_at: '' },
  workspaces: [w1, w2], csrf_token: 'csrf',
};

function LocationProbe() {
  const location = useLocation();
  return <output aria-label="Current route">{location.pathname + location.search + location.hash}</output>;
}

function mount(path: string) {
  return render(<MemoryRouter initialEntries={[path]}><App /><LocationProbe /></MemoryRouter>);
}

beforeEach(async () => {
  localStorage.clear();
  await i18n.changeLanguage('en');
  vi.spyOn(api, 'me').mockResolvedValue(session);
  vi.spyOn(api, 'projects').mockImplementation(async (id) => id === 'w1' ? [p1] : [p2]);
  vi.spyOn(api, 'streams').mockResolvedValue([]);
  vi.spyOn(api, 'environments').mockResolvedValue([{ id: 'e1', project_id: 'p1', name: 'production', created_at: '' }]);
  vi.spyOn(api, 'searchLogs').mockResolvedValue({ items: [], next_cursor: null, total: 0, level_counts: {}, searched_tiers: ['hot'], partial: false });
  vi.spyOn(api, 'savedQueries').mockResolvedValue([]);
});

afterEach(() => vi.restoreAllMocks());

test('a failed scoped workspace keeps its workspace on the retry page', async () => {
  localStorage.setItem('workspace', 'w1');
  vi.mocked(api.projects).mockRejectedValue(new Error('unreachable'));
  mount('/w/w2');
  expect(await screen.findByText(i18n.t('projects.loadFailed'))).toBeInTheDocument();
  expect(screen.getByLabelText('Current route')).toHaveTextContent('/w/w2/projects');
  expect(screen.getByRole('combobox', { name: 'Workspace' })).toHaveValue('w2');
  expect(vi.mocked(api.projects).mock.calls.every(([id]) => id === 'w2')).toBe(true);
});

test('the URL chooses the workspace even when another workspace is saved', async () => {
  localStorage.setItem('workspace', 'w1');
  mount('/w/w2/p/p2');
  expect(await screen.findByRole('heading', { name: 'Send the first event to Other' })).toBeInTheDocument();
  expect(screen.getByRole('combobox', { name: 'Workspace' })).toHaveValue('w2');
  expect(screen.getByText('Viewer')).toBeInTheDocument();
  expect(api.projects).toHaveBeenCalledWith('w2', expect.any(AbortSignal));
  expect(api.projects).not.toHaveBeenCalledWith('w1', expect.anything());
});

test('a project in a different workspace never makes a scoped URL switch tenants', async () => {
  mount('/w/w2/p/p1');
  expect(await screen.findByRole('heading', { name: 'Page not found', level: 1 })).toBeInTheDocument();
  expect(screen.getByLabelText('Current route')).toHaveTextContent('/w/w2/p/p1');
  expect(api.projects).not.toHaveBeenCalledWith('w1', expect.anything());
  expect(api.searchLogs).not.toHaveBeenCalled();
  expect(api.environments).not.toHaveBeenCalled();
});

test('an unknown workspace has no fallback project or data request', async () => {
  mount('/w/missing/p/p1');
  expect(await screen.findByRole('heading', { name: 'Page not found', level: 1 })).toBeInTheDocument();
  expect(api.projects).not.toHaveBeenCalled();
  expect(api.searchLogs).not.toHaveBeenCalled();
  expect(api.streams).not.toHaveBeenCalled();
});

test('legacy deep links find the workspace and retain their query and fragment', async () => {
  mount('/projects/p2/streams/s2?source=bookmark#event');
  await waitFor(() => expect(screen.getByLabelText('Current route')).toHaveTextContent('/w/w2/p/p2/streams/s2?source=bookmark#event'));
  expect(screen.getByRole('combobox', { name: 'Workspace' })).toHaveValue('w2');
});

test('a 401 during legacy lookup refreshes authentication', async () => {
  vi.mocked(api.me).mockResolvedValueOnce(session).mockRejectedValue(new ApiError(401, 'signed out', null));
  vi.mocked(api.projects).mockImplementation(async (id) => {
    if (id === 'w2') throw new ApiError(401, 'signed out', null);
    return [p1];
  });
  vi.spyOn(api, 'providers').mockResolvedValue({ local: true, providers: [], open_registration: false, setup_required: false });
  mount('/projects/p2');
  expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
  expect(api.me).toHaveBeenCalledTimes(2);
});

test('workspace changes discard a response that arrives after switching back', async () => {
  let finish: ((projects: Project[]) => void) | undefined;
  vi.mocked(api.projects).mockImplementation(async (id) => id === 'w1' ? [p1] : new Promise<Project[]>((resolve) => { finish = resolve; }));
  mount('/w/w1/p/p1');
  await screen.findByRole('heading', { name: 'Send the first event to Shop' });
  fireEvent.change(screen.getByRole('combobox', { name: 'Workspace' }), { target: { value: 'w2' } });
  await waitFor(() => expect(api.projects).toHaveBeenCalledWith('w2', expect.any(AbortSignal)));
  expect(screen.queryByRole('link', { name: 'Shop' })).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole('combobox', { name: 'Workspace' }), { target: { value: 'w1' } });
  await screen.findAllByRole('link', { name: 'Shop' });
  await act(async () => { finish?.([p2]); });
  expect(screen.getByLabelText('Current route')).toHaveTextContent('/w/w1/projects');
  expect(screen.queryByRole('link', { name: 'Other' })).not.toBeInTheDocument();
  expect(screen.getAllByRole('link', { name: 'Shop' }).length).toBeGreaterThan(0);
});

test('mobile navigation traps focus, closes with Escape and backdrop, and restores focus', async () => {
  mount('/w/w1/p/p1');
  await screen.findByRole('heading', { name: 'Send the first event to Shop' });
  const trigger = screen.getByRole('button', { name: i18n.t('nav.menu') });
  await userEvent.click(trigger);
  const drawer = screen.getByRole('dialog', { name: i18n.t('nav.primary') });
  const brand = within(drawer).getByRole('link', { name: 'Ervisio Logs' });
  expect(brand).toHaveFocus();
  await userEvent.tab({ shift: true });
  expect(within(drawer).getByRole('button', { name: 'Account' })).toHaveFocus();
  await userEvent.tab();
  expect(brand).toHaveFocus();
  await userEvent.keyboard('{Escape}');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(trigger).toHaveFocus();
  await userEvent.click(trigger);
  fireEvent.click(screen.getByRole('dialog').previousElementSibling!);
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(trigger).toHaveFocus();
});

test('the environment control still scopes the explorer request', async () => {
  mount('/w/w1/p/p1');
  await screen.findByRole('heading', { name: 'Send the first event to Shop' });
  await userEvent.selectOptions(screen.getByRole('combobox', { name: i18n.t('nav.environment') }), 'e1');
  await waitFor(() => expect(api.searchLogs).toHaveBeenCalledWith(expect.objectContaining({ project_id: 'p1', environments: ['production'] })));
});

test('instance storage works without a project and stays absent for non-admins', async () => {
  vi.spyOn(api, 'storage').mockResolvedValue({ providers: [], segments: {}, archiver_enabled: false });
  vi.spyOn(api, 'retention').mockResolvedValue([]);
  vi.mocked(api.me).mockResolvedValue({ ...session, user: { ...session.user, is_instance_admin: true }, workspaces: [] });
  const first = mount('/admin/storage');
  expect(await screen.findByRole('heading', { name: 'Storage', level: 1 })).toBeInTheDocument();
  await waitFor(() => expect(api.storage).toHaveBeenCalled());
  expect(api.projects).not.toHaveBeenCalled();
  first.unmount();
  vi.mocked(api.me).mockResolvedValue(session);
  mount('/w/w1/p/p1');
  await screen.findByRole('heading', { name: 'Send the first event to Shop' });
  expect(screen.queryByRole('link', { name: 'Storage' })).not.toBeInTheDocument();
});


test.each([
  ['viewer', false],
  ['analyst', false],
  ['developer', true],
  ['admin', true],
  ['owner', true],
])('token navigation and palette reflect the %s role', async (role, allowed) => {
  vi.mocked(api.me).mockResolvedValue({ ...session, workspaces: [{ ...w1, role }] });
  mount('/w/w1/p/p1');
  await screen.findByRole('heading', { name: 'Send the first event to Shop' });
  await userEvent.click(screen.getByText(i18n.t('shell.projectSettings')));
  const configuration = screen.getAllByRole('link', { name: i18n.t('shell.configuration') });
  expect(configuration.some(link => link.getAttribute('href') === '/w/w1/p/p1/settings')).toBe(true);
  expect(!!screen.queryByRole('link', { name: i18n.t('nav.tokens') })).toBe(allowed);
  await userEvent.keyboard('{Control>}k{/Control}');
  const palette = screen.getByRole('dialog', { name: i18n.t('palette.title') });
  expect(!!within(palette).queryByRole('option', { name: i18n.t('nav.tokens') })).toBe(allowed);
  expect(within(palette).getByRole('option', { name: i18n.t('nav.alerts') })).toBeInTheDocument();
  expect(within(palette).getByRole('option', { name: i18n.t('nav.users') })).toBeInTheDocument();
  expect(within(palette).getByRole('option', { name: i18n.t('shell.channels') })).toBeInTheDocument();
});
