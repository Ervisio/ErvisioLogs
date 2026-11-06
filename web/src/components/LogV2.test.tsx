import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom';
import type { ReactNode } from 'react';
import { api, type LogItem, type Project, type SearchRequest, type SearchResult } from '../lib/api';
import { management, type EventType, type FieldRule } from '../lib/manage-api';
import i18n from '../i18n';
import Logs from '../pages/Logs';
import { EventDetail, eventMessage } from './LogEvent';
import { Histogram } from './LogFilters';

const retryAuth = vi.hoisted(() => vi.fn());
vi.mock('../lib/auth', () => ({ useAuth: () => ({ retry: retryAuth }) }));
vi.mock('../lib/tenancy', async (original) => ({ ...await original<typeof import('../lib/tenancy')>(), useTenancy: () => ({ workspace: { role: 'owner' } }) }));
const project: Project = { id: 'p1', workspace_id: 'w1', slug: 'server', name: 'Server', created_at: '' };
const eventType: EventType = { name: 'trade.completed', title: 'Trade completed', template: '{actor.name} transferred {amount}', icon: 'wallet', category: 'economy', color: '#3b82f6', featured_fields: ['amount'], discovered: false };
const hiddenName: FieldRule = { field: 'actor.name', label: 'Player name', hidden: true, format: 'text', redact: false };
const buckets = [0, 1, 2].map((index) => ({ timestamp: `2026-11-06T12:0${index}:00.000Z`, count: index + 1 }));
const histogramCalls: unknown[] = [];
function event(index = 0, patch: Partial<LogItem> = {}): LogItem {
  return { id: `event-${index}`, timestamp: new Date(Date.now() - index * 1000).toISOString(), ingested_at: new Date().toISOString(), level: 'INFO', severity_number: 9, event: `event.${index}`, message: `Event message ${index}`, stream: 'economy', environment: 'production', service: 'bank', host: 'server-1', resource: 'money', trace_id: '0123456789abcdef', span_id: 'abcdef', attributes: { actor: { type: 'player', id: 7, name: 'Private player name' }, amount: 42 }, tags: ['economy'], tier: 'hot', ...patch };
}
function result(items: LogItem[], next: string | null = null, total = items.length): SearchResult { return { items, next_cursor: next, total, level_counts: { INFO: total }, searched_tiers: ['hot', 'archive'], partial: false }; }
class Source extends EventTarget {
  static CLOSED = 2;
  static instances: Source[] = [];
  readonly url: string;
  readyState = 1;
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  closed = false;
  constructor(url: string) { super(); this.url = url; Source.instances.push(this); }
  close() { this.closed = true; this.readyState = Source.CLOSED; }
}
function Context() { return <Outlet context={{ topbar: (node: ReactNode) => <header>{node}</header>, streams: [], environments: [], environment: null, setEnvironment: vi.fn() }} />; }
function mountLogs(live = false) { return render(<MemoryRouter><Routes><Route element={<Context />}><Route index element={<Logs project={project} live={live} />} /></Route></Routes></MemoryRouter>); }
function setupSearch(items: LogItem[] = [event()]) {
  vi.spyOn(api, 'searchLogs').mockResolvedValue(result(items));
  vi.spyOn(api, 'savedQueries').mockResolvedValue([]);
  vi.spyOn(management, 'eventTypes').mockResolvedValue([eventType]);
  vi.spyOn(management, 'fieldRules').mockResolvedValue([]);
}
beforeEach(async () => {
  vi.restoreAllMocks(); Source.instances = []; histogramCalls.length = 0; sessionStorage.clear();
  await i18n.changeLanguage('en');
  vi.stubGlobal('EventSource', Source);
  vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
    if (typeof init?.body === 'string') histogramCalls.push(JSON.parse(init.body));
    return new Response(JSON.stringify({ items: buckets, partial: false, searched_tiers: ['hot'] }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  }));
});
afterEach(() => vi.unstubAllGlobals());

it('sends the same attribute and tag filters to search and Live', async () => {
  setupSearch(); mountLogs();
  await screen.findByText('Event message 0');
  await userEvent.type(screen.getByLabelText('Search'), 'tag:economy actor.id=7 transfer{Enter}');
  await waitFor(() => expect(api.searchLogs).toHaveBeenCalledWith(expect.objectContaining({ query: 'transfer', tags: ['economy'], filters: [{ field: 'actor.id', op: 'eq', value: '7' }] }), expect.anything()));
  await userEvent.click(screen.getByRole('button', { name: 'Go live' }));
  await waitFor(() => expect(Source.instances.length).toBeGreaterThan(0));
  const source = Source.instances[Source.instances.length - 1];
  if (!source) throw new Error('Missing live connection');
  const params = new URL(source.url, 'http://localhost').searchParams;
  expect(params.get('query')).toBe('transfer'); expect(params.get('tags')).toBe('economy');
  expect(JSON.parse(params.get('filters') ?? 'null')).toEqual([{ field: 'actor.id', op: 'eq', value: '7' }]);
});

it('keeps 250 loaded events outside the DOM until their scroll window is visible', async () => {
  const events = Array.from({ length: 250 }, (_, index) => event(index));
  setupSearch();
  vi.mocked(api.searchLogs).mockImplementation(async (request: SearchRequest) => {
    const offset = request.cursor ? Number(request.cursor) : 0;
    return result(events.slice(offset, offset + 100), offset + 100 < events.length ? String(offset + 100) : null, 250);
  });
  mountLogs();
  await userEvent.click(await screen.findByRole('button', { name: 'Load more' }));
  await screen.findByText('Showing 200 of 250');
  await userEvent.click(screen.getByRole('button', { name: 'Load more' }));
  await screen.findByText('Showing 250 of 250');
  expect(screen.getAllByRole('row').length).toBeLessThan(60);
  expect(screen.queryByText('Event message 210')).not.toBeInTheDocument();
  const viewport = screen.getByRole('table').parentElement;
  if (!viewport) throw new Error('Missing scroll viewport');
  fireEvent.scroll(viewport, { target: { scrollTop: 42 * 200 } });
  expect(await screen.findByText('Event message 210')).toBeInTheDocument();
  expect(screen.getAllByRole('row').length).toBeLessThan(60);
  expect(screen.queryByText('Event message 0')).not.toBeInTheDocument();
});

it('marks only rows that actually came from the archive', async () => {
  setupSearch([event(0, { message: 'Archived operation', tier: 'archive' }), event(1, { message: 'Hot operation', tier: 'hot' })]);
  mountLogs();
  const archived = (await screen.findByText('Archived operation')).closest('tr');
  const hot = screen.getByText('Hot operation').closest('tr');
  if (!archived || !hot) throw new Error('Missing event rows');
  expect(within(archived).getByText(i18n.t('explorer.tier.archive'))).toBeInTheDocument();
  expect(within(hot).queryByText(i18n.t('explorer.tier.archive'))).not.toBeInTheDocument();
});

it('does not let a cancelled slow search replace a newer completed search', async () => {
  setupSearch();
  let resolveOld: (value: SearchResult) => void = () => { throw new Error('Old search not initialized'); };
  const old = new Promise<SearchResult>((resolve) => { resolveOld = resolve; });
  vi.mocked(api.searchLogs).mockImplementation(async (request: SearchRequest) => request.query === 'old' ? old : result([event(0, { message: request.query === 'new' ? 'New result' : 'Initial result' })]));
  mountLogs(); await screen.findByText('Initial result');
  const input = screen.getByLabelText('Search');
  await userEvent.type(input, 'old{Enter}');
  await waitFor(() => expect(api.searchLogs).toHaveBeenCalledWith(expect.objectContaining({ query: 'old' }), expect.anything()));
  await userEvent.clear(input); await userEvent.type(input, 'new{Enter}');
  await screen.findByText('New result');
  await act(async () => resolveOld(result([event(0, { message: 'Stale result' })])));
  expect(screen.getByText('New result')).toBeInTheDocument();
  expect(screen.queryByText('Stale result')).not.toBeInTheDocument();
  expect(screen.queryByText('Searching…')).not.toBeInTheDocument();
});

it('renders template data as plain text and omits hidden names from formatted detail', () => {
  const item = event(0, { event: 'trade.completed' });
  expect(eventMessage(item, eventType)).toBe('Private player name transferred 42');
  render(<EventDetail item={item} type={eventType} rules={[hiddenName]} onClose={vi.fn()} onFilter={vi.fn()} />);
  const dialog = screen.getByRole('dialog', { name: 'Trade completed' });
  expect(within(dialog).queryByRole('button', { name: /Filter by actor.name/ })).not.toBeInTheDocument();
  expect(within(dialog).queryByText('Private player name transferred 42')).not.toBeInTheDocument();
  expect(within(dialog).getByText('transferred 42', { exact: false })).toBeVisible();
});

it('traps keyboard focus, filters raw values, and restores focus when detail closes', async () => {
  const opener = document.createElement('button'); opener.textContent = 'Open fixture'; document.body.append(opener); opener.focus();
  const close = vi.fn(); const filter = vi.fn();
  const view = render(<EventDetail item={event()} rules={[]} onClose={close} onFilter={filter} />);
  const dialog = screen.getByRole('dialog');
  expect(within(dialog).getByRole('button', { name: 'Close' })).toHaveFocus();
  const first = within(dialog).getByRole('button', { name: 'Copy JSON' });
  const last = within(dialog).getByText('Show original');
  first.focus(); await userEvent.tab({ shift: true }); expect(last).toHaveFocus();
  await userEvent.tab(); expect(first).toHaveFocus();
  await userEvent.click(within(dialog).getByRole('button', { name: 'Filter by amount: 42' }));
  expect(filter).toHaveBeenCalledWith('attributes.amount', '42');
  await userEvent.click(within(dialog).getByRole('button', { name: '#economy' }));
  expect(filter).toHaveBeenCalledWith('tag', 'economy');
  expect(within(dialog).queryByRole('button', { name: /Filter by timestamp:/ })).not.toBeInTheDocument();
  expect(within(dialog).queryByRole('button', { name: /Filter by host:/ })).not.toBeInTheDocument();
  await userEvent.keyboard('{Escape}'); expect(close).toHaveBeenCalledOnce();
  view.unmount(); expect(opener).toHaveFocus(); opener.remove();
});

it('brushes histogram buckets into an inclusive start and exclusive end', async () => {
  const select = vi.fn();
  render(<Histogram search={{ project_id: 'p1', from: '2026-11-06T12:00:00.000Z', to: '2026-11-06T12:03:00.000Z', tags: ['economy'], filters: [{ field: 'amount', op: 'gt', value: '0' }] }} interval={60} onRange={select} />);
  const region = screen.getByRole('region', { name: 'Events over time' });
  await waitFor(() => expect(within(region).getAllByRole('button')).toHaveLength(3));
  const bars = within(region).getAllByRole('button');
  fireEvent.pointerDown(bars[0]); fireEvent.pointerEnter(bars[2]); fireEvent.pointerUp(bars[2]);
  expect(select).toHaveBeenLastCalledWith('2026-11-06T12:00:00.000Z', '2026-11-06T12:03:00.000Z');
  fireEvent.click(bars[1], { detail: 0 });
  expect(select).toHaveBeenLastCalledWith('2026-11-06T12:01:00.000Z', '2026-11-06T12:02:00.000Z');
  expect(histogramCalls[0]).toMatchObject({ project_id: 'p1', interval_seconds: 60, tags: ['economy'], filters: [{ field: 'amount', op: 'gt', value: '0' }] });
});

it('uses a histogram brush as the next search range', async () => {
  setupSearch(); mountLogs(); await screen.findByText('Event message 0');
  const firstSearch = vi.mocked(api.searchLogs).mock.calls[0]?.[0];
  if (!firstSearch?.from) throw new Error('Missing range anchor');
  const from = firstSearch.from;
  const to = new Date(Math.floor(Date.parse(from) / 3600000) * 3600000 + 2 * 3600000).toISOString();
  const region = screen.getByRole('region', { name: 'Events over time' });
  await waitFor(() => expect(within(region).getAllByRole('button').length).toBeGreaterThan(2));
  const bars = within(region).getAllByRole('button');
  fireEvent.pointerDown(bars[0]); fireEvent.pointerUp(bars[1]);
  await waitFor(() => expect(api.searchLogs).toHaveBeenCalledWith(expect.objectContaining({ from, to }), expect.anything()));
});

it('keeps zero-count time gaps in the histogram and reports partial storage', async () => {
  vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ items: [buckets[0], buckets[2]], partial: true }), { status: 200 }));
  render(<Histogram search={{ project_id: 'p1', from: '2026-11-06T12:00:00.000Z', to: '2026-11-06T12:03:00.000Z' }} interval={60} onRange={vi.fn()} />);
  const region = screen.getByRole('region', { name: 'Events over time' });
  await waitFor(() => expect(within(region).getAllByRole('button')).toHaveLength(3));
  expect(within(region).getAllByRole('button')[1]).toHaveAccessibleName(/: 0$/);
  expect(within(region).getByText(i18n.t('explorer.partial'))).toBeVisible();
});

it('keeps service and trace detail filters in their builtin search and Live fields', async () => {
  setupSearch([event(0, { message: 'Filterable operation' })]); mountLogs(true);
  await userEvent.click(await screen.findByText('Filterable operation'));
  await userEvent.click(screen.getByRole('button', { name: 'Filter by service: bank' }));
  await waitFor(() => expect(api.searchLogs).toHaveBeenCalledWith(expect.objectContaining({ services: ['bank'] }), expect.anything()));
  await waitFor(() => {
    const latest = Source.instances[Source.instances.length - 1];
    expect(latest && new URL(latest.url, 'http://localhost').searchParams.get('services')).toBe('bank');
  });
  await userEvent.click(await screen.findByText('Filterable operation'));
  await userEvent.click(screen.getByRole('button', { name: 'Filter by trace_id: 0123456789abcdef' }));
  await waitFor(() => expect(api.searchLogs).toHaveBeenCalledWith(expect.objectContaining({ services: ['bank'], trace_id: '0123456789abcdef' }), expect.anything()));
  await waitFor(() => {
    const latest = Source.instances[Source.instances.length - 1];
    expect(latest && new URL(latest.url, 'http://localhost').searchParams.get('trace_id')).toBe('0123456789abcdef');
  });
});

it('escapes template values instead of interpreting markup', () => {
  const hostile = '<img src=x onerror="alert(1)">';
  render(<EventDetail item={event(0, { event: eventType.name, attributes: { actor: { name: hostile }, amount: 42 } })} type={eventType} rules={[]} onClose={vi.fn()} onFilter={vi.fn()} />);
  expect(screen.getByText(`${hostile} transferred 42`)).toBeVisible();
  expect(screen.queryByRole('img')).not.toBeInTheDocument();
  expect(document.querySelector('[onerror]')).toBeNull();
});

it('hides a nested descendant without hiding neighboring attribute values', () => {
  const hidden: FieldRule = { field: 'payment.secret', label: 'Secret', hidden: true, format: 'text', redact: false };
  render(<EventDetail item={event(0, { attributes: { payment: { secret: 'hidden-payment-secret', amount: 42 } } })} rules={[hidden]} onClose={vi.fn()} onFilter={vi.fn()} />);
  const what = screen.getByRole('heading', { name: 'What' }).parentElement;
  if (!what) throw new Error('Missing What section');
  expect(within(what).queryByText(/hidden-payment-secret/)).not.toBeInTheDocument();
  expect(within(what).getByText('42', { exact: false })).toBeVisible();
});

it('hides every descendant and template placeholder of a hidden parent', () => {
  const hidden: FieldRule = { field: 'attributes.actor', label: '', hidden: true, format: 'text', redact: false };
  render(<EventDetail item={event(0, { event: eventType.name })} type={eventType} rules={[hidden]} onClose={vi.fn()} onFilter={vi.fn()} />);
  const who = screen.getByRole('heading', { name: 'Who' }).parentElement;
  if (!who) throw new Error('Missing Who section');
  expect(within(who).queryByRole('button')).not.toBeInTheDocument();
  expect(screen.queryByText('Private player name transferred 42')).not.toBeInTheDocument();
  expect(screen.getByText('transferred 42', { exact: false })).toBeVisible();
});

it('distinguishes an attribute named service from the builtin service filter', async () => {
  setupSearch([event(0, { message: 'Colliding field', attributes: { service: 'attribute-service', id: 'attribute-id' } })]); mountLogs();
  await userEvent.click(await screen.findByText('Colliding field'));
  await userEvent.click(screen.getByRole('button', { name: 'Filter by service: attribute-service' }));
  await waitFor(() => expect(api.searchLogs).toHaveBeenCalledWith(expect.objectContaining({ filters: [{ field: 'attributes.service', op: 'eq', value: 'attribute-service' }] }), expect.anything()));
  const latest = vi.mocked(api.searchLogs).mock.calls.at(-1)?.[0];
  expect(latest?.services).toBeUndefined();
});

it('prunes hidden descendants from object placeholders in templates', () => {
  const hidden: FieldRule = { field: 'payment.secret', label: '', hidden: true, format: 'text', redact: false };
  const type = { ...eventType, template: 'Payment {payment}' };
  render(<EventDetail item={event(0, { attributes: { payment: { amount: 42, secret: 'private-payment-token' } } })} type={type} rules={[hidden]} onClose={vi.fn()} onFilter={vi.fn()} />);
  expect(screen.getByText('Payment {"amount":42}')).toBeVisible();
  expect(screen.queryByText(/Payment .*private-payment-token/)).not.toBeInTheDocument();
});

it('restores builtin service and trace filters from a saved view', async () => {
  setupSearch();
  const view = { id: 'v1', workspace_id: 'w1', project_id: 'p1', name: 'Service trace', query: { services: ['restored-service'], trace_id: '0123abc' }, created_by: 'me', created_at: '', updated_at: '' };
  vi.mocked(api.savedQueries).mockResolvedValue([view]);
  mountLogs();
  await userEvent.selectOptions(await screen.findByLabelText('Saved views'), 'v1');
  await waitFor(() => expect(api.searchLogs).toHaveBeenCalledWith(expect.objectContaining({ services: ['restored-service'], trace_id: '0123abc' }), expect.anything()));
});

it('preserves literal quotes and backslashes when filtering from a detail value', async () => {
  const value = 'Player "Seven" \\ unit';
  setupSearch([event(0, { message: 'Quoted actor', attributes: { actor: { name: value } } })]); mountLogs();
  await userEvent.click(await screen.findByText('Quoted actor'));
  await userEvent.click(screen.getByRole('button', { name: `Filter by actor.name: ${value}` }));
  await waitFor(() => expect(api.searchLogs).toHaveBeenCalledWith(expect.objectContaining({ filters: [{ field: 'attributes.actor.name', op: 'eq', value }] }), expect.anything()));
});
