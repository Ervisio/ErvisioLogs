import { expect, test, type Page } from '@playwright/test';

/**
 * One instance, one story: the first administrator creates a workspace and a project, gets a token
 * from the setup wizard, sends an event with it and finds that event in search. Later tests reuse the
 * account, so this file runs in order (workers: 1).
 */
// A missing code fails the run: a skipped end-to-end test must never look like a pass.
const setupCode = process.env.E2E_SETUP_CODE;
if (!setupCode) throw new Error('E2E_SETUP_CODE is not set: run scripts/e2e.sh');
const stamp = Date.now().toString(36);
const admin = { email: `admin-${stamp}@example.com`, username: `admin-${stamp}`, password: `pw-${stamp}-long-enough` };
const projectName = 'Game server';

test.describe.configure({ mode: 'serial' });

async function signIn(page: Page) {
  await page.goto('/login');
  await page.getByLabel('Email or username').fill(admin.username);
  await page.getByLabel('Password', { exact: true }).fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  // The project rail hides in the drawer on phones; the explorer heading shows on every size.
  await expect(page.getByRole('heading', { name: 'All streams', level: 1 })).toBeVisible();
}

test('first run: setup, project, token, ingest, search', async ({ page, request }) => {

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Create the first administrator' })).toBeVisible();
  await page.getByLabel('Setup code').fill(setupCode);
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Username').fill(admin.username);
  await page.getByLabel('Password', { exact: true }).fill(admin.password);
  await page.getByRole('button', { name: 'Create administrator' }).click();

  await page.getByLabel('Workspace name').fill('Acme');
  await page.getByRole('button', { name: 'Create workspace' }).click();
  await page.getByLabel('Project name').fill(projectName);
  await page.getByRole('button', { name: 'Create project' }).click();

  // An empty project opens on the setup wizard.
  await expect(page.getByRole('heading', { name: `Send the first event to ${projectName}` })).toBeVisible();
  await page.getByRole('button', { name: 'Create token' }).click();
  await expect(page.getByText('Token created.')).toBeVisible();

  // The wizard's curl command carries the plaintext token; a server would use it the same way.
  const command = await page.locator('pre, code').filter({ hasText: 'Authorization: Bearer' }).first().innerText();
  const token = /Bearer (\S+?)["\\\s]/.exec(command)?.[1];
  expect(token, 'token in the curl command').toMatch(/^evl_/);

  await page.getByRole('button', { name: 'Send a test event' }).click();
  // The live tail sees the event and the explorer replaces the wizard.
  await expect(page.getByRole('cell', { name: 'ervisio.test' }).first()).toBeVisible({ timeout: 20_000 });

  // A second event straight from "a server": token only, no cookie.
  const res = await request.post('/api/v1/ingest', {
    headers: { Authorization: `Bearer ${token}` },
    data: { level: 'error', event: 'payment.failed', message: `card declined ${stamp}`, attributes: { user: 'ada' } },
  });
  expect(res.status()).toBe(202);

  // Search finds it by free text and by a key=value filter; flushing to storage takes a moment.
  const search = page.getByPlaceholder('Search messages, or filter with key=value');
  // Typing the same query again does not search again, so each attempt clears the box first.
  await expect(async () => {
    await search.fill('');
    await search.press('Enter');
    await search.fill(`user=ada ${stamp}`);
    await search.press('Enter');
    await expect(page.getByRole('cell', { name: 'payment.failed' })).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 20_000 });
  await expect(page.getByRole('cell', { name: 'ervisio.test' })).toHaveCount(0);

  // The event opens in the detail dialog.
  await page.getByRole('cell', { name: 'payment.failed' }).click();
  const dialog = page.getByRole('dialog', { name: 'payment.failed' });
  await expect(dialog).toContainText(`card declined ${stamp}`);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();

  // The wizard's token shows up on the tokens page.
  await page.goto(page.url().replace(/\/projects\/([^/?]+).*/, '/projects/$1/tokens'));
  await expect(page.getByText('First server', { exact: true })).toBeVisible();
});

test('mobile: no sideways scroll, navigation in the drawer', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 760 });
  await signIn(page);

  const overflow = () => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(await overflow()).toBeLessThanOrEqual(0);

  // Tokens and users sit under Settings: the drawer lists them once you are inside it.
  const sections = page.getByRole('complementary', { name: 'Project sections' });
  await page.getByRole('button', { name: 'Open navigation' }).click();
  await sections.getByRole('link', { name: 'Settings' }).click();
  if (!(await sections.getByRole('link', { name: 'Users' }).isVisible())) {
    await page.getByRole('button', { name: 'Open navigation' }).click();
  }
  await expect(sections.getByRole('link', { name: 'API tokens' })).toBeVisible();
  await sections.getByRole('link', { name: 'Users' }).click();
  await expect(page.getByRole('heading', { name: 'Members' })).toBeVisible();
  await expect(page.getByText(admin.email)).toBeVisible();
  expect(await overflow()).toBeLessThanOrEqual(0);

  // Targets on touch screens stay at least 44px tall.
  const leave = await page.getByRole('button', { name: 'Leave workspace' }).boundingBox();
  expect(leave?.height ?? 0).toBeGreaterThanOrEqual(44);
});

test('dark theme persists across reloads', async ({ page }) => {
  await signIn(page);
  await page.getByRole('button', { name: 'Account', exact: true }).click();
  const before = await page.evaluate(() => document.documentElement.dataset.theme ?? '');
  await page.getByRole('button', { name: 'Switch theme' }).click();
  const after = await page.evaluate(() => document.documentElement.dataset.theme ?? '');
  expect(after).not.toBe(before);
  await page.reload();
  await expect.poll(() => page.evaluate(() => document.documentElement.dataset.theme ?? '')).toBe(after);
});
