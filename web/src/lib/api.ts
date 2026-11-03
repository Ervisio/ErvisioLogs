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
export type ApiToken = {
  id: string;
  project_id: string;
  name: string;
  prefix: string;
  scopes: string[];
  created_at: string;
  expires_at: string | null;
  last_used_at: string | null;
  revoked_at: string | null;
};
export type Member = { user_id: string; email: string; username: string; role: string; joined_at: string };
export type LogItem = {
  id: string;
  timestamp: string;
  ingested_at: string;
  level: string;
  severity_number: number;
  event: string;
  message: string;
  stream: string;
  environment: string;
  service: string;
  host: string;
  resource: string;
  trace_id: string;
  span_id: string;
  attributes: Record<string, unknown> | null;
};
export type AttrFilter = { field: string; op: string; value?: string };
export type SearchRequest = {
  project_id: string;
  from?: string;
  to?: string;
  query?: string;
  levels?: string[];
  streams?: string[];
  environments?: string[];
  filters?: AttrFilter[];
  limit?: number;
  cursor?: string;
  include_total?: boolean;
};
export type SearchResult = {
  items: LogItem[] | null;
  next_cursor: string | null;
  total?: number;
  level_counts?: Record<string, number>;
  searched_tiers: string[];
  partial: boolean;
};
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
  searchLogs: (body: SearchRequest, signal?: AbortSignal) => request<SearchResult>('POST', '/logs/search', body, signal),
  tokens: (projectId: string, signal?: AbortSignal) =>
    list<ApiToken>(`/tokens?project_id=${encodeURIComponent(projectId)}`, signal),
  createToken: (body: { project_id: string; name: string; scopes: string[] }) =>
    request<{ token: string; api_token: ApiToken }>('POST', '/tokens', body),
  revokeToken: (id: string) => request<void>('DELETE', `/tokens/${encodeURIComponent(id)}`),
  members: (workspaceId: string, signal?: AbortSignal) =>
    list<Member>(`/workspaces/${encodeURIComponent(workspaceId)}/members`, signal),
  addMember: (workspaceId: string, body: { email: string; role: string }) =>
    request<Member>('POST', `/workspaces/${encodeURIComponent(workspaceId)}/members`, body),
  setMemberRole: (workspaceId: string, userId: string, role: string) =>
    request<void>('PATCH', `/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(userId)}`, { role }),
  removeMember: (workspaceId: string, userId: string) =>
    request<void>('DELETE', `/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(userId)}`),
};

/** URL of the live tail. EventSource sends the session cookie; GET needs no CSRF header. */
export const tailUrl = (params: Record<string, string | undefined>) => {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) q.set(k, v);
  return `/api/v1/logs/tail?${q.toString()}`;
};

/** Browser navigation, not fetch: the OAuth flow ends with a redirect back to the app. */
export const oauthStartUrl = (provider: string) => `/api/v1/auth/oauth/${encodeURIComponent(provider)}/start`;
