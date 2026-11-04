import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import App from './App';
import i18n from './i18n';
import en from './i18n/en.json';
import it from './i18n/it.json';
import { safeNext } from './lib/auth';

const user = { id: 'u1', email: 'ada@example.com', username: 'ada', email_verified: true, is_instance_admin: false, status: 'active', created_at: '' };
const workspace = { id: 'w1', slug: 'acme', name: 'Acme', created_at: '', role: 'owner' };
const session = { user, workspaces: [workspace], csrf_token: 'csrf-1' };
const project = { id: 'p1', workspace_id: 'w1', slug: 'shop', name: 'Shop', created_at: '' };
const providers = { local: true, providers: ['google', 'discord', 'cognito'], open_registration: true, setup_required: false };

type Reply = { status?: number; body?: unknown; headers?: Record<string, string> } | Error;
type Routes = Record<string, Reply | (() => Reply)>;
type Call = { key: string; headers: Record<string, string>; body: unknown };

/** Stubs fetch by "METHOD /path". An unlisted call fails the test with a 599 the UI cannot hide. */
function stubApi(routes: Routes) {
  const calls: Call[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const key = `${init?.method ?? 'GET'} ${url.replace('/api/v1', '')}`;
      calls.push({ key, headers: (init?.headers ?? {}) as Record<string, string>, body: init?.body ? JSON.parse(init.body as string) : undefined });
      const route = routes[key];
      const reply = typeof route === 'function' ? route() : (route ?? { status: 599 });
      if (reply instanceof Error) throw reply;
      const status = reply.status ?? 200;
      return {
        status,
        ok: status < 400,
        statusText: 'stub',
        headers: new Headers(reply.headers),
        json: async () => reply.body,
      };
    }),
  );
  return calls;
}
const signedIn: Routes = {
  'GET /auth/me': { body: session },
  'GET /projects?workspace_id=w1': { body: { items: [project] } },
  'GET /projects/p1/environments': { body: { items: [{ id: 'e1', project_id: 'p1', name: 'production', created_at: '' }] } },
  'GET /projects/p1/streams': { body: { items: [{ id: 's1', project_id: 'p1', name: 'default', type: 'log', created_at: '' }] } },
  'POST /logs/search': { body: { items: [], next_cursor: null, total: 0, level_counts: {}, searched_tiers: ['hot'], partial: false } },
};
const anonymous: Routes = { 'GET /auth/me': { status: 401 }, 'GET /auth/providers': { body: providers } };

const renderApp = (path = '/') =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );

beforeEach(async () => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  await i18n.changeLanguage('en');
});
afterEach(() => vi.unstubAllGlobals());

describe('auth guard', () => {
  test('an anonymous visitor lands on sign in and keeps the destination', async () => {
    const calls = stubApi({ ...anonymous, 'POST /auth/login': { body: session }, ...{ 'GET /projects?workspace_id=w1': signedIn['GET /projects?workspace_id=w1'], 'GET /projects/p1/environments': signedIn['GET /projects/p1/environments'], 'GET /projects/p1/streams': signedIn['GET /projects/p1/streams'], 'POST /logs/search': signedIn['POST /logs/search'] } });
    renderApp('/projects/p1/live');
    await userEvent.type(await screen.findByLabelText('Email or username'), 'Ada ');
    await userEvent.type(screen.getByLabelText('Password'), 'correct-horse');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('heading', { name: 'Live' })).toBeInTheDocument();
    expect(calls.find((c) => c.key === 'POST /auth/login')?.body).toEqual({ login: 'ada', password: 'correct-horse' });
  });

  test('a server failure does not look like being signed out', async () => {
    stubApi({ 'GET /auth/me': { status: 500 } });
    renderApp();
    expect(await screen.findByText('The server does not answer')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Sign in' })).not.toBeInTheDocument();
  });

  test.each([
    ['/projects/p1', '/projects/p1'],
    ['https://evil.example', '/'],
    ['//evil.example', '/'],
    ['/\\evil.example', '/'],
    [null, '/'],
  ])('safeNext(%s) is %s', (input, expected) => {
    expect(safeNext(input)).toBe(expected);
  });
});

describe('sign in page', () => {
  test('explains a wrong password', async () => {
    stubApi({ ...anonymous, 'POST /auth/login': { status: 401, body: { detail: 'no' } } });
    renderApp('/login');
    await userEvent.type(await screen.findByLabelText('Email or username'), 'ada');
    await userEvent.type(screen.getByLabelText('Password'), 'wrong');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Wrong email, username or password');
  });

  test('turns Retry-After into a wait', async () => {
    stubApi({ ...anonymous, 'POST /auth/login': { status: 429, headers: { 'Retry-After': '900' }, body: {} } });
    renderApp('/login');
    await userEvent.type(await screen.findByLabelText('Email or username'), 'ada');
    await userEvent.type(screen.getByLabelText('Password'), 'wrong');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Try again in 15 minutes.');
  });

  test('blocks an empty form and focuses the first invalid field', async () => {
    const calls = stubApi(anonymous);
    renderApp('/login');
    const login = await screen.findByLabelText('Email or username');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(screen.getByText('Enter your email or username.')).toBeInTheDocument();
    expect(login).toHaveFocus();
    expect(calls.some((c) => c.key.startsWith('POST'))).toBe(false);
  });

  test('offers only the configured providers', async () => {
    stubApi({ ...anonymous, 'GET /auth/providers': { body: { ...providers, providers: ['discord'] } } });
    renderApp('/login');
    expect(await screen.findByRole('button', { name: 'Continue with Discord' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Continue with Google' })).not.toBeInTheDocument();
  });

  test('hides the form when local accounts are off', async () => {
    stubApi({ ...anonymous, 'GET /auth/providers': { body: { ...providers, local: false } } });
    renderApp('/login');
    await screen.findByRole('button', { name: 'Continue with Google' });
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
    expect(screen.queryByText('Create an account')).not.toBeInTheDocument();
  });

  test('registration closed shows the way out, not a form', async () => {
    stubApi({ ...anonymous, 'GET /auth/providers': { body: { ...providers, open_registration: false } } });
    renderApp('/login?view=register');
    expect(await screen.findByText(/Registration is closed/)).toBeInTheDocument();
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
  });

  test('registers with normalized fields and checks the password locally', async () => {
    const calls = stubApi({ ...anonymous, ...signedIn, 'GET /auth/me': { status: 401 }, 'POST /auth/register': { status: 201, body: session } });
    renderApp('/login?view=register');
    await userEvent.type(await screen.findByLabelText('Email'), ' Ada@Example.com');
    await userEvent.type(screen.getByLabelText('Username'), 'Ada');
    await userEvent.type(screen.getByLabelText('Password'), 'short');
    await userEvent.tab();
    expect(screen.getByText('Too short: 5 more characters.')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Password'), '-but-long');
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }));
    await waitFor(() => expect(calls.find((c) => c.key === 'POST /auth/register')?.body).toEqual({ email: 'ada@example.com', username: 'ada', password: 'short-but-long' }));
  });

  test('a first run goes straight to setup and sends the code', async () => {
    const calls = stubApi({ ...anonymous, ...signedIn, 'GET /auth/me': { status: 401 }, 'GET /auth/providers': { body: { ...providers, setup_required: true } }, 'POST /auth/register': { status: 201, body: session } });
    renderApp('/login');
    expect(await screen.findByRole('heading', { name: 'Create the first administrator' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Continue with Google' })).not.toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Setup code'), ' code-1 ');
    await userEvent.type(screen.getByLabelText('Email'), 'ada@example.com');
    await userEvent.type(screen.getByLabelText('Username'), 'ada');
    await userEvent.type(screen.getByLabelText('Password'), 'correct-horse');
    await userEvent.click(screen.getByRole('button', { name: 'Create administrator' }));
    await waitFor(() => expect(calls.find((c) => c.key === 'POST /auth/register')?.body).toMatchObject({ setup_code: 'code-1' }));
  });

  test('explains an OAuth error code and an unknown one', async () => {
    stubApi(anonymous);
    const { unmount } = renderApp('/login?error=email_in_use');
    expect(await screen.findByText('This email already has an account')).toBeInTheDocument();
    unmount();
    renderApp('/login?error=%3Cscript%3E');
    expect(await screen.findByText('Sign in failed')).toBeInTheDocument();
    expect(screen.queryByText(/script/)).not.toBeInTheDocument();
  });

  test('the password toggle reveals the value', async () => {
    stubApi(anonymous);
    renderApp('/login');
    const password = await screen.findByLabelText('Password');
    expect(password).toHaveAttribute('type', 'password');
    await userEvent.click(screen.getByRole('button', { name: 'Show password' }));
    expect(password).toHaveAttribute('type', 'text');
    expect(screen.getByRole('button', { name: 'Hide password' })).toHaveAttribute('aria-pressed', 'true');
  });
});

describe('navigation', () => {
  test('shows real projects, environments and streams, and guides an empty project to its first event', async () => {
    stubApi(signedIn);
    renderApp();
    expect(await screen.findByRole('link', { name: 'default' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Shop' })).toHaveAttribute('aria-current', 'true');
    expect(screen.getByRole('option', { name: 'production' })).toBeInTheDocument();
    expect(await screen.findByRole('heading', { name: 'Send the first event to Shop' })).toBeInTheDocument();
  });

  test('a section without a backend says so', async () => {
    stubApi(signedIn);
    renderApp('/projects/p1/dashboards');
    expect(await screen.findByRole('heading', { name: 'Dashboards' })).toBeInTheDocument();
    expect(screen.getByText('Not available yet')).toBeInTheDocument();
  });

  test('a project outside the workspace is not found', async () => {
    stubApi(signedIn);
    renderApp('/projects/other');
    expect(await screen.findByRole('heading', { name: 'Page not found', level: 1 })).toBeInTheDocument();
  });

  test('a project of another workspace opens by its link and switches to that workspace', async () => {
    const w2 = { ...workspace, id: 'w2', slug: 'beta', name: 'Beta' };
    stubApi({
      ...signedIn,
      'GET /auth/me': { body: { ...session, workspaces: [workspace, w2] } },
      'GET /projects?workspace_id=w2': { body: { items: [{ ...project, id: 'p2', workspace_id: 'w2', name: 'Other' }] } },
      'GET /projects/p2/environments': { body: { items: [] } },
      'GET /projects/p2/streams': { body: { items: [] } },
    });
    renderApp('/projects/p2');
    expect(await screen.findByRole('heading', { name: 'Send the first event to Other' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Page not found' })).not.toBeInTheDocument();
  });

  test('rail expands and collapses', async () => {
    stubApi(signedIn);
    renderApp();
    const btn = await screen.findByRole('button', { name: 'Expand' });
    expect(btn).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(btn);
    expect(screen.getByRole('button', { name: 'Collapse' })).toHaveAttribute('aria-expanded', 'true');
    expect(localStorage.getItem('rail')).toBe('wide');
  });

  test('the account menu switches language and theme', async () => {
    stubApi(signedIn);
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Account' }));
    await userEvent.click(screen.getByRole('button', { name: 'Switch theme' }));
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(localStorage.getItem('theme')).toBe('light');
    await userEvent.selectOptions(screen.getByLabelText('Language'), 'it');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Esci' })).toBeInTheDocument());
    expect(localStorage.getItem('lang')).toBe('it');
  });

  test('sign out sends the CSRF token and returns to sign in', async () => {
    const calls = stubApi({ ...signedIn, 'GET /auth/providers': { body: providers }, 'POST /auth/logout': { status: 204 } });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Account' }));
    await userEvent.click(screen.getByRole('button', { name: 'Sign out' }));
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(calls.find((c) => c.key === 'POST /auth/logout')?.headers['X-CSRF-Token']).toBe('csrf-1');
  });

  test('the palette opens with Ctrl+K, filters and navigates', async () => {
    stubApi(signedIn);
    renderApp();
    await screen.findByRole('link', { name: 'default' });
    await userEvent.keyboard('{Control>}k{/Control}');
    const dialog = screen.getByRole('dialog', { name: 'Command palette' });
    await userEvent.keyboard('alerts');
    expect(within(dialog).getAllByRole('option')).toHaveLength(1);
    await userEvent.keyboard('{Enter}');
    expect(await screen.findByRole('heading', { name: 'Alerts' })).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});

describe('first project', () => {
  test('a user with no workspace creates one, then a project', async () => {
    const calls = stubApi({
      'GET /auth/me': { body: { ...session, workspaces: [] } },
      'POST /workspaces': { status: 201, body: workspace },
      'GET /projects?workspace_id=w1': { body: { items: [] } },
      'POST /projects': { status: 201, body: project },
      'GET /projects/p1/environments': signedIn['GET /projects/p1/environments'],
      'GET /projects/p1/streams': signedIn['GET /projects/p1/streams'],
      'POST /logs/search': signedIn['POST /logs/search'],
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Workspace name'), 'Acme');
    await userEvent.click(screen.getByRole('button', { name: 'Create workspace' }));
    await userEvent.type(await screen.findByLabelText('Project name'), 'Shop');
    await userEvent.click(screen.getByRole('button', { name: 'Create project' }));
    expect(await screen.findByRole('heading', { name: 'Send the first event to Shop' })).toBeInTheDocument();
    const post = calls.find((c) => c.key === 'POST /projects');
    expect(post?.body).toEqual({ workspace_id: 'w1', name: 'Shop', slug: 'shop' });
    expect(post?.headers['X-CSRF-Token']).toBe('csrf-1');
  });

  test('a role without permission gets a clear reason', async () => {
    stubApi({ ...signedIn, 'GET /projects?workspace_id=w1': { body: { items: [] } }, 'POST /projects': { status: 403, body: {} } });
    renderApp('/projects');
    await userEvent.type(await screen.findByLabelText('Project name'), 'Shop');
    await userEvent.click(screen.getByRole('button', { name: 'Create project' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Your role in this workspace cannot do this');
  });
});

describe('alerts', () => {
  test('an instance without alerts says so instead of a generic failure', async () => {
    stubApi({ ...signedIn, 'GET /alerts?project_id=p1': { status: 503, body: {} }, 'GET /notification-channels?workspace_id=w1': { status: 503, body: {} } });
    renderApp('/projects/p1/alerts');
    expect(await screen.findByRole('alert')).toHaveTextContent('Alerts are not available on this instance.');
  });
});

describe('system status', () => {
  const withReadyz = (reply: Reply) => {
    const routes = { ...signedIn };
    const real = vi.fn();
    const calls = stubApi(routes);
    const inner = globalThis.fetch as unknown as (u: string, i?: RequestInit) => Promise<unknown>;
    vi.stubGlobal(
      'fetch',
      real.mockImplementation((url: string, init?: RequestInit) => {
        if (url !== '/readyz') return inner(url, init);
        if (reply instanceof Error) return Promise.reject(reply);
        return Promise.resolve({ status: (reply as { status?: number }).status ?? 200, json: async () => (reply as { body?: unknown }).body });
      }),
    );
    return calls;
  };

  test('shows all services ready', async () => {
    withReadyz({ body: { status: 'ready', checks: { postgres: 'ok', clickhouse: 'ok', migrations_postgres: 'ok', migrations_clickhouse: 'ok' } } });
    renderApp('/status');
    expect(await screen.findByText('All services are ready')).toBeInTheDocument();
    expect(screen.getAllByText('OK')).toHaveLength(4);
  });

  test('shows a failing service from a 503 body', async () => {
    withReadyz({ status: 503, body: { status: 'not_ready', checks: { postgres: 'ok', clickhouse: 'fail', migrations_postgres: 'ok', migrations_clickhouse: 'ok' } } });
    renderApp('/status');
    expect(await screen.findByText('Some services are not ready')).toBeInTheDocument();
    expect(screen.getByText('Failing')).toBeInTheDocument();
  });

  test('reports an unreachable server', async () => {
    withReadyz(new Error('network'));
    renderApp('/status');
    expect(await screen.findByText('The server does not answer')).toBeInTheDocument();
  });
});

describe('in-app documentation', () => {
  test('opens from the rail without a workspace or project', async () => {
    const calls = stubApi({ 'GET /auth/me': { body: { ...session, workspaces: [] } } });
    renderApp('/projects');
    await userEvent.click(await screen.findByRole('link', { name: 'Documentation' }));
    expect(await screen.findByRole('heading', { name: 'Learn Ervisio Logs' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Getting started' })).toBeInTheDocument();
    expect(calls.every((call) => call.key === 'GET /auth/me')).toBe(true);
  });

  test('searches article bodies, handles no matches and clears the search', async () => {
    stubApi(signedIn);
    renderApp('/docs');
    const search = await screen.findByRole('searchbox', { name: 'Search documentation' });
    await userEvent.type(search, 'path-style');
    expect(screen.getByRole('heading', { name: 'Storage and retention' })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Getting started' })).not.toBeInTheDocument();
    await userEvent.clear(search);
    await userEvent.type(search, 'no-such-topic-xyz');
    expect(screen.getByText('No topics match this search.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Clear search' }));
    expect(screen.getByRole('heading', { name: 'Getting started' })).toBeInTheDocument();
  });

  test('deep links keep the article when changing language and copy server examples', async () => {
    const ui = userEvent.setup();
    const write = vi.spyOn(navigator.clipboard, 'writeText');
    stubApi(signedIn);
    renderApp('/docs/fivem');
    expect(await screen.findByRole('heading', { name: 'FiveM integration' })).toBeInTheDocument();
    await ui.click(screen.getAllByRole('button', { name: 'Copy example' })[0]);
    expect(write).toHaveBeenCalledWith(expect.stringContaining('set ervisio_logs_token'));
    expect(write.mock.calls[0][0]).not.toContain('setr');
    await ui.click(screen.getByRole('button', { name: 'Account' }));
    await ui.selectOptions(screen.getByLabelText('Language'), 'it');
    expect(await screen.findByRole('heading', { name: 'Integrazione FiveM' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Installa la risorsa' })).toBeInTheDocument();
    write.mockRestore();
  });

  test('explains clipboard failure and keeps the example available', async () => {
    const ui = userEvent.setup();
    const write = vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('denied'));
    stubApi(signedIn);
    renderApp('/docs/ingest-api');
    await ui.click((await screen.findAllByRole('button', { name: 'Copy example' }))[0]);
    expect(await screen.findByText('Could not copy. Select and copy the example manually.')).toBeInTheDocument();
    expect(screen.getByText(/curl -X POST https:\/\/logs.example.com\/api\/v1\/ingest /)).toBeInTheDocument();
    write.mockRestore();
  });

  test('an unknown topic offers the index, and command search opens the guide', async () => {
    stubApi(signedIn);
    renderApp('/docs/no-such-topic');
    expect(await screen.findByRole('heading', { name: 'Topic not found' })).toBeInTheDocument();
    await userEvent.keyboard('{Control>}k{/Control}');
    await userEvent.keyboard('Documentation{Enter}');
    expect(await screen.findByRole('heading', { name: 'Learn Ervisio Logs' })).toBeInTheDocument();
  });
});

test('English and Italian define the same keys', () => {
  const keys = (o: object, prefix = ''): string[] =>
    Object.entries(o).flatMap(([k, v]) => (typeof v === 'object' ? keys(v as object, `${prefix}${k}.`) : [`${prefix}${k}`]));
  expect(keys(it).sort()).toEqual(keys(en).sort());
});

const ev = (id: string, level: string, message: string, attributes: Record<string, unknown> | null = null) => ({
  id,
  timestamp: '2026-11-03T10:15:30.000Z',
  ingested_at: '2026-11-03T10:15:31.000Z',
  level,
  severity_number: 9,
  event: 'order.paid',
  message,
  stream: 'default',
  environment: 'production',
  service: 'api',
  host: 'web-1',
  resource: '',
  trace_id: '',
  span_id: '',
  attributes,
});
const searches = (calls: Call[]) => calls.filter((c) => c.key === 'POST /logs/search').map((c) => c.body as Record<string, unknown>);

describe('log explorer', () => {
  const found = {
    items: [ev('2', 'ERROR', 'payment declined', { user: 'ada' }), ev('1', 'INFO', 'order created')],
    next_cursor: 'c1',
    total: 3,
    level_counts: { INFO: 2, ERROR: 1 },
    searched_tiers: ['hot'],
    partial: false,
  };

  test('lists events with counts and sends key=value filters apart from the text', async () => {
    const calls = stubApi({ ...signedIn, 'POST /logs/search': { body: found } });
    renderApp('/projects/p1');
    expect(await screen.findByText('payment declined')).toBeInTheDocument();
    expect(screen.getByText('order created')).toBeInTheDocument();
    expect(screen.getByText('Showing 2 of 3')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /error\s*1/ })).toHaveAttribute('aria-pressed', 'false');

    await userEvent.type(screen.getByLabelText('Search'), 'user=ada "card declined"{Enter}');
    await waitFor(() => expect(searches(calls).at(-1)?.query).toBe('card declined'));
    expect(searches(calls).at(-1)).toMatchObject({ project_id: 'p1', filters: [{ field: 'user', op: 'eq', value: 'ada' }], include_total: true });
    expect(calls.find((c) => c.key === 'POST /logs/search')?.headers['X-CSRF-Token']).toBe('csrf-1');
  });

  test('load more appends the next page and a level chip narrows the search', async () => {
    const page2 = { ...found, items: [ev('0', 'INFO', 'older event')], next_cursor: null };
    const calls: Call[] = stubApi({
      ...signedIn,
      'POST /logs/search': () => ((calls.at(-1)?.body as { cursor?: string }).cursor ? { body: page2 } : { body: found }),
    });
    renderApp('/projects/p1');
    await userEvent.click(await screen.findByRole('button', { name: 'Load more' }));
    expect(await screen.findByText('older event')).toBeInTheDocument();
    expect(screen.getByText('payment declined')).toBeInTheDocument();
    expect(searches(calls).at(-1)?.cursor).toBe('c1');

    await userEvent.click(screen.getByRole('button', { name: /error\s*1/ }));
    await waitFor(() => expect(searches(calls).filter((c) => c.levels).at(-1)?.levels).toEqual(['ERROR']));
  });

  test('a row opens its details and Escape closes them', async () => {
    stubApi({ ...signedIn, 'POST /logs/search': { body: found } });
    renderApp('/projects/p1');
    await screen.findByText('payment declined');
    await userEvent.click(screen.getAllByRole('button', { name: /Open event from/ })[0]);
    const dialog = screen.getByRole('dialog', { name: 'order.paid' });
    expect(within(dialog).getByText(/"user": "ada"/)).toBeInTheDocument();
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  test('a search with no match says so instead of showing the setup guide', async () => {
    const calls = stubApi(signedIn);
    renderApp('/projects/p1');
    await screen.findByRole('heading', { name: 'Send the first event to Shop' });
    await userEvent.type(screen.getByLabelText('Search'), 'nothing{Enter}');
    expect(await screen.findByText('No events match')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Send the first event to Shop' })).not.toBeInTheDocument();
    expect(searches(calls).some((b) => b.query === 'nothing')).toBe(true);
  });

  test('a failed search explains itself and retries', async () => {
    let fail = true;
    stubApi({ ...signedIn, 'POST /logs/search': () => (fail ? { status: 500, body: {} } : { body: found }) });
    renderApp('/projects/p1');
    expect(await screen.findByText(/The search failed/)).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('payment declined')).toBeInTheDocument();
  });

  test('the level chips keep their counts while a level is selected', async () => {
    const calls: Call[] = stubApi({
      ...signedIn,
      'POST /logs/search': () =>
        (calls.at(-1)?.body as { levels?: string[] }).levels
          ? { body: { ...found, items: [ev('2', 'ERROR', 'payment declined')], level_counts: { ERROR: 1 } } }
          : { body: found },
    });
    renderApp('/projects/p1');
    await userEvent.click(await screen.findByRole('button', { name: /error\s*1/ }));
    await waitFor(() => expect(screen.getByRole('button', { name: /error\s*1/ })).toHaveAttribute('aria-pressed', 'true'));
    expect(screen.getByRole('button', { name: /info\s*2/ })).toBeInTheDocument();
  });

  test.each([
    ['viewer', false],
    ['analyst', true],
  ])('export shown for %s: %s', async (role, visible) => {
    stubApi({ ...signedIn, 'GET /auth/me': { body: { ...session, workspaces: [{ ...workspace, role }] } }, 'POST /logs/search': { body: found } });
    renderApp('/projects/p1');
    await screen.findByText('payment declined');
    expect(screen.queryByText('Export') !== null).toBe(visible);
  });
});

describe('first event', () => {
  test('creates an ingest token and sends a test event with it, without the session cookie', async () => {
    const created = { id: 't1', project_id: 'p1', name: 'First server', prefix: 'elk_ab', scopes: ['logs:ingest'], created_at: '', expires_at: null, last_used_at: null, revoked_at: null };
    const calls = stubApi({ ...signedIn, 'POST /tokens': { status: 201, body: { token: 'elk_ab_secret', api_token: created } }, 'POST /ingest': { status: 202, body: {} } });
    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [string, RequestInit | undefined][] } };
    renderApp('/projects/p1');
    await userEvent.click(await screen.findByRole('button', { name: 'Create token' }));
    expect(await screen.findByText(/Bearer elk_ab_secret/)).toBeInTheDocument();
    expect(calls.find((c) => c.key === 'POST /tokens')?.body).toEqual({ project_id: 'p1', name: expect.stringMatching(/^First server \(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\)$/), scopes: ['logs:ingest'] });

    await userEvent.click(screen.getByRole('button', { name: 'Send a test event' }));
    expect(await screen.findByText('Waiting for the first event…')).toBeInTheDocument();
    expect(calls.find((c) => c.key === 'POST /ingest')?.headers.Authorization).toBe('Bearer elk_ab_secret');
    expect(fetchMock.mock.calls.find((c) => c[0] === '/api/v1/ingest')?.[1]?.credentials).toBe('omit');
  });

  test('a role that cannot manage tokens gets a reason', async () => {
    stubApi({ ...signedIn, 'POST /tokens': { status: 403, body: {} } });
    renderApp('/projects/p1');
    await userEvent.click(await screen.findByRole('button', { name: 'Create token' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('You need permission to manage tokens');
  });
});

describe('api tokens', () => {
  const tok = (id: string, name: string, revoked = false) => ({
    id,
    project_id: 'p1',
    name,
    prefix: `elk_${id}`,
    scopes: ['logs:ingest'],
    created_at: '2026-10-01T00:00:00Z',
    expires_at: null,
    last_used_at: null,
    revoked_at: revoked ? '2026-10-02T00:00:00Z' : null,
  });

  test('lists active tokens, shows a new one once, and revokes after confirming', async () => {
    let items = [tok('a1', 'Game server'), tok('z9', 'Old box', true)];
    const calls = stubApi({
      ...signedIn,
      'GET /tokens?project_id=p1': () => ({ body: { items } }),
      'POST /tokens': () => {
        items = [...items, tok('b2', 'Reader')];
        return { status: 201, body: { token: 'elk_b2_plain', api_token: tok('b2', 'Reader') } };
      },
      'DELETE /tokens/a1': () => {
        items = items.map((x) => (x.id === 'a1' ? { ...x, revoked_at: '2026-11-03T00:00:00Z' } : x));
        return { status: 204 };
      },
    });
    renderApp('/projects/p1/tokens');
    expect(await screen.findByText('Game server', { selector: 'p' })).toBeInTheDocument();
    expect(screen.getByText('1 revoked token')).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText('Name'), 'Reader');
    await userEvent.click(screen.getByRole('checkbox', { name: /logs:ingest/ }));
    await userEvent.click(screen.getByRole('checkbox', { name: /logs:read/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Create token' }));
    expect(await screen.findByText('elk_b2_plain')).toBeInTheDocument();
    expect(calls.find((c) => c.key === 'POST /tokens')?.body).toEqual({ project_id: 'p1', name: 'Reader', scopes: ['logs:read'] });
    await userEvent.click(screen.getByRole('button', { name: 'I copied it' }));
    expect(screen.queryByText('elk_b2_plain')).not.toBeInTheDocument();

    vi.stubGlobal('confirm', vi.fn(() => true));
    await userEvent.click(screen.getByRole('button', { name: 'Revoke Game server' }));
    expect(await screen.findByText('2 revoked tokens')).toBeInTheDocument();
    expect(calls.find((c) => c.key === 'DELETE /tokens/a1')?.headers['X-CSRF-Token']).toBe('csrf-1');
  });

  test('creating needs a name and a permission', async () => {
    const calls = stubApi({ ...signedIn, 'GET /tokens?project_id=p1': { body: { items: [] } } });
    renderApp('/projects/p1/tokens');
    expect(await screen.findByText('No active tokens.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Create token' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Enter a name.');
    await userEvent.type(screen.getByLabelText('Name'), 'x');
    await userEvent.click(screen.getByRole('checkbox', { name: /logs:ingest/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Create token' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Pick at least one permission.');
    expect(calls.some((c) => c.key === 'POST /tokens')).toBe(false);
  });

  test('without permission the page says so and hides the form', async () => {
    stubApi({ ...signedIn, 'GET /tokens?project_id=p1': { status: 403, body: {} } });
    renderApp('/projects/p1/tokens');
    expect(await screen.findByText(/You need permission to manage tokens/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Create token' })).not.toBeInTheDocument();
  });
});

describe('workspace members', () => {
  const mem = (id: string, username: string, role: string) => ({ user_id: id, email: `${username}@example.com`, username, role, joined_at: '2026-10-01T00:00:00Z' });

  test('an owner changes a role, removes a member and adds one', async () => {
    let items = [mem('u1', 'ada', 'owner'), mem('u2', 'bob', 'viewer'), mem('u3', 'cy', 'developer')];
    vi.stubGlobal('confirm', vi.fn(() => true));
    const calls = stubApi({
      ...signedIn,
      'GET /workspaces/w1/members': () => ({ body: { items } }),
      'PATCH /workspaces/w1/members/u2': () => {
        items = items.map((m) => (m.user_id === 'u2' ? { ...m, role: 'analyst' } : m));
        return { status: 204 };
      },
      'DELETE /workspaces/w1/members/u3': () => {
        items = items.filter((m) => m.user_id !== 'u3');
        return { status: 204 };
      },
      'POST /workspaces/w1/members': () => {
        items = [...items, mem('u4', 'dee', 'viewer')];
        return { status: 201, body: mem('u4', 'dee', 'viewer') };
      },
    });
    renderApp('/projects/p1/users');
    expect(await screen.findByText('bob@example.com')).toBeInTheDocument();
    // Your own row has no role picker, only a way to leave.
    expect(screen.queryByRole('combobox', { name: 'Role of ada' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Leave workspace' })).toBeInTheDocument();

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Role of bob' }), 'analyst');
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Role of bob' })).toHaveValue('analyst'));
    const patch = calls.find((c) => c.key === 'PATCH /workspaces/w1/members/u2');
    expect(patch?.body).toEqual({ role: 'analyst' });
    expect(patch?.headers['X-CSRF-Token']).toBe('csrf-1');

    await userEvent.click(screen.getByRole('button', { name: 'Remove cy' }));
    await waitFor(() => expect(screen.queryByText('cy@example.com')).not.toBeInTheDocument());

    await userEvent.type(screen.getByLabelText('Email'), 'dee@example.com');
    await userEvent.click(screen.getByRole('button', { name: 'Add member' }));
    expect(await screen.findByText('dee joined as Viewer.')).toBeInTheDocument();
    expect(calls.find((c) => c.key === 'POST /workspaces/w1/members')?.body).toEqual({ email: 'dee@example.com', role: 'viewer' });
  });

  test('adding explains an unknown email, and demoting the last owner is refused', async () => {
    stubApi({
      ...signedIn,
      'GET /workspaces/w1/members': { body: { items: [mem('u1', 'ada', 'owner'), mem('u2', 'bob', 'owner')] } },
      'POST /workspaces/w1/members': { status: 404, body: { detail: 'no account' } },
      'PATCH /workspaces/w1/members/u2': { status: 409, body: { detail: 'a workspace needs at least one owner' } },
    });
    renderApp('/projects/p1/users');
    expect(await screen.findByText('bob@example.com')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Add member' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Enter a valid email address.');
    await userEvent.type(screen.getByLabelText('Email'), 'nobody@example.com');
    await userEvent.click(screen.getByRole('button', { name: 'Add member' }));
    expect(await screen.findByText(/No account uses this email/)).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Role of bob' }), 'admin');
    expect(await screen.findByText(/at least one owner/)).toBeInTheDocument();
  });

  test('an admin cannot touch owners or grant the owner role; a viewer only reads', async () => {
    const items = [mem('u1', 'ada', 'admin'), mem('u2', 'bob', 'owner'), mem('u3', 'cy', 'viewer')];
    const asRole = (role: string) => ({ ...signedIn, 'GET /auth/me': { body: { ...session, workspaces: [{ ...workspace, role }] } }, 'GET /workspaces/w1/members': { body: { items } } });

    stubApi(asRole('admin'));
    const { unmount } = renderApp('/projects/p1/users');
    expect(await screen.findByText('bob@example.com')).toBeInTheDocument();
    expect(screen.queryByRole('combobox', { name: 'Role of bob' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Remove bob' })).not.toBeInTheDocument();
    const picker = screen.getByRole('combobox', { name: 'Role of cy' });
    expect(within(picker).queryByRole('option', { name: 'Owner' })).not.toBeInTheDocument();
    unmount();

    stubApi(asRole('viewer'));
    renderApp('/projects/p1/users');
    expect(await screen.findByText('bob@example.com')).toBeInTheDocument();
    expect(screen.queryByRole('combobox', { name: /^Role of/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Add member' })).not.toBeInTheDocument();
  });
});

describe('storage', () => {
  const admin = { ...session, user: { ...user, is_instance_admin: true } };
  const instance = { scope_type: 'instance', scope_id: '00000000-0000-0000-0000-000000000000', local_age_hours: 168, global_age_hours: 2160, cache_max_bytes: 10 * 1024 ** 3, high_pct: 90, low_pct: 75, updated_by: null, updated_at: '' };
  const disk = { id: 'pv1', name: 'Disk', type: 'local', config: { root: '/srv/archive' }, secret_file: null, active: true, status: 'ok', last_error: null, last_checked_at: null, created_at: '', updated_at: '' };
  const overview = {
    providers: [disk],
    segments: { CATALOGED: { count: 3, bytes: 3 * 1024 ** 2 } },
    archiver_enabled: true,
    usage: { hot_bytes: 6 * 1024 ** 3, cache_bytes: 1024 ** 3, max_bytes: 10 * 1024 ** 3, high_pct: 90, low_pct: 75, over_capacity: false },
  };
  const storageRoutes: Routes = {
    ...signedIn,
    'GET /auth/me': { body: admin },
    'GET /admin/storage': { body: overview },
    'GET /admin/retention': { body: { items: [instance] } },
    'GET /admin/storage/secrets': { body: { items: [] } },
  };

  test('the section is listed only for the instance administrator', async () => {
    stubApi(signedIn);
    const { unmount } = renderApp('/projects/p1');
    await screen.findByRole('link', { name: 'default' });
    expect(screen.queryByRole('link', { name: 'Storage' })).not.toBeInTheDocument();
    unmount();
    stubApi(storageRoutes);
    renderApp('/projects/p1');
    expect(await screen.findByRole('link', { name: 'Storage' })).toBeInTheDocument();
  });

  test('a member who is not the instance administrator gets an explanation', async () => {
    stubApi(signedIn);
    renderApp('/projects/p1/storage');
    expect(await screen.findByRole('heading', { name: 'Instance administrators only' })).toBeInTheDocument();
  });

  test('shows disk use against the cap and the active destination', async () => {
    stubApi(storageRoutes);
    renderApp('/projects/p1/storage');
    expect(await screen.findByRole('img', { name: 'Hot data 6 GB, archive cache 1 GB, cap 10 GB' })).toBeInTheDocument();
    expect(screen.getByText('New archive segments go to Disk.')).toBeInTheDocument();
    expect(screen.getByText('3 segments, 3 MB')).toBeInTheDocument();
    expect(screen.getByLabelText('Days on local disk', { selector: '#st-p-local' })).toHaveAttribute('placeholder', '7');
  });

  test('a project override sends hours and only the fields filled in', async () => {
    const calls = stubApi({ ...storageRoutes, 'PUT /admin/retention/project/p1': { body: { ...instance, scope_type: 'project', scope_id: 'p1' } } });
    renderApp('/projects/p1/storage');
    await userEvent.type(await screen.findByLabelText('Days before deletion', { selector: '#st-p-global' }), '30');
    await userEvent.click(screen.getAllByRole('button', { name: 'Save retention' })[0]);
    await waitFor(() => expect(calls.find((c) => c.key === 'PUT /admin/retention/project/p1')?.body).toEqual({ global_age_hours: 720 }));
    expect(await screen.findByText('Retention saved.')).toBeInTheDocument();
  });

  test('adds a folder destination and never sends a secret file for it', async () => {
    const calls = stubApi({ ...storageRoutes, 'POST /admin/storage/providers': { status: 201, body: { ...disk, id: 'pv2', name: 'Spare', active: false } } });
    renderApp('/projects/p1/storage');
    await userEvent.click(await screen.findByText('Folder on this server'));
    await userEvent.type(screen.getByLabelText('Name'), 'Spare');
    await userEvent.type(screen.getByLabelText('Absolute folder path'), '/mnt/spare');
    await userEvent.click(screen.getByRole('button', { name: 'Add destination' }));
    await waitFor(() =>
      expect(calls.find((c) => c.key === 'POST /admin/storage/providers')?.body).toEqual({ name: 'Spare', type: 'local', config: { root: '/mnt/spare' }, secret_file: null }),
    );
  });

  test('S3 keys typed in the form go to a new secret file, then the provider names it', async () => {
    const routes: Routes = { ...storageRoutes, 'POST /admin/storage/providers': { status: 201, body: { ...disk, id: 'pv3', name: 'Bucket', type: 's3', active: false } } };
    // The file name ends in a time stamp, so any secret PUT answers 204.
    const calls = stubApi(new Proxy(routes, { get: (r, k: string) => (k.startsWith('PUT /admin/storage/secrets/') ? { status: 204 } : r[k]) }));
    renderApp('/projects/p1/storage');
    await userEvent.type(await screen.findByLabelText('Name'), 'My Bucket');
    await userEvent.type(screen.getByLabelText('Bucket'), 'logs');
    await userEvent.type(screen.getByLabelText('Access key'), 'AK');
    await userEvent.type(screen.getByLabelText('Secret key'), 'SK');
    await userEvent.click(screen.getByRole('button', { name: 'Add destination' }));
    await waitFor(() => expect(calls.find((c) => c.key === 'POST /admin/storage/providers')).toBeDefined());
    const put = calls.find((c) => c.key.startsWith('PUT /admin/storage/secrets/'));
    const file = put?.key.slice('PUT /admin/storage/secrets/'.length);
    expect(file).toMatch(/^my-bucket-[0-9a-z]+$/);
    expect(put?.body).toEqual({ value: JSON.stringify({ access_key: 'AK', secret_key: 'SK' }) });
    expect(calls.find((c) => c.key === 'POST /admin/storage/providers')?.body).toEqual({ name: 'My Bucket', type: 's3', config: { bucket: 'logs' }, secret_file: file });
  });
});
