import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom';
import type { ReactNode } from 'react';
import { api, type ApiToken, type NotificationChannel, type Project, type Workspace } from '../lib/api';
import { notifications, futureExpiry } from '../lib/notifications-api';
import { management } from '../lib/manage-api';
import i18n from '../i18n';
import Alerts from './Alerts';
import Channels from './Channels';
import Users from './Users';
import Tokens from './Tokens';
import LogViews from '../components/LogViews';

const retryAuth = vi.hoisted(() => vi.fn());
vi.mock('../lib/auth', () => ({ useAuth: () => ({ retry: retryAuth }), useSession: () => ({ user: { id: 'me' } }) }));
vi.mock('../lib/tenancy', async (original) => ({ ...await original<typeof import('../lib/tenancy')>(), useTenancy: () => ({ workspace: { role: 'owner' } }) }));
const workspace: Workspace = { id: 'w1', slug: 'acme', name: 'Acme', role: 'owner', created_at: '' };
const project: Project = { id: 'p1', workspace_id: 'w1', slug: 'logs', name: 'Logs', created_at: '' };
const channel: NotificationChannel = { id: 'c1', workspace_id: 'w1', name: 'Operations', type: 'webhook', format: 'ervisio', enabled: true, url_hint: 'https://example.org', last_status: null, last_error: null, last_sent_at: null, created_at: '', updated_at: '' };
function Context() { return <Outlet context={{ topbar: (node: ReactNode) => <header>{node}</header>, streams: [] }} />; }
function mount(node: ReactNode) { return render(<MemoryRouter><Routes><Route element={<Context />}><Route index element={node} /></Route></Routes></MemoryRouter>); }
beforeEach(async () => { await i18n.changeLanguage('en'); vi.restoreAllMocks(); });

it('creates a rule only after three steps and preserves exact event filters', async () => {
  vi.spyOn(api, 'alertRules').mockResolvedValue([]);
  vi.spyOn(api, 'channels').mockResolvedValue([channel]);
  const create = vi.spyOn(api, 'createAlertRule').mockResolvedValue({ id: 'rule', workspace_id: 'w1', project_id: 'p1', name: 'Alert', query: {}, window_seconds: 300, threshold: 0, interval_seconds: 60, channel_ids: ['c1'], enabled: true, state: 'ok', last_value: null, last_error: null, last_evaluated_at: null, next_eval_at: '', created_at: '', updated_at: '' });
  mount(<Alerts workspace={workspace} project={project} />);
  await userEvent.click(await screen.findByRole('button', { name: i18n.t('alerts.new') }));
  await userEvent.click(screen.getByRole('button', { name: 'Failed payments' }));
  await userEvent.type(screen.getByLabelText(i18n.t('alerts.match')), 'tag:economy actor.id=7');
  await userEvent.click(screen.getByRole('button', { name: 'Next' }));
  expect(create).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole('button', { name: 'Next' }));
  expect(screen.getByRole('heading', { name: 'Rule summary' })).toBeInTheDocument();
  await userEvent.click(screen.getByRole('button', { name: i18n.t('alerts.create') }));
  await waitFor(() => expect(create).toHaveBeenCalledWith(expect.objectContaining({ query: expect.objectContaining({ events: ['payment.failed'], tags: ['economy'], filters: [{ field: 'actor.id', op: 'eq', value: '7' }] }), channel_ids: ['c1'] })));
});

it('creates email delivery without webhook fields and clears destination after success', async () => {
  vi.spyOn(notifications, 'channels').mockResolvedValue([]);
  const create = vi.spyOn(notifications, 'createChannel').mockResolvedValue({ ...channel, type: 'email' });
  mount(<Channels workspace={workspace} />);
  await userEvent.type(await screen.findByLabelText('Name'), 'Email alerts');
  await userEvent.selectOptions(screen.getByLabelText('Delivery method'), 'email');
  expect(screen.queryByLabelText('Webhook URL')).not.toBeInTheDocument();
  await userEvent.type(screen.getByLabelText('Recipient email'), 'ops@example.org');
  await userEvent.click(screen.getByRole('button', { name: i18n.t('channels.create') }));
  await waitFor(() => expect(create).toHaveBeenCalledWith({ workspace_id: 'w1', name: 'Email alerts', type: 'email', recipient: 'ops@example.org' }));
  await waitFor(() => expect(screen.getByLabelText('Name')).toHaveValue(''));
});

it('shows channel delivery failures without requesting histories before expansion', async () => {
  vi.spyOn(notifications, 'channels').mockResolvedValue([{ ...channel, type: 'telegram' }]);
  const deliveries = vi.spyOn(notifications, 'deliveries').mockResolvedValue([{ id: 'd1', status: 'error', error: 'Delivery rejected', created_at: '2026-11-06T12:00:00Z' }]);
  mount(<Channels workspace={workspace} />);
  const summary = await screen.findByText('Delivery history');
  expect(deliveries).not.toHaveBeenCalled();
  await userEvent.click(summary);
  expect(await screen.findByText('Delivery rejected')).toBeInTheDocument();
});

it('invites new users while retaining direct membership controls', async () => {
  vi.spyOn(api, 'members').mockResolvedValue([]);
  vi.spyOn(management, 'invitations').mockResolvedValue([]);
  const invite = vi.spyOn(management, 'invite').mockResolvedValue({ id: 'i1', email: 'new@example.org', role: 'viewer', expires_at: '', emailed: false, url: 'https://example.org/invitation#token=secret' });
  mount(<Users workspace={workspace} />);
  const section = (await screen.findByRole('heading', { name: 'Invitations' })).closest('section');
  if (!section) throw new Error('Missing invitations');
  await userEvent.type(within(section).getByLabelText(i18n.t('users.email')), 'new@example.org');
  await userEvent.click(within(section).getByRole('button', { name: 'Send invitation' }));
  await waitFor(() => expect(invite).toHaveBeenCalledWith('w1', 'new@example.org', 'viewer'));
  expect(await screen.findByText('https://example.org/invitation#token=secret')).toBeInTheDocument();
  expect(screen.getByRole('heading', { name: i18n.t('users.add') })).toBeInTheDocument();
});

it('rotates a token and exposes the replacement once', async () => {
  const token: ApiToken = { id: 't1', project_id: 'p1', name: 'Game server', prefix: 'evl_old', scopes: ['logs:ingest'], expires_at: null, revoked_at: null, last_used_at: null, created_at: '2026-11-05T00:00:00Z' };
  vi.spyOn(api, 'tokens').mockResolvedValue([token]);
  const rotate = vi.spyOn(notifications, 'rotateToken').mockResolvedValue({ token: 'replacement-secret', api_token: token });
  vi.spyOn(window, 'confirm').mockReturnValue(true);
  mount(<Tokens project={project} />);
  await userEvent.click(await screen.findByRole('button', { name: 'Rotate' }));
  await waitFor(() => expect(rotate).toHaveBeenCalledWith('t1'));
  expect(await screen.findByText('replacement-secret')).toBeInTheDocument();
});

it('updates a selected saved query with the current tags and filters', async () => {
  const view = { id: 'v1', workspace_id: 'w1', project_id: 'p1', name: 'Economy', query: {}, created_by: 'me', created_at: '', updated_at: '' };
  vi.spyOn(api, 'savedQueries').mockResolvedValue([view]);
  const update = vi.spyOn(notifications, 'updateSavedQuery').mockResolvedValue(view);
  const current = { tags: ['economy'], filters: [{ field: 'actor.id', op: 'eq', value: '7' }] };
  mount(<LogViews project={project} current={current} onApply={vi.fn()} exportRequest={(format) => ({ project_id: 'p1', format })} />);
  await userEvent.selectOptions(await screen.findByLabelText(i18n.t('views.label')), 'v1');
  await userEvent.click(screen.getByRole('button', { name: 'Update view' }));
  await waitFor(() => expect(update).toHaveBeenCalledWith(view, current));
});

it('validates expiration against the local clock', () => {
  expect(futureExpiry('2026-11-07T12:00', new Date('2026-11-06').getTime())).toMatch(/^2026-11-07T/);
  expect(futureExpiry('2026-11-05T12:00', new Date('2026-11-06').getTime())).toBeNull();
  expect(futureExpiry('invalid')).toBeNull();
});

it('refuses an expired date before creating a token', async () => {
  vi.spyOn(api, 'tokens').mockResolvedValue([]);
  const create = vi.spyOn(api, 'createToken');
  mount(<Tokens project={project} />);
  await userEvent.type(await screen.findByLabelText(i18n.t('tokens.name')), 'Worker');
  fireEvent.change(screen.getByLabelText('Expiration (optional)', { exact: false }), { target: { value: '2020-01-01T12:00' } });
  await userEvent.click(screen.getByRole('button', { name: i18n.t('tokens.create') }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Choose a future expiration date.');
  expect(create).not.toHaveBeenCalled();
});
