import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MetadataList, Retention } from './Settings';
import { Catalog } from './catalog';
import { management } from '../lib/manage-api';
import i18n from '../i18n';

const project = { id: 'p1', workspace_id: 'w1', slug: 'production', name: 'Production', created_at: '' };
const stream = {
  id: 's1',
  project_id: 'p1',
  name: 'players',
  type: 'logs',
  display_name: '',
  description: '',
  color: '',
  created_at: '',
};
type Call = { method: string; path: string; body: unknown };
function server(reply: (call: Call) => unknown) {
  const calls: Call[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (path: string, init?: RequestInit) => {
      const call = {
        method: init?.method ?? 'GET',
        path: path.replace('/api/v1', ''),
        body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
      };
      calls.push(call);
      return { status: 200, ok: true, headers: new Headers(), json: async () => reply(call) };
    }),
  );
  return calls;
}
beforeEach(async () => {
  await i18n.changeLanguage('en');
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

test('metadata edits preserve an unset color and enforce backend name and text limits', async () => {
  const calls = server((call) => (call.method === 'GET' ? { items: [stream] } : stream));
  render(<MetadataList project={project} kind="streams" writable />);
  const summary = await screen.findByText('players', { selector: 'summary' });
  await userEvent.click(summary);
  const details = summary.closest('details');
  if (!details) throw new Error('Missing metadata editor');
  const editor = within(details);
  await userEvent.type(editor.getByLabelText(i18n.t('manage.displayName')), 'Players online');
  expect(editor.getByLabelText(i18n.t('manage.displayName'))).toHaveAttribute('maxlength', '100');
  expect(editor.getByLabelText(i18n.t('manage.description'))).toHaveAttribute('maxlength', '500');
  await userEvent.click(editor.getByRole('button', { name: i18n.t('manage.save') }));
  await waitFor(() =>
    expect(calls.find((call) => call.method === 'PATCH')?.body).toEqual({
      display_name: 'Players online',
      description: '',
      color: '',
    }),
  );
  const name = screen.getByLabelText(i18n.t('manage.technicalName'));
  expect(name).toHaveAttribute('maxlength', '63');
  fireEvent.change(name, { target: { value: 'Uppercase' } });
  expect(name).toBeInvalid();
  fireEvent.change(name, { target: { value: 'economy.cash' } });
  expect(name).toBeValid();
});

test('workspace retention saves and resets through tenant routes without admin access', async () => {
  const own = { scope_type: 'workspace', scope_id: 'w1', local_age_hours: 24, global_age_hours: 720 };
  const calls = server((call) => (call.method === 'GET' ? { items: [own] } : own));
  render(<Retention scope="workspace" id="w1" writable />);
  const local = await screen.findByLabelText(i18n.t('manage.localHours'));
  fireEvent.change(local, { target: { value: '48' } });
  await userEvent.click(screen.getByRole('button', { name: i18n.t('manage.save') }));
  await waitFor(() =>
    expect(calls.find((call) => call.method === 'PUT')).toEqual({
      method: 'PUT',
      path: '/workspaces/w1/retention',
      body: { local_age_hours: 48, global_age_hours: 720 },
    }),
  );
  await userEvent.click(await screen.findByRole('button', { name: i18n.t('manage.inherit') }));
  await waitFor(() =>
    expect(calls.some((call) => call.method === 'DELETE' && call.path === '/workspaces/w1/retention')).toBe(true),
  );
  expect(calls.some((call) => call.path.startsWith('/admin'))).toBe(false);
});

test('catalog accepts multiple featured fields and sends only the strict create contract', async () => {
  const calls = server((call) => (call.method === 'GET' ? { items: [] } : {}));
  render(<Catalog project={project} writable />);
  await userEvent.click(screen.getByText(i18n.t('manage.createEvent'), { selector: 'summary' }));
  await userEvent.type(screen.getByLabelText(i18n.t('manage.event_name')), 'money.add');
  await userEvent.type(screen.getByLabelText(i18n.t('manage.featuredFields')), 'actor.name, amount');
  expect(screen.getByLabelText(i18n.t('manage.featuredFields'))).toHaveValue('actor.name, amount');
  const createSummary = screen.getByText(i18n.t('manage.createEvent'), { selector: 'summary' });
  const details = createSummary.closest('details');
  if (!details) throw new Error('Missing event editor');
  await userEvent.click(within(details).getByRole('button', { name: i18n.t('manage.save') }));
  await waitFor(() =>
    expect(calls.find((call) => call.method === 'POST' && call.path.endsWith('/event-types'))?.body).toEqual({
      name: 'money.add',
      title: '',
      template: '',
      icon: '',
      category: '',
      color: '',
      featured_fields: ['actor.name', 'amount'],
    }),
  );
});

test('administrator token plaintext is read from the one-time token key', async () => {
  const calls = server(() => ({ token: 'evl_adm_one-time', admin_token: { id: 'adm1', prefix: 'evl_adm_' } }));
  const response = await management.createAdminToken({
    name: 'Plugin',
    scopes: ['admin:read'],
    expires_at: '2027-01-01T00:00:00Z',
  });
  expect(response.token).toBe('evl_adm_one-time');
  expect(calls).toEqual([
    {
      method: 'POST',
      path: '/admin/tokens',
      body: { name: 'Plugin', scopes: ['admin:read'], expires_at: '2027-01-01T00:00:00Z' },
    },
  ]);
});

test('clearing both retention overrides restores inheritance rather than sending an invalid empty override', async () => {
  const own = { scope_type: 'project', scope_id: 'p1', local_age_hours: 24, global_age_hours: 720 };
  const calls = server((call) => call.method === 'GET' ? { items: [own] } : {});
  render(<Retention scope="project" id="p1" writable />);
  fireEvent.change(await screen.findByLabelText(i18n.t('manage.localHours')), { target: { value: '' } });
  fireEvent.change(screen.getByLabelText(i18n.t('manage.globalHours')), { target: { value: '' } });
  await userEvent.click(screen.getByRole('button', { name: i18n.t('manage.save') }));
  await waitFor(() => expect(calls.some((call) => call.method === 'DELETE' && call.path === '/projects/p1/retention')).toBe(true));
  expect(calls.some((call) => call.method === 'PUT')).toBe(false);
});
