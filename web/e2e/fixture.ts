import { expect, type APIRequestContext, type Page } from '@playwright/test';

const configuredCode = process.env.E2E_SETUP_CODE;
if (!configuredCode) throw new Error('E2E_SETUP_CODE is not set: run scripts/e2e.sh');
export const setupCode = configuredCode;
export const administrator = { email: 'admin-e2e@example.com', username: 'admin-e2e', password: `${setupCode}-account-password` };
export type Entity = { id: string; name: string; workspace_id?: string };
export class RealAPI {
  private csrf = '';
  constructor(private readonly request: APIRequestContext) {}
  async call<T>(method: string, path: string, data?: unknown, status = 200): Promise<T> {
    const response = await this.request.fetch(`/api/v1${path}`, { method, data, headers: this.csrf ? { 'X-CSRF-Token': this.csrf } : {} });
    expect(response.status(), `${method} ${path}: ${response.status() >= 400 ? await response.text() : ''}`).toBe(status);
    return status === 204 ? undefined as T : await response.json() as T;
  }
  async login(username = administrator.username, password = administrator.password) {
    const session = await this.call<{ csrf_token: string }>('POST', '/auth/login', { login: username, password });
    this.csrf = session.csrf_token;
  }
  async project(name: string) {
    const workspace = await this.call<Entity>('POST', '/workspaces', { name }, 201);
    const project = await this.call<Entity>('POST', '/projects', { workspace_id: workspace.id, name }, 201);
    return { workspace, project, path: `/w/${workspace.id}/p/${project.id}` };
  }
}
export async function signIn(page: Page, username = administrator.username, password = administrator.password) {
  await page.goto('/login');
  await page.getByLabel('Email or username').fill(username);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).not.toHaveURL(/\/login(?:[?#]|$)/);
}
