import { createHmac } from 'node:crypto';
import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { RealAPI, signIn, type Entity } from './fixture';

type ProjectFixture = { workspace: Entity; project: Entity; path: string };
type Setting = { key: string; value: boolean | number | string; source: string; locked: boolean; inherited: boolean | number | string };
let api: RealAPI;
let fixture: ProjectFixture;
test.beforeEach(async ({ request }) => {
  api = new RealAPI(request);
  await api.login();
  fixture = await api.project(`Redesign ${test.info().title.slice(0, 50)} ${Date.now().toString(36)}`);
});

async function token(scopes = ['logs:ingest', 'logs:read']) {
  return api.call<{ token: string; api_token: { id: string; expires_at: string | null } }>('POST', '/tokens', { project_id: fixture.project.id, name: 'Collector integration', scopes }, 201);
}
async function ingest(request: APIRequestContext, bearer: string, events: unknown[]) {
  const response = await request.post('/api/v1/ingest/batch', { headers: { Authorization: `Bearer ${bearer}` }, data: { events } });
  expect(response.status()).toBe(202);
}
async function waitStored(query: Record<string, unknown>, expected: number) {
  await expect.poll(async () => {
    const response = await api.call<{ total: number }>('POST', '/logs/search', { project_id: fixture.project.id, include_total: true, ...query });
    return response.total;
  }, { timeout: 20_000 }).toBe(expected);
}
async function openProject(page: Page, suffix = '') {
  await signIn(page);
  await page.goto(fixture.path + suffix);
}

for (const kind of ['streams', 'environments'] as const) {
  test(`scoped project settings create and edit ${kind} metadata`, async ({ page }) => {
    await openProject(page, `/settings/${kind}`);
    await page.getByLabel('Technical name', { exact: true }).fill(kind === 'streams' ? 'economy' : 'staging');
    await page.getByRole('button', { name: 'Create', exact: true }).click();
    const technical = kind === 'streams' ? 'economy' : 'staging';
    const details = page.locator('details').filter({ has: page.locator('summary', { hasText: technical }) });
    await details.locator('summary').click();
    await details.getByLabel('Display name').fill('Friendly label');
    await details.getByLabel('Description').fill('Configured through scoped project settings');
    await details.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.locator('summary').filter({ hasText: 'Friendly label' })).toBeVisible();
    const saved = await api.call<{ items: { name: string; display_name: string; description: string }[] }>('GET', `/projects/${fixture.project.id}/${kind}`);
    expect(saved.items.find((item) => item.name === technical)).toMatchObject({ display_name: 'Friendly label', description: 'Configured through scoped project settings' });
    await page.reload();
    await expect(page.locator('summary').filter({ hasText: 'Friendly label' })).toBeVisible();
  });
}

test('runtime overrides inherit after reset and environment locks stay read-only', async ({ page }) => {
  await api.call('PUT', `/workspaces/${fixture.workspace.id}/settings/ingest.project_quota_events`, { value: 700 });
  await api.call('PUT', `/projects/${fixture.project.id}/settings/ingest.project_quota_events`, { value: 900 });
  let settings = await api.call<{ items: Setting[] }>('GET', `/projects/${fixture.project.id}/settings`);
  expect(settings.items.find((item) => item.key === 'ingest.project_quota_events')).toMatchObject({ value: 900, source: 'project', inherited: 700 });
  await api.call('DELETE', `/projects/${fixture.project.id}/settings/ingest.project_quota_events`);
  settings = await api.call<{ items: Setting[] }>('GET', `/projects/${fixture.project.id}/settings`);
  expect(settings.items.find((item) => item.key === 'ingest.project_quota_events')).toMatchObject({ value: 700, source: 'workspace' });
  const instance = await api.call<{ items: Setting[] }>('GET', '/admin/settings');
  const locked = instance.items.find((item) => item.key === 'auth.session_ttl');
  expect(locked).toMatchObject({ locked: true, value: 3600 });
  await api.call('PUT', '/admin/settings/auth.session_ttl', { value: 7200 }, 409);
  await openProject(page);
  await page.goto('/admin/settings');
  await expect(page.getByLabel('Session lifetime')).toBeDisabled();
});

test('catalog templates and tags render while redacted values never reach search or detail', async ({ page, request }) => {
  await openProject(page, '/settings/catalog');
  const eventEditor = page.locator('details').filter({ has: page.locator('summary', { hasText: 'Create event type' }) });
  await eventEditor.locator('summary').click();
  await eventEditor.getByLabel('Technical event name', { exact: true }).fill('money.transfer');
  await eventEditor.getByLabel('Title', { exact: true }).fill('Money transfer');
  await eventEditor.getByLabel('Message template', { exact: false }).fill('{actor.name} paid {amount}');
  await eventEditor.getByLabel('Featured fields (comma separated)', { exact: true }).fill('actor.name, amount');
  await eventEditor.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.locator('summary').filter({ hasText: 'Money transfer' })).toBeVisible();
  const rules = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Field rules', exact: true }) });
  await rules.getByRole('button', { name: 'Add field', exact: true }).click();
  await rules.getByLabel('Field path', { exact: true }).fill('secret');
  await rules.getByLabel('Display name', { exact: true }).fill('Sensitive value');
  await rules.getByLabel('Redact on ingest', { exact: true }).check();
  const ruleSave = page.waitForResponse((response) => response.url().endsWith(`/projects/${fixture.project.id}/field-rules`) && response.request().method() === 'PUT');
  await rules.getByRole('button', { name: 'Save', exact: true }).click();
  expect((await ruleSave).status()).toBe(200);
  const created = await token();
  await ingest(request, created.token, [{ id: 'redesign-redacted', level: 'info', event: 'money.transfer', message: 'Fallback payment', tags: ['economy', 'vip'], attributes: { actor: { name: 'Ada' }, amount: 42, secret: 'never-store-this-value' } }]);
  await waitStored({ tags: ['economy', 'vip'] }, 1);
  const result = await api.call<{ items: { attributes: { secret: string }; tags: string[] }[] }>('POST', '/logs/search', { project_id: fixture.project.id, tags: ['economy'] });
  expect(result.items[0].attributes.secret).toBe('[REDACTED]');
  expect(JSON.stringify(result)).not.toContain('never-store-this-value');
  await page.goto(fixture.path);
  await page.getByLabel('Search', { exact: true }).fill('#economy');
  await page.getByLabel('Search', { exact: true }).press('Enter');
  await expect(page.getByRole('cell').filter({ hasText: 'Money transfer' })).toContainText('Ada paid 42');
  await page.getByRole('button', { name: /Open event from/ }).click();
  const dialog = page.getByRole('dialog', { name: 'Money transfer' });
  await expect(dialog).toContainText('[REDACTED]');
  await expect(dialog).not.toContainText('never-store-this-value');
  await expect(dialog).toContainText('vip');
});

test('attribute filters and histogram share full results and Live receives a new matching event', async ({ page, request }) => {
  const created = await token();
  await ingest(request, created.token, [{ id: 'filter-match', level: 'error', event: 'filter.match', message: 'first matching event', attributes: { actor: { id: 'ada' } }, tags: ['e2e'] }, { id: 'filter-miss', level: 'info', event: 'filter.miss', message: 'another actor', attributes: { actor: { id: 'bob' } } }]);
  await waitStored({}, 2);
  await openProject(page);
  await page.getByRole('button', { name: 'Filters', exact: true }).click();
  await page.getByLabel('Field path', { exact: true }).fill('actor.id');
  await page.getByLabel('Value', { exact: true }).fill('ada');
  const histogramResponse = page.waitForResponse((response) => response.url().endsWith('/api/v1/logs/histogram') && response.request().method() === 'POST' && response.request().postDataJSON().filters?.[0]?.value === 'ada');
  await page.getByRole('button', { name: 'Add filter', exact: true }).click();
  const histogram = await histogramResponse;
  expect(histogram.status()).toBe(200);
  const buckets = await histogram.json() as { items: { count: number }[] };
  expect(buckets.items.reduce((sum, bucket) => sum + bucket.count, 0)).toBe(1);
  await expect(page.getByRole('cell').filter({ hasText: 'filter.match' })).toBeVisible();
  await expect(page.getByRole('cell').filter({ hasText: 'filter.miss' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Go live', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Pause', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByRole('status').filter({ hasText: /^Live$/ })).toBeVisible();
  await ingest(request, created.token, [{ id: 'filter-live', level: 'error', event: 'filter.live', message: 'arrived over Live', attributes: { actor: { id: 'ada' } }, tags: ['e2e'] }]);
  await expect(page.getByRole('cell').filter({ hasText: 'filter.live' })).toBeVisible({ timeout: 20_000 });
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Go live', exact: true })).toHaveAttribute('aria-pressed', 'false');
});

test('saved views drive persisted count, time-series and top panels and usage', async ({ page, request }) => {
  const created = await token();
  await ingest(request, created.token, [{ id: 'insights-a', level: 'error', event: 'invoice.failed', message: 'invoice failed' }, { id: 'insights-b', level: 'info', event: 'invoice.sent', message: 'invoice sent' }]);
  await waitStored({}, 2);
  await openProject(page);
  await page.getByLabel('Search', { exact: true }).fill('failed');
  await page.getByLabel('Search', { exact: true }).press('Enter');
  page.once('dialog', (dialog) => dialog.accept('Failed invoices'));
  await page.getByRole('button', { name: 'Save view', exact: true }).click();
  await expect(page.getByRole('option', { name: 'Failed invoices' })).toBeAttached();
  const saved = await api.call<{ items: { id: string; name: string }[] }>('GET', `/queries?project_id=${fixture.project.id}`);
  expect(saved.items).toHaveLength(1);
  await page.goto(fixture.path + '/dashboards');
  const panelForm = page.locator('details').filter({ has: page.locator('summary', { hasText: 'Add panel' }) });
  await panelForm.locator('summary').click();
  await panelForm.getByLabel('Name', { exact: true }).fill('Failed invoices count');
  await panelForm.getByLabel('Saved view', { exact: true }).selectOption(saved.items[0].id);
  await panelForm.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Failed invoices count' })).toBeVisible();
  for (const type of ['timeseries', 'top']) {
    await api.call('POST', '/dashboard-panels', { project_id: fixture.project.id, saved_query_id: saved.items[0].id, name: `Failed invoices ${type}`, type, field: 'level' }, 201);
  }
  await page.reload();
  for (const type of ['count', 'timeseries', 'top']) await expect(page.getByRole('heading', { name: `Failed invoices ${type}` })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Failed invoices count' }).locator('../..').getByText('1', { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Failed invoices count' })).toBeVisible();
  await page.goto(fixture.path + '/usage');
  await expect(page.getByRole('heading', { name: 'Usage', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'default', exact: true })).toBeVisible();
  const usage = await api.call<{ events: number; bytes: number }>('GET', `/usage?project_id=${fixture.project.id}&range=24h&interval_seconds=3600`);
  expect(usage.events).toBe(2); expect(usage.bytes).toBeGreaterThan(0);
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('invitation fallback displays a direct link and accepts a new member without SMTP', async ({ page, browser }) => {
  const email = `invited-${Date.now().toString(36)}@example.com`;
  await openProject(page);
  await page.goto(`/w/${fixture.workspace.id}/settings/members`);
  const invitations = page.getByRole('region', { name: 'Invitations' });
  await invitations.getByLabel('Email', { exact: true }).fill(email);
  const created = page.waitForResponse((response) => response.url().endsWith(`/workspaces/${fixture.workspace.id}/invitations`) && response.request().method() === 'POST');
  await invitations.getByRole('button', { name: 'Send invitation' }).click();
  const response = await created;
  expect(response.status()).toBe(201);
  const invitation = await response.json() as { emailed: boolean; url: string };
  expect(invitation.emailed).toBe(false); expect(invitation.url).toContain('/invite#token=');
  await expect(invitations.getByText(invitation.url, { exact: true })).toBeVisible();
  const context = await browser.newContext({ baseURL: process.env.E2E_BASE_URL });
  try {
    const invited = await context.newPage();
    await invited.goto(invitation.url);
    await expect(invited.getByRole('heading', { name: 'Workspace invitation' })).toBeVisible();
    await invited.getByLabel('Username', { exact: true }).fill(`invited-${Date.now().toString(36)}`);
    await invited.getByLabel('New password', { exact: true }).fill('invited-account-password');
    await invited.getByRole('button', { name: 'Continue', exact: true }).click();
    await expect(invited.getByText('Completed.', { exact: true })).toBeVisible();
    const members = await api.call<{ items: { email: string; role: string }[] }>('GET', `/workspaces/${fixture.workspace.id}/members`);
    expect(members.items.find((item) => item.email === email)).toMatchObject({ role: 'viewer' });
  } finally { await context.close(); }
});

function totp(secret: string): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const character of secret) bits += alphabet.indexOf(character).toString(2).padStart(5, '0');
  const key = Buffer.from((bits.match(/.{8}/g) ?? []).map((byte) => Number.parseInt(byte, 2)));
  const counter = Buffer.alloc(8); counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30_000)));
  const digest = createHmac('sha1', key).update(counter).digest(); const offset = digest[digest.length - 1] & 15;
  return ((digest.readUInt32BE(offset) & 0x7fffffff) % 1_000_000).toString().padStart(6, '0');
}

test('optional TOTP account enrollment, recovery login and removal use the real backend', async ({ page }) => {
  const username = `totp-${Date.now().toString(36)}`; const password = 'authenticator-test-password';
  await api.call('POST', '/admin/users', { email: `${username}@example.com`, username, password }, 201);
  await signIn(page, username, password); await page.goto('/account');
  const security = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Security', exact: true }) });
  await security.getByLabel('Current password', { exact: false }).fill(password);
  const setup = page.waitForResponse((response) => response.url().endsWith('/auth/totp/setup'));
  await security.getByRole('button', { name: 'Set up authenticator' }).click();
  const secret = (await (await setup).json() as { secret: string }).secret;
  await security.getByLabel('Authenticator or recovery code').fill(totp(secret));
  const confirmed = page.waitForResponse((response) => response.url().endsWith('/auth/totp/confirm'));
  await security.getByRole('button', { name: 'Confirm', exact: true }).click();
  const recovery = (await (await confirmed).json() as { recovery_codes: string[] }).recovery_codes;
  expect(recovery).toHaveLength(10);
  await expect(security.getByText(recovery[0], { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Account', exact: true }).click(); await page.getByRole('button', { name: 'Sign out' }).click();
  await page.getByLabel('Email or username').fill(username); await page.getByLabel('Password', { exact: true }).fill(password); await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByLabel('Authenticator or recovery code').fill(recovery[0]); await page.getByRole('button', { name: 'Confirm', exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/); await page.goto('/account');
  await security.getByLabel('Current password', { exact: false }).fill(password); await security.getByLabel('Authenticator or recovery code').fill(recovery[1]);
  await security.getByRole('button', { name: 'Disable', exact: true }).click();
  await expect(security.getByRole('button', { name: 'Set up authenticator' })).toBeVisible();
});

test('virtual WebAuthn authenticator enrolls, signs in and removes an optional passkey', async ({ page, context }) => {
  const username = `passkey-${Date.now().toString(36)}`; const password = 'passkey-test-password';
  await api.call('POST', '/admin/users', { email: `${username}@example.com`, username, password }, 201);
  const cdp = await context.newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  const authenticator = await cdp.send('WebAuthn.addVirtualAuthenticator', { options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true } });
  try {
    await signIn(page, username, password); await page.goto('/account');
    const security = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Security', exact: true }) });
    await security.getByLabel('Current password', { exact: false }).fill(password); await security.getByLabel('Passkey name').fill('Virtual test passkey');
    await security.getByRole('button', { name: 'Add passkey', exact: true }).click();
    await expect(security.getByText('Virtual test passkey', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Account', exact: true }).click(); await page.getByRole('button', { name: 'Sign out' }).click();
    await page.getByLabel('Email or username').fill(username); await page.getByRole('button', { name: 'Sign in with a passkey' }).click();
    await expect(page).not.toHaveURL(/\/login/); await page.goto('/account');
    await security.getByLabel('Current password', { exact: false }).fill(password); await security.getByRole('button', { name: 'Delete', exact: true }).click();
    await expect(security.getByText('Virtual test passkey', { exact: true })).toHaveCount(0);
    const credentials = await cdp.send('WebAuthn.getCredentials', { authenticatorId: authenticator.authenticatorId });
    expect(credentials.credentials).toHaveLength(1);
  } finally { await cdp.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId: authenticator.authenticatorId }); await cdp.detach(); }
});

test('token rotation replaces credentials immediately and preserves its expiry', async ({ page, request }) => {
  const expiry = new Date(Date.now() + 3_600_000).toISOString();
  const original = await api.call<{ token: string; api_token: { id: string } }>('POST', '/tokens', { project_id: fixture.project.id, name: 'Rotating integration', scopes: ['logs:ingest'], expires_at: expiry }, 201);
  await openProject(page, '/settings/tokens');
  const row = page.getByRole('listitem').filter({ hasText: 'Rotating integration' });
  const rotated = page.waitForResponse((response) => response.url().endsWith(`/tokens/${original.api_token.id}/rotate`));
  page.once('dialog', (dialog) => dialog.accept()); await row.getByRole('button', { name: 'Rotate', exact: true }).click();
  const replacement = await (await rotated).json() as { token: string; api_token: { expires_at: string } };
  expect(Date.parse(replacement.api_token.expires_at)).toBe(Date.parse(expiry));
  const oldResult = await request.get('/api/v1/tokens/current', { headers: { Authorization: `Bearer ${original.token}` } }); expect(oldResult.status()).toBe(401);
  const newResult = await request.get('/api/v1/tokens/current', { headers: { Authorization: `Bearer ${replacement.token}` } }); expect(newResult.status()).toBe(200);
  await expect(page.getByText(replacement.token, { exact: true })).toBeVisible();
});
