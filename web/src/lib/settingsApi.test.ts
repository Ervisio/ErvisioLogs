import { api, setCsrfToken, type SettingEntry } from './api';

const entry: SettingEntry = { key: 'ingest.max_streams', type: 'int', value: 20, inherited: 20, source: 'default', locked: false, min: 0, max: 10000 };

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

test('settings and metadata use the existing transport, scope paths and CSRF', async () => {
  const fetcher = vi.fn(async (_url: string, init?: RequestInit) => ({ ok: true, status: 200, json: async () => init?.method === 'GET' ? { items: [entry] } : entry }));
  vi.stubGlobal('fetch', fetcher);
  setCsrfToken('csrf');
  const ctrl = new AbortController();
  expect(await api.settings({ type: 'workspace', id: 'a/b' }, ctrl.signal)).toEqual([entry]);
  await api.putSetting({ type: 'project', id: 'p1' }, 'ingest.auto_create_streams', false, ctrl.signal);
  await api.resetSetting({ type: 'instance' }, 'auth.open_registration', ctrl.signal);
  await api.updateStream('p1', 's/1', { display_name: '', description: '', color: '' }, ctrl.signal);
  await api.updateEnvironment('p1', 'e1', { display_name: 'Production' }, ctrl.signal);
  expect(fetcher.mock.calls[0]).toEqual(['/api/v1/workspaces/a%2Fb/settings', expect.objectContaining({ method: 'GET', signal: ctrl.signal, credentials: 'same-origin' })]);
  expect(fetcher.mock.calls[1]).toEqual(['/api/v1/projects/p1/settings/ingest.auto_create_streams', expect.objectContaining({ method: 'PUT', body: '{"value":false}', headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }) })]);
  expect(fetcher.mock.calls[2]).toEqual(['/api/v1/admin/settings/auth.open_registration', expect.objectContaining({ method: 'DELETE', body: undefined, headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }) })]);
  expect(fetcher.mock.calls[3]).toEqual(['/api/v1/projects/p1/streams/s%2F1', expect.objectContaining({ method: 'PATCH', body: '{"display_name":"","description":"","color":""}' })]);
  expect(fetcher.mock.calls[4]).toEqual(['/api/v1/projects/p1/environments/e1', expect.objectContaining({ method: 'PATCH', body: '{"display_name":"Production"}' })]);
});

test('an empty settings list is normalized and error detail reaches the caller', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ items: null }) }).mockResolvedValueOnce({ ok: false, status: 409, headers: new Headers(), json: async () => ({ detail: 'set by environment' }) }));
  expect(await api.settings({ type: 'instance' })).toEqual([]);
  await expect(api.putSetting({ type: 'instance' }, 'export.max_rows', 5)).rejects.toMatchObject({ status: 409, message: 'set by environment' });
});
