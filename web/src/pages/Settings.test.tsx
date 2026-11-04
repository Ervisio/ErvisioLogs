import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Outlet, Route, Routes, useParams } from 'react-router-dom';
import type { ShellContext } from '../components/Shell';
import { AuthProvider, RequireAuth } from '../lib/auth';
import { TenancyProvider } from '../lib/tenancy';
import { api, ApiError, type Session, type SettingEntry } from '../lib/api';
import i18n from '../i18n';
import Settings from './Settings';

const workspace = { id: 'w1', slug: 'team', name: 'Team', role: 'owner', created_at: '' };
const project = { id: 'p1', workspace_id: 'w1', name: 'Shop', slug: 'shop', created_at: '' };
const second = { ...project, id: 'p2', name: 'Second' };
const session: Session = { user: { id: 'u1', username: 'ada', email: 'a@example.com', email_verified: true, is_instance_admin: false, status: 'active', created_at: '' }, workspaces: [workspace], csrf_token: 'csrf' };
const maximum: SettingEntry = { key: 'ingest.max_streams', type: 'int', value: 60, inherited: 30, source: 'project', locked: false, min: 0, max: 10000 };
const automatic: SettingEntry = { key: 'ingest.auto_create_streams', type: 'bool', value: true, inherited: true, source: 'project', locked: false };

function Frame() {
  return <TenancyProvider><Outlet context={{ streams: [], environments: [], environment: null, setEnvironment: () => undefined, openMenu: () => undefined, topbar: (title) => <header>{title}</header> } satisfies ShellContext} /></TenancyProvider>;
}
function ProjectSettings() {
  const { projectId } = useParams();
  return <Settings scope="project" project={projectId === 'p2' ? second : project} />;
}
function mount(path = '/w/w1/p/p1/settings') {
  return render(<MemoryRouter initialEntries={[path]}><AuthProvider><Routes><Route element={<RequireAuth loading={<p>Loading</p>} unreachable={<p>Unreachable</p>} />}><Route element={<Frame />}><Route path="/w/:workspaceId/p/:projectId/settings" element={<ProjectSettings />} /><Route path="/w/:workspaceId/settings" element={<Settings scope="workspace" />} /><Route path="/admin/settings" element={<Settings scope="instance" />} /></Route></Route></Routes></AuthProvider></MemoryRouter>);
}
const save = () => screen.getByRole('button', { name: 'Save' });
const field = (key: string) => document.querySelector<HTMLElement>(`[data-setting="${key}"]`)!;

beforeEach(async () => {
  localStorage.clear();
  await i18n.changeLanguage('en');
  vi.spyOn(api, 'me').mockResolvedValue(session);
  vi.spyOn(api, 'projects').mockResolvedValue([project, second]);
  vi.spyOn(api, 'settings').mockResolvedValue([maximum, automatic]);
  vi.spyOn(api, 'putSetting');
  vi.spyOn(api, 'resetSetting');
});
afterEach(() => vi.restoreAllMocks());

test('validates a setting before writing any key', async () => {
  mount();
  const input = await screen.findByRole('spinbutton', { name: 'Maximum streams' });
  await userEvent.clear(input);
  await userEvent.type(input, '10001');
  await userEvent.click(save());
  expect(await screen.findByText('Enter a whole number from 0 to 10000.')).toBeInTheDocument();
  expect(api.putSetting).not.toHaveBeenCalled();
  expect(api.resetSetting).not.toHaveBeenCalled();
});

test('writes sequentially and retries only failed keys after a partial save', async () => {
  let finish: ((value: SettingEntry) => void) | undefined;
  vi.mocked(api.settings).mockResolvedValueOnce([maximum, automatic]).mockResolvedValueOnce([maximum, automatic]).mockResolvedValue([{ ...maximum, value: 50 }, { ...automatic, value: false }]);
  vi.mocked(api.putSetting).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; })).mockRejectedValueOnce(new ApiError(422, 'Cannot change stream creation', null)).mockResolvedValueOnce({ ...automatic, value: false });
  mount();
  const input = await screen.findByRole('spinbutton', { name: 'Maximum streams' });
  await userEvent.clear(input);
  await userEvent.type(input, '50');
  await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Create streams automatically' }), 'false');
  await userEvent.click(save());
  expect(api.putSetting).toHaveBeenCalledTimes(1);
  await act(async () => { finish?.({ ...maximum, value: 50 }); });
  await screen.findByText('Cannot change stream creation');
  expect(input).toHaveValue(50);
  expect(screen.getByRole('combobox', { name: 'Create streams automatically' })).toHaveValue('false');
  expect(screen.getByText('Saved 1 values; 1 values failed and remain unsaved.')).toBeInTheDocument();
  expect(api.putSetting).toHaveBeenCalledTimes(2);
  await userEvent.click(save());
  await waitFor(() => expect(save()).toBeDisabled());
  expect(api.putSetting).toHaveBeenCalledTimes(3);
  expect(vi.mocked(api.putSetting).mock.calls[2][1]).toBe('ingest.auto_create_streams');
});

test('inheritance deletes the override and uses the actual source returned by the server', async () => {
  const inherited: SettingEntry = { ...maximum, source: 'instance', value: 40, inherited: 40 };
  vi.mocked(api.resetSetting).mockResolvedValue(inherited);
  vi.mocked(api.settings).mockResolvedValueOnce([maximum]).mockResolvedValue([inherited]);
  mount();
  await screen.findByRole('spinbutton', { name: 'Maximum streams' });
  await userEvent.click(within(field(maximum.key)).getByRole('checkbox'));
  expect(screen.getByRole('spinbutton')).toBeDisabled();
  expect(screen.getByRole('spinbutton')).toHaveValue(30);
  await userEvent.click(save());
  await screen.findByText('Instance value');
  expect(screen.getByRole('spinbutton')).toHaveValue(40);
  expect(api.resetSetting).toHaveBeenCalledWith({ type: 'project', id: 'p1' }, maximum.key, expect.any(AbortSignal));
  expect(api.putSetting).not.toHaveBeenCalled();
});

test.each([['admin', '/w/w1/settings'], ['viewer', '/w/w1/p/p1/settings'], ['analyst', '/w/w1/p/p1/settings']])('role %s can read but cannot write at %s', async (role, path) => {
  vi.mocked(api.me).mockResolvedValue({ ...session, workspaces: [{ ...workspace, role }] });
  mount(path);
  expect(await screen.findByRole('spinbutton', { name: 'Maximum streams' })).toBeDisabled();
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
});

test('an instance administrator cannot write tenant settings without the workspace role', async () => {
  vi.mocked(api.me).mockResolvedValue({ ...session, user: { ...session.user, is_instance_admin: true }, workspaces: [{ ...workspace, role: 'viewer' }] });
  mount();
  expect(await screen.findByRole('spinbutton', { name: 'Maximum streams' })).toBeDisabled();
});

test.each(['admin', 'developer'])('project role %s can save settings', async (role) => {
  vi.mocked(api.me).mockResolvedValue({ ...session, workspaces: [{ ...workspace, role }] });
  vi.mocked(api.putSetting).mockResolvedValue({ ...maximum, value: 50 });
  mount();
  const input = await screen.findByRole('spinbutton', { name: 'Maximum streams' });
  await userEvent.clear(input);
  await userEvent.type(input, '50');
  await userEvent.click(save());
  await waitFor(() => expect(api.putSetting).toHaveBeenCalledWith({ type: 'project', id: 'p1' }, maximum.key, 50, expect.any(AbortSignal)));
});

test('environment overrides have no reset or editable control', async () => {
  vi.mocked(api.me).mockResolvedValue({ ...session, user: { ...session.user, is_instance_admin: true } });
  vi.mocked(api.settings).mockResolvedValue([{ key: 'auth.open_registration', type: 'bool', value: false, inherited: false, source: 'env', locked: true, env: 'ERVISIO_LOGS_OPEN_REGISTRATION' }]);
  mount('/admin/settings');
  expect(await screen.findByRole('combobox', { name: 'Open registration' })).toBeDisabled();
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Restore inherited value' })).not.toBeInTheDocument();
  expect(save()).toBeDisabled();
});

test('instance settings are not requested for a non-admin user', async () => {
  mount('/admin/settings');
  expect(await screen.findByRole('alert')).toHaveTextContent('You do not have permission');
  expect(api.settings).not.toHaveBeenCalled();
});

test('scope switches abort old loads and never show their later response', async () => {
  let finish: ((entries: SettingEntry[]) => void) | undefined;
  vi.mocked(api.settings).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; })).mockResolvedValue([{ ...maximum, value: 9 }]);
  mount();
  const scope = await screen.findByRole('combobox', { name: 'Scope' });
  await waitFor(() => expect(within(scope).getByRole('option', { name: 'Project · Second' })).toBeInTheDocument());
  const signal = vi.mocked(api.settings).mock.calls[0][1]!;
  await userEvent.selectOptions(scope, '/w/w1/p/p2/settings');
  await screen.findByRole('spinbutton', { name: 'Maximum streams' });
  expect(signal.aborted).toBe(true);
  await act(async () => { finish?.([maximum]); });
  expect(screen.getByRole('spinbutton')).toHaveValue(9);
});

test('leaving a scope aborts a pending save and does not start its remaining writes', async () => {
  let finish: ((entry: SettingEntry) => void) | undefined;
  vi.mocked(api.settings).mockImplementation(async (scope) => 'id' in scope && scope.id === 'p2' ? [{ ...maximum, value: 9 }] : [maximum, automatic]);
  vi.mocked(api.putSetting).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  mount();
  const input = await screen.findByRole('spinbutton', { name: 'Maximum streams' });
  await userEvent.clear(input);
  await userEvent.type(input, '50');
  await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Create streams automatically' }), 'false');
  await userEvent.click(save());
  const signal = vi.mocked(api.putSetting).mock.calls[0][3]!;
  await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Scope' }), '/w/w1/p/p2/settings');
  await waitFor(() => expect(screen.getByRole('spinbutton')).toHaveValue(9));
  expect(signal.aborted).toBe(true);
  await act(async () => { finish?.({ ...maximum, value: 50 }); });
  expect(api.putSetting).toHaveBeenCalledTimes(1);
  expect(screen.getByRole('spinbutton')).toHaveValue(9);
});

test('metadata validation and PATCH preserve immutable technical names and support clearing', async () => {
  const stream = { id: 's1', project_id: 'p1', name: 'economy', type: 'log', created_at: '', display_name: 'Economy', description: 'Money', color: '#ffb547' };
  const environment = { id: 'e1', project_id: 'p1', name: 'production', created_at: '', display_name: 'Production' };
  vi.spyOn(api, 'streams').mockResolvedValue([stream]);
  vi.spyOn(api, 'environments').mockResolvedValue([environment]);
  vi.spyOn(api, 'updateStream').mockResolvedValue({ ...stream, display_name: '', description: '', color: '' });
  vi.spyOn(api, 'updateEnvironment').mockResolvedValue({ ...environment, display_name: 'Live' });
  mount();
  await userEvent.click(await screen.findByRole('button', { name: 'Environments and streams' }));
  const name = await screen.findByRole('textbox', { name: /Technical name/ });
  expect(name).toHaveAttribute('readonly');
  await userEvent.clear(screen.getByRole('textbox', { name: 'Description' }));
  await userEvent.type(screen.getByRole('textbox', { name: 'Description' }), 'Draft money');
  await userEvent.click(screen.getByRole('button', { name: 'Log collection' }));
  await userEvent.click(screen.getByRole('button', { name: 'Environments and streams' }));
  expect(screen.getByRole('textbox', { name: 'Description' })).toHaveValue('Draft money');
  expect(api.streams).toHaveBeenCalledTimes(1);
  await userEvent.clear(screen.getByRole('textbox', { name: /Color/ }));
  await userEvent.type(screen.getByRole('textbox', { name: /Color/ }), 'red');
  await userEvent.click(save());
  expect(await screen.findByRole('alert')).toHaveTextContent('Use a color such as #ffb547');
  expect(api.updateStream).not.toHaveBeenCalled();
  await userEvent.clear(screen.getByRole('textbox', { name: 'Display name' }));
  await userEvent.clear(screen.getByRole('textbox', { name: 'Description' }));
  await userEvent.clear(screen.getByRole('textbox', { name: /Color/ }));
  await userEvent.click(save());
  await screen.findByText('Metadata saved.');
  expect(api.updateStream).toHaveBeenCalledWith('p1', 's1', { display_name: '', description: '', color: '' }, expect.any(AbortSignal));
  await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Edit' }), 'environment:e1');
  await userEvent.clear(screen.getByRole('textbox', { name: 'Display name' }));
  await userEvent.type(screen.getByRole('textbox', { name: 'Display name' }), 'Live');
  await userEvent.click(save());
  await waitFor(() => expect(api.updateEnvironment).toHaveBeenCalledWith('p1', 'e1', { display_name: 'Live', description: '', color: '' }, expect.any(AbortSignal)));
});
