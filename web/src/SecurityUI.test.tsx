import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useLocation } from 'react-router-dom';
import App from './App';
import i18n from './i18n';

const user = { id: 'u1', email: 'ada@example.com', username: 'ada', email_verified: true, is_instance_admin: false, status: 'active', created_at: '' };
const workspace = { id: 'w1', slug: 'acme', name: 'Acme', created_at: '', role: 'owner' };
const project = { id: 'p1', workspace_id: 'w1', slug: 'shop', name: 'Shop', created_at: '' };
const session = { user, workspaces: [workspace], csrf_token: 'csrf-security' };
type Reply = { status?: number; body?: unknown };
type Call = { key: string; body: unknown; headers: Headers };
type Routes = Record<string, Reply | (() => Reply | Promise<Reply>)>;
const authenticated: Routes = {
  'GET /auth/me': { body: session },
  'GET /projects?workspace_id=w1': { body: { items: [project] } },
  'GET /projects/p1/environments': { body: { items: [] } },
  'GET /projects/p1/streams': { body: { items: [] } },
  'GET /projects/p1/event-types': { body: { items: [] } },
  'GET /projects/p1/field-rules': { body: { items: [] } },
  'GET /queries?project_id=p1': { body: { items: [] } },
  'POST /logs/search': { body: { items: [], next_cursor: null, total: 0, level_counts: {}, partial: false, searched_tiers: ['hot'] } },
  'POST /logs/histogram': { body: { items: [], partial: false, searched_tiers: ['hot'] } },
  'GET /auth/sessions': { body: { items: [] } },
  'GET /auth/security': { body: { totp_enabled: false, passkeys: [] } },
};
function stubApi(routes: Routes) {
  const calls: Call[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${url.replace('/api/v1', '')}`;
    calls.push({ key, body: init?.body ? JSON.parse(String(init.body)) : undefined, headers: new Headers(init?.headers) });
    const route = routes[key];
    const reply = typeof route === 'function' ? await route() : route ?? { status: 599, body: { detail: `Unexpected API request: ${key}` } };
    return { status: reply.status ?? 200, ok: (reply.status ?? 200) < 400, headers: new Headers(), json: async () => reply.body };
  }));
  return calls;
}
function CurrentRoute() {
  const location = useLocation();
  return <output data-testid="current-route">{location.pathname}</output>;
}
function renderApp(path: string) {
  return render(<MemoryRouter initialEntries={[path]}><App /><CurrentRoute /></MemoryRouter>);
}
beforeEach(async () => {
  localStorage.clear();
  window.history.replaceState(null, '', '/');
  await i18n.changeLanguage('en');
});
const originalCredentials = Object.getOwnPropertyDescriptor(navigator, 'credentials');
afterEach(() => {
  vi.unstubAllGlobals();
  if (originalCredentials) Object.defineProperty(navigator, 'credentials', originalCredentials);
  else Reflect.deleteProperty(navigator, 'credentials');
});

test('password authentication waits for MFA and submits the one-time challenge without issuing an early session', async () => {
  const calls = stubApi({
    ...authenticated,
    'GET /auth/me': { status: 401 },
    'GET /auth/providers': { body: { local: true, providers: [], open_registration: false, setup_required: false } },
    'POST /auth/login': { status: 202, body: { mfa_required: true, challenge: 'challenge-1' } },
    'POST /auth/mfa/verify': { body: session },
  });
  renderApp('/login?next=%2Fw%2Fw1%2Fp%2Fp1');
  await userEvent.type(await screen.findByLabelText('Email or username'), 'Ada');
  await userEvent.type(screen.getByLabelText('Password'), 'correct-password');
  await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
  const code = await screen.findByLabelText('Authenticator or recovery code');
  expect(screen.getByTestId('current-route')).toHaveTextContent('/login');
  expect(calls.some((call) => call.key === 'GET /projects?workspace_id=w1')).toBe(false);
  await userEvent.type(code, '123456');
  await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  await screen.findByRole('heading', { name: 'Send the first event to Shop' });
  expect(calls.find((call) => call.key === 'POST /auth/mfa/verify')?.body).toEqual({ challenge: 'challenge-1', code: '123456' });
  expect(screen.getByTestId('current-route')).toHaveTextContent('/w/w1/p/p1');
});

test('an invalid MFA code stays on the verification form and explains the server error', async () => {
  stubApi({ ...authenticated, 'GET /auth/me': { status: 401 },
    'GET /auth/providers': { body: { local: true, providers: [], open_registration: false, setup_required: false } },
    'POST /auth/login': { status: 202, body: { mfa_required: true, challenge: 'challenge-1' } },
    'POST /auth/mfa/verify': { status: 401, body: { detail: 'invalid or already used verification code' } },
  });
  renderApp('/login');
  await userEvent.type(await screen.findByLabelText('Email or username'), 'ada');
  await userEvent.type(screen.getByLabelText('Password'), 'correct-password');
  await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
  await userEvent.type(await screen.findByLabelText('Authenticator or recovery code'), '000000');
  await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('invalid or already used verification code');
  expect(screen.getByTestId('current-route')).toHaveTextContent('/login');
  await userEvent.click(screen.getByRole('button', { name: 'Back' }));
  expect(screen.getByLabelText('Password')).toBeInTheDocument();
});

test('an OAuth MFA fragment is consumed locally and a recovery code can complete it', async () => {
  window.history.replaceState(null, '', '/login#mfa=oauth-challenge');
  const calls = stubApi({ ...authenticated, 'GET /auth/me': { status: 401 },
    'GET /auth/providers': { body: { local: true, providers: [], open_registration: false, setup_required: false } },
    'POST /auth/mfa/verify': { body: session },
  });
  renderApp('/login');
  await userEvent.type(await screen.findByLabelText('Authenticator or recovery code'), 'unused-recovery-code');
  expect(window.location.hash).toBe('');
  await userEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  await screen.findByRole('heading', { name: 'Send the first event to Shop' });
  expect(calls.find((call) => call.key === 'POST /auth/mfa/verify')?.body).toEqual({ challenge: 'oauth-challenge', code: 'unused-recovery-code' });
  expect(calls.some((call) => call.key === 'POST /auth/login')).toBe(false);
});

test('TOTP enrollment confirms the code and displays recovery codes until acknowledged', async () => {
  let enabled = false;
  const calls = stubApi({ ...authenticated,
    'GET /auth/security': () => ({ body: { totp_enabled: enabled, passkeys: [] } }),
    'POST /auth/totp/setup': { body: { secret: 'AUTHENTICATORSECRET', uri: 'otpauth://totp/Ervisio?secret=AUTHENTICATORSECRET' } },
    'POST /auth/totp/confirm': () => { enabled = true; return { body: { totp_enabled: true, recovery_codes: ['one-time-code-a', 'one-time-code-b'] } }; },
  });
  renderApp('/account');
  const security = (await screen.findByRole('heading', { name: 'Security' })).closest('section');
  if (!security) throw new Error('Security section not found');
  const form = within(security);
  await userEvent.type(await form.findByLabelText(/Current password/), 'password-proof');
  await userEvent.click(form.getByRole('button', { name: 'Set up authenticator' }));
  expect(await form.findByText('AUTHENTICATORSECRET')).toBeInTheDocument();
  await userEvent.type(form.getByLabelText('Authenticator or recovery code'), '123456');
  await userEvent.click(form.getByRole('button', { name: 'Confirm' }));
  expect(await form.findByText(/one-time-code-a/)).toBeInTheDocument();
  expect(await form.findByRole('button', { name: 'Disable' })).toBeInTheDocument();
  expect(calls.find((call) => call.key === 'POST /auth/totp/setup')?.body).toEqual({ password: 'password-proof' });
  expect(calls.find((call) => call.key === 'POST /auth/totp/confirm')?.headers.get('X-CSRF-Token')).toBe('csrf-security');
  await userEvent.click(form.getByRole('button', { name: 'I saved the codes' }));
  expect(form.queryByText(/one-time-code-a/)).not.toBeInTheDocument();
});

class BrowserPasskey {
  static parseCreationOptionsFromJSON = vi.fn((options: unknown) => options);
  static parseRequestOptionsFromJSON = vi.fn((options: unknown) => options);
  toJSON() { return { id: 'credential-id', rawId: 'credential-id', type: 'public-key', response: { signature: 'signed-response' } }; }
}
function enableBrowserPasskeys() {
  vi.stubGlobal('PublicKeyCredential', BrowserPasskey);
  const get = vi.fn().mockResolvedValue(new BrowserPasskey());
  const create = vi.fn().mockResolvedValue(new BrowserPasskey());
  Object.defineProperty(navigator, 'credentials', { configurable: true, value: { get, create } });
  return { get, create };
}
test('passkey sign-in uses browser credential verification and submits the serialized assertion', async () => {
  const browser = enableBrowserPasskeys();
  const calls = stubApi({ ...authenticated, 'GET /auth/me': { status: 401 },
    'GET /auth/providers': { body: { local: true, providers: [], open_registration: false, setup_required: false } },
    'POST /auth/passkeys/login/start': { body: { challenge: 'passkey-challenge', options: { publicKey: { challenge: 'server-challenge', rpId: 'localhost' } } } },
    'POST /auth/passkeys/login/finish': { body: session },
  });
  renderApp('/login');
  const button = await screen.findByRole('button', { name: 'Sign in with a passkey' });
  expect(button).toBeDisabled();
  await userEvent.type(screen.getByLabelText('Email or username'), 'Ada');
  await userEvent.click(button);
  await screen.findByRole('heading', { name: 'Send the first event to Shop' });
  expect(browser.get).toHaveBeenCalledWith({ publicKey: { challenge: 'server-challenge', rpId: 'localhost' } });
  expect(calls.find((call) => call.key === 'POST /auth/passkeys/login/start')?.body).toEqual({ login: 'ada' });
  expect(calls.find((call) => call.key === 'POST /auth/passkeys/login/finish')?.body).toEqual({ challenge: 'passkey-challenge', credential: new BrowserPasskey().toJSON() });
  expect(calls.some((call) => call.key === 'POST /auth/login')).toBe(false);
});

test('explicit workspace/project routes reject a mismatched workspace before requesting tenant logs', async () => {
  const calls = stubApi(authenticated);
  renderApp('/w/not-member/p/p1');
  expect(await screen.findByRole('heading', { name: 'Page not found', level: 1 })).toBeInTheDocument();
  expect(calls.some((call) => call.key === 'POST /logs/search')).toBe(false);
});

test('usage renders the backend event and byte totals without assuming a nested totals object', async () => {
  stubApi({ ...authenticated, 'GET /usage?project_id=p1&range=24h&interval_seconds=3600': { body: {
    items: [{ timestamp: '2026-11-06T12:00:00Z', stream: 'economy', events: 7, bytes: 512 }],
    events: 7, bytes: 512, byte_measurement: 'uncompressed_text_payload', partial: false, searched_tiers: ['hot'],
    quota: { daily_events: 100, daily_bytes: 1024 }, quota_used: { events: 7, bytes: 512, scope: 'current_process' },
  } } });
  renderApp('/w/w1/p/p1/usage');
  expect(await screen.findByRole('heading', { name: 'Usage' })).toBeInTheDocument();
  expect(await screen.findByText('economy')).toBeInTheDocument();
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
  expect(screen.getAllByText('7')).toHaveLength(2);
});

test('optional passkey enrollment serializes the browser credential and removal uses current account proof', async () => {
  const browser = enableBrowserPasskeys();
  let registered = false;
  const key = { id: 'passkey-1', name: 'Hardware key', created_at: '2026-11-06T12:00:00Z', last_used_at: null };
  const calls = stubApi({ ...authenticated,
    'GET /auth/security': () => ({ body: { totp_enabled: false, passkeys: registered ? [key] : [] } }),
    'POST /auth/passkeys/register/start': { body: { challenge: 'registration-challenge', options: { publicKey: { challenge: 'server-create-challenge' } } } },
    'POST /auth/passkeys/register/finish': () => { registered = true; return { status: 201, body: key }; },
    'DELETE /auth/passkeys/passkey-1': () => { registered = false; return { status: 204 }; },
  });
  renderApp('/account');
  const security = (await screen.findByRole('heading', { name: 'Security' })).closest('section');
  if (!security) throw new Error('Security section not found');
  const form = within(security);
  await userEvent.type(await form.findByLabelText(/Current password/), 'password-proof');
  await userEvent.type(form.getByLabelText('Passkey name'), 'Hardware key');
  await userEvent.click(form.getByRole('button', { name: 'Add passkey' }));
  expect(await form.findByText('Hardware key')).toBeInTheDocument();
  expect(browser.create).toHaveBeenCalledWith({ publicKey: { challenge: 'server-create-challenge' } });
  expect(calls.find((call) => call.key === 'POST /auth/passkeys/register/start')?.body).toEqual({ password: 'password-proof', code: '', name: 'Hardware key' });
  expect(calls.find((call) => call.key === 'POST /auth/passkeys/register/finish')?.body).toEqual({ challenge: 'registration-challenge', credential: new BrowserPasskey().toJSON() });
  await userEvent.click(form.getByRole('button', { name: 'Delete' }));
  await waitFor(() => expect(form.queryByText('Hardware key')).not.toBeInTheDocument());
  expect(calls.find((call) => call.key === 'DELETE /auth/passkeys/passkey-1')?.body).toEqual({ password: 'password-proof', code: '' });
});

test('cancelling browser passkey verification keeps sign-in open and never submits a completion', async () => {
  const browser = enableBrowserPasskeys();
  browser.get.mockResolvedValue(null);
  const calls = stubApi({ ...authenticated, 'GET /auth/me': { status: 401 },
    'GET /auth/providers': { body: { local: true, providers: [], open_registration: false, setup_required: false } },
    'POST /auth/passkeys/login/start': { body: { challenge: 'passkey-challenge', options: { publicKey: { challenge: 'server-challenge', rpId: 'localhost' } } } },
  });
  renderApp('/login');
  await userEvent.type(await screen.findByLabelText('Email or username'), 'ada');
  await userEvent.click(screen.getByRole('button', { name: 'Sign in with a passkey' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Passkey sign-in cancelled');
  expect(calls.some((call) => call.key === 'POST /auth/passkeys/login/finish')).toBe(false);
  expect(screen.getByTestId('current-route')).toHaveTextContent('/login');
});

test('existing invitation accounts keep the secret in fragments across local sign-in', async () => {
  const token = 'private-invitation-token';
  let signedIn = false;
  const calls = stubApi({ ...authenticated,
    'GET /auth/me': () => signedIn ? { body: session } : { status: 401 },
    'GET /auth/providers': { body: { local: true, providers: [], open_registration: false, setup_required: false } },
    'POST /auth/login': () => { signedIn = true; return { body: session }; },
    'POST /invitations/preview': { body: { workspace_name: 'Acme', email: user.email, role: 'viewer', has_account: true } },
    'POST /invitations/accept': { status: 204 },
  });
  const { unmount } = renderApp(`/invite#token=${token}`);
  const link = await screen.findByRole('link', { name: 'Sign in to accept the invitation' });
  const target = new URL(link.getAttribute('href') ?? '', window.location.origin);
  expect(target.searchParams.get('next')).toBe('/invite');
  expect(target.search).not.toContain(token);
  expect(target.hash).toBe(`#invite=${token}`);
  window.history.replaceState(null, '', target.pathname + target.search + target.hash);
  unmount();
  renderApp(target.pathname + target.search + target.hash);
  await userEvent.type(await screen.findByLabelText('Email or username'), 'ada');
  expect(window.location.hash).toBe('');
  await userEvent.type(screen.getByLabelText('Password'), 'account-password');
  await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
  const accept = await screen.findByRole('button', { name: 'Continue' });
  await waitFor(() => expect(accept).toBeEnabled());
  await userEvent.click(accept);
  expect(await screen.findByText('Completed.', { exact: true })).toBeInTheDocument();
  expect(calls.find((call) => call.key === 'POST /invitations/accept')?.body).toEqual({ token });
  expect(calls.every((call) => !call.key.includes(token))).toBe(true);
  expect(Object.values(sessionStorage).some((value) => String(value).includes(token))).toBe(false);
});

test('invitation preview waits for authentication so cookie requests include the CSRF token', async () => {
  let resolveSession: (value: Reply) => void = () => { throw new Error('Session was not requested'); };
  const pending = new Promise<Reply>((resolve) => { resolveSession = resolve; });
  const calls = stubApi({ ...authenticated,
    'GET /auth/me': () => pending,
    'POST /invitations/preview': { body: { workspace_name: 'Acme', email: user.email, role: 'viewer', has_account: true } },
  });
  renderApp('/invite#token=private-token');
  await waitFor(() => expect(calls.some((call) => call.key === 'GET /auth/me')).toBe(true));
  expect(calls.some((call) => call.key === 'POST /invitations/preview')).toBe(false);
  resolveSession({ body: session });
  await screen.findByRole('button', { name: 'Continue' });
  await waitFor(() => expect(calls.find((call) => call.key === 'POST /invitations/preview')).toBeDefined());
  const preview = calls.find((call) => call.key === 'POST /invitations/preview');
  expect(preview?.headers.get('X-CSRF-Token')).toBe('csrf-security');
  expect(preview?.body).toEqual({ token: 'private-token' });
});
