import { expect, test, type Page } from '@playwright/test';

import { administrator as admin, setupCode } from './fixture';

const stamp = Date.now().toString(36);
const projectName = 'Game server';

test.describe.configure({ mode: 'serial' });

async function signIn(page: Page) {
  await page.goto('/login');
  await page.getByLabel('Email or username').fill(admin.username);
  await page.getByLabel('Password', { exact: true }).fill(admin.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  // The project rail hides in the drawer on phones; the explorer heading shows on every size.
  await expect(page.getByRole('heading', { name: projectName, level: 1 })).toBeVisible();
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
  await expect(page.getByRole('cell').filter({ hasText: 'ervisio.test' }).first()).toBeVisible({ timeout: 20_000 });

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
    await expect(page.getByRole('cell').filter({ hasText: 'payment.failed' })).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 20_000 });
  await expect(page.getByRole('cell').filter({ hasText: 'ervisio.test' })).toHaveCount(0);

  // The event opens in the detail dialog.
  await page.getByRole('cell').filter({ hasText: 'payment.failed' }).click();
  const dialog = page.getByRole('dialog', { name: 'payment.failed' });
  await expect(dialog).toContainText(`card declined ${stamp}`);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();

  // The wizard's token shows up on the tokens page.
  const scoped = new URL(page.url()).pathname.match(/^\/w\/([^/]+)\/p\/([^/]+)/);
  expect(scoped).not.toBeNull();
  await page.goto(`/w/${scoped?.[1]}/p/${scoped?.[2]}/settings/tokens`);
  await expect(page.getByText(/^First server \(/)).toBeVisible();
});

test('mobile: no sideways scroll, navigation in the drawer', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 760 });
  await signIn(page);

  const overflow = () => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(await overflow()).toBeLessThanOrEqual(0);

  const sections = page.getByRole('complementary', { name: 'Project sections' });
  await page.getByRole('button', { name: 'Open navigation' }).click();
  await sections.getByRole('link', { name: 'Settings', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Project settings' })).toBeVisible();
  const tabs = page.getByRole('combobox', { name: 'Project settings' });
  await tabs.selectOption({ label: 'Tokens' });
  await expect(page).toHaveURL(/\/settings\/tokens$/);
  await expect(page.getByRole('heading', { name: 'Active tokens', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Open navigation' }).click();
  await sections.getByRole('link', { name: 'Workspace settings' }).click();
  await page.getByRole('combobox', { name: 'Workspace settings' }).selectOption({ label: 'Members and invitations' });
  await expect(page.getByRole('heading', { name: 'Members' })).toBeVisible();
  await expect(page.getByText(admin.email, { exact: true })).toBeVisible();
  expect(await overflow()).toBeLessThanOrEqual(0);

  // Targets on touch screens stay at least 44px tall.
  const leave = await page.getByRole('button', { name: 'Leave workspace' }).boundingBox();
  expect(leave?.height ?? 0).toBeGreaterThanOrEqual(44);
});

test('all theme choices persist across reloads', async ({ page }) => {
  await signIn(page);
  for (const theme of ['light', 'amoled', 'dark']) {
    await page.getByRole('button', { name: 'Account', exact: true }).click();
    await page.getByRole('combobox', { name: 'Theme' }).selectOption(theme);
    await page.reload();
    await expect.poll(() => page.evaluate(() => document.documentElement.dataset.theme ?? '')).toBe(theme);
  }
});
