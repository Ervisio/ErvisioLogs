import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Outlet, Route, Routes } from 'react-router-dom';
import Logs from './Logs';
import { api, type LogItem, type SearchResult } from '../lib/api';
import i18n from '../i18n';

const { retry } = vi.hoisted(() => ({ retry: vi.fn() }));
vi.mock('../lib/auth', () => ({ useAuth: () => ({ retry }) }));
vi.mock('../components/LogViews', () => ({ default: () => null }));

const project = { id: 'p1', workspace_id: 'w1', slug: 'roma', name: 'Roma RP', created_at: '' };
const item: LogItem = {
  id: 'event-1', tier: 'hot', timestamp: '2026-11-04T12:00:00Z', ingested_at: '2026-11-04T12:00:01Z',
  level: 'INFO', severity_number: 9, event: 'money.add', message: 'Cash +500', stream: 'economy', environment: 'production',
  resource: 'bank', service: '', host: '', trace_id: '', span_id: '', attributes: { amount: 500 },
};
const result: SearchResult = { items: [item], next_cursor: null, total: 1, level_counts: { INFO: 8, ERROR: 2 }, searched_tiers: ['hot'], partial: false };

function Context() {
  return <Outlet context={{ streams: [], environment: null, environments: [], setEnvironment: vi.fn(), topbar: (title: React.ReactNode) => <header>{title}</header> }} />;
}

beforeEach(async () => { await i18n.changeLanguage('en'); });
afterEach(() => { vi.restoreAllMocks(); });

test('keeps all seven severity choices and their counts while a level-filtered request is pending', async () => {
  const user = userEvent.setup();
  let finish: (value: SearchResult) => void = () => {};
  const search = vi.spyOn(api, 'searchLogs').mockResolvedValueOnce(result).mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  render(<MemoryRouter initialEntries={['/']}><Routes><Route element={<Context />}><Route index element={<Logs project={project} live={false} />} /></Route></Routes></MemoryRouter>);
  await screen.findByText('Cash +500');
  await user.click(screen.getByRole('button', { name: 'Filters' }));
  const levels = screen.getByRole('group', { name: 'Levels' });
  expect(within(levels).getAllByRole('button')).toHaveLength(7);
  await user.click(within(levels).getByRole('button', { name: /Error\s*2/ }));
  await waitFor(() => expect(search).toHaveBeenLastCalledWith(expect.objectContaining({ levels: ['ERROR'] })));
  expect(within(levels).getByRole('button', { name: /Info\s*8/ })).toBeInTheDocument();
  expect(within(levels).getByRole('button', { name: /Trace\s*0/ })).toBeInTheDocument();
  await act(async () => finish({ ...result, items: [], total: 0 }));
  expect(screen.getByRole('group', { name: 'Levels' })).toHaveTextContent('Info8');
});

test('clicking an attribute preserves quotes, paths and line breaks in the real search request', async () => {
  const user = userEvent.setup();
  const value = 'C:\\Games\\"Roma RP"\nsecond line';
  const search = vi.spyOn(api, 'searchLogs').mockResolvedValue({ ...result, items: [{ ...item, attributes: { note: value } }] });
  render(<MemoryRouter initialEntries={['/']}><Routes><Route element={<Context />}><Route index element={<Logs project={project} live={false} />} /></Route></Routes></MemoryRouter>);
  await screen.findByText('Cash +500');
  await user.click(screen.getByRole('button', { name: /Open event/ }));
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Filter by note' }));
  await waitFor(() => expect(search).toHaveBeenLastCalledWith(expect.objectContaining({ filters: [{ field: 'note', op: 'eq', value }] })));
  expect(screen.getByRole('dialog')).toBeInTheDocument();
});
