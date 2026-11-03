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
    const calls = stubApi({ ...anonymous, 'POST /auth/login': { body: session }, ...{ 'GET /projects?workspace_id=w1': signedIn['GET /projects?workspace_id=w1'], 'GET /projects/p1/environments': signedIn['GET /projects/p1/environments'], 'GET /projects/p1/streams': signedIn['GET /projects/p1/streams'] } });
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
  test('shows real projects, environments and streams, with an honest empty explorer', async () => {
    stubApi(signedIn);
    renderApp();
    expect(await screen.findByRole('link', { name: 'default' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Shop' })).toHaveAttribute('aria-current', 'true');
    expect(screen.getByRole('option', { name: 'production' })).toBeInTheDocument();
    expect(screen.getByText('No events yet')).toBeInTheDocument();
  });

  test('a section without a backend says so', async () => {
    stubApi(signedIn);
    renderApp('/projects/p1/alerts');
    expect(await screen.findByRole('heading', { name: 'Alerts' })).toBeInTheDocument();
    expect(screen.getByText('Not available yet')).toBeInTheDocument();
  });

  test('a project outside the workspace is not found', async () => {
    stubApi(signedIn);
    renderApp('/projects/other');
    expect(await screen.findByRole('heading', { name: 'Page not found', level: 1 })).toBeInTheDocument();
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
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Workspace name'), 'Acme');
    await userEvent.click(screen.getByRole('button', { name: 'Create workspace' }));
    await userEvent.type(await screen.findByLabelText('Project name'), 'Shop');
    await userEvent.click(screen.getByRole('button', { name: 'Create project' }));
    expect(await screen.findByText('No events yet')).toBeInTheDocument();
    const post = calls.find((c) => c.key === 'POST /projects');
    expect(post?.body).toEqual({ workspace_id: 'w1', name: 'Shop' });
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

test('English and Italian define the same keys', () => {
  const keys = (o: object, prefix = ''): string[] =>
    Object.entries(o).flatMap(([k, v]) => (typeof v === 'object' ? keys(v as object, `${prefix}${k}.`) : [`${prefix}${k}`]));
  expect(keys(it).sort()).toEqual(keys(en).sort());
});
