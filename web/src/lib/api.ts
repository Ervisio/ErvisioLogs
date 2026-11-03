/** Thin client for /api/v1: cookie session, CSRF header on unsafe calls, RFC 7807 errors. */

export type User = {
  id: string;
  email: string;
  username: string;
  email_verified: boolean;
  is_instance_admin: boolean;
  status: string;
  created_at: string;
};
export type Workspace = { id: string; slug: string; name: string; created_at: string; role: string };
export type Project = { id: string; workspace_id: string; slug: string; name: string; created_at: string };
export type Environment = { id: string; project_id: string; name: string; created_at: string };
export type Stream = { id: string; project_id: string; name: string; type: string; created_at: string };
export type Session = { user: User; workspaces: Workspace[]; csrf_token: string };
export type Providers = {
  local: boolean;
  providers: string[];
  open_registration: boolean;
  setup_required: boolean;
};

export class ApiError extends Error {
  readonly status: number;
  /** Seconds, from the Retry-After header. */
  readonly retryAfter: number | null;
  constructor(status: number, detail: string, retryAfter: number | null) {
    super(detail);
    this.status = status;
    this.retryAfter = retryAfter;
  }
}

let csrfToken: string | null = null;
export const setCsrfToken = (token: string | null) => {
  csrfToken = token;
};

async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json, application/problem+json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken;

  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    credentials: 'same-origin',
    signal,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return undefined as T;

  const data: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const retry = Number(res.headers?.get('Retry-After'));
    const detail = (data as { detail?: string } | null)?.detail ?? res.statusText;
    throw new ApiError(res.status, detail, Number.isFinite(retry) && retry > 0 ? retry : null);
  }
  return data as T;
}

const list = async <T>(path: string, signal?: AbortSignal) =>
  (await request<{ items: T[] | null }>('GET', path, undefined, signal)).items ?? [];

export const api = {
  providers: (signal?: AbortSignal) => request<Providers>('GET', '/auth/providers', undefined, signal),
  me: (signal?: AbortSignal) => request<Session>('GET', '/auth/me', undefined, signal),
  login: (body: { login: string; password: string }) => request<Session>('POST', '/auth/login', body),
  register: (body: { email: string; username: string; password: string; setup_code?: string }) =>
    request<Session>('POST', '/auth/register', body),
  logout: () => request<void>('POST', '/auth/logout'),
  createWorkspace: (name: string) => request<Workspace>('POST', '/workspaces', { name }),
  createProject: (workspaceId: string, name: string) =>
    request<Project>('POST', '/projects', { workspace_id: workspaceId, name }),
  projects: (workspaceId: string, signal?: AbortSignal) =>
    list<Project>(`/projects?workspace_id=${encodeURIComponent(workspaceId)}`, signal),
  environments: (projectId: string, signal?: AbortSignal) =>
    list<Environment>(`/projects/${encodeURIComponent(projectId)}/environments`, signal),
  streams: (projectId: string, signal?: AbortSignal) =>
    list<Stream>(`/projects/${encodeURIComponent(projectId)}/streams`, signal),
};

/** Browser navigation, not fetch: the OAuth flow ends with a redirect back to the app. */
export const oauthStartUrl = (provider: string) => `/api/v1/auth/oauth/${encodeURIComponent(provider)}/start`;
