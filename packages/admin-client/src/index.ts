export type Request = <T>(method: string, path: string, body?: unknown, signal?: AbortSignal) => Promise<T>;
export type Page<T> = { items: T[] | null; next_cursor?: string | null };
export type Setting = {
  key: string;
  type: 'bool' | 'int' | 'string' | 'duration' | 'choice';
  value: unknown;
  inherited: unknown;
  source: string;
  locked: boolean;
  env?: string;
  min?: number;
  max?: number;
  options?: string[];
};
export type User = {
  id: string;
  email: string;
  username: string;
  status: string;
  is_instance_admin: boolean;
  email_verified: boolean;
  created_at: string;
};
export type Workspace = { id: string; name: string; slug: string; created_at: string; role?: string };
export type Project = Workspace & { workspace_id: string };
export type Metadata = { display_name: string; description: string; color: string };
export type Environment = Metadata & { id: string; project_id: string; name: string; created_at: string };
export type Stream = Environment & { type: string };
export type Member = { user_id: string; email: string; username: string; role: string; joined_at: string };
export type Invitation = { id: string; email: string; role: string; expires_at: string };
export type Session = {
  id: string;
  ip: string;
  user_agent: string;
  created_at: string;
  last_seen_at: string | null;
  expires_at: string;
};
export type Token = {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  expires_at: string | null;
  revoked_at: string | null;
  last_used_at: string | null;
};
export type ProviderInput = {
  name: string;
  type: 's3' | 'sftp' | 'local';
  config: Record<string, unknown>;
  secret_file: string | null;
};
export type Provider = ProviderInput & { id: string; active: boolean; status: string; last_error: string | null };
export type Storage = {
  providers: Provider[] | null;
  segments: Record<string, { count: number; bytes: number }>;
  archiver_enabled: boolean;
  usage?: {
    hot_bytes: number;
    cache_bytes: number;
    max_bytes: number;
    high_pct: number;
    low_pct: number;
    over_capacity: boolean;
  };
};
export type RetentionInput = {
  local_age_hours?: number | null;
  global_age_hours?: number | null;
  cache_max_bytes?: number;
  high_pct?: number;
  low_pct?: number;
};
export type Retention = RetentionInput & { scope_type: string; scope_id: string };
export type Health = {
  status: string;
  postgres: { ok: boolean; error: string | null };
  clickhouse: { ok: boolean; error: string | null };
  wal: { bytes: number; max_bytes: number; pending_records: number; lag_seconds: number; segments: number } | null;
  upload: {
    waiting: { count: number; bytes: number };
    failed: { count: number; bytes: number };
    last_verified_at: string | null;
    oldest_unarchived_event_at: string | null;
  };
  archive: { enabled: boolean; provider: { name: string; status: string; last_error: string | null } | null };
};
export type Audit = {
  id: string;
  action: string;
  actor_name: string;
  target_type: string;
  target_id: string;
  created_at: string;
  detail: unknown;
};
export const INSTANCE_SCOPE = '00000000-0000-0000-0000-000000000000';
const id = encodeURIComponent;
const query = (values: Record<string, string | undefined>) => {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(values)) if (v) p.set(k, v);
  return p.size ? `?${p}` : '';
};

export function createAdminClient(request: Request) {
  const list = async <T>(path: string) => (await request<Page<T>>('GET', path)).items ?? [];
  return {
    request,
    health: () => request<Health>('GET', '/admin/health'),
    settings: (scope = '/admin') => list<Setting>(`${scope}/settings`),
    setSetting: (scope: string, key: string, value: unknown) =>
      request<Setting>('PUT', `${scope}/settings/${id(key)}`, { value }),
    resetSetting: (scope: string, key: string) => request<Setting>('DELETE', `${scope}/settings/${id(key)}`),
    users: (q = '', cursor = '') => request<Page<User>>('GET', `/admin/users${query({ q, cursor, limit: '100' })}`),
    createUser: (input: { email: string; username: string; password?: string }) =>
      request<User>('POST', '/admin/users', input),
    updateUser: (user: string, input: { status?: string; is_instance_admin?: boolean }) =>
      request<User>('PATCH', `/admin/users/${id(user)}`, input),
    deleteUser: (user: string) => request<void>('DELETE', `/admin/users/${id(user)}`),
    resetPassword: (user: string, password: string) =>
      request<void>('PUT', `/admin/users/${id(user)}/password`, { password }),
    sessions: (user: string) => list<Session>(`/admin/users/${id(user)}/sessions`),
    revokeSessions: (user: string, session?: string) =>
      request<void>('DELETE', `/admin/users/${id(user)}/sessions${session ? `/${id(session)}` : ''}`),
    workspaces: () => list<Workspace>('/workspaces'),
    createWorkspace: (input: { name: string; slug: string; owner_id?: string }) =>
      request<Workspace>('POST', '/workspaces', input),
    renameWorkspace: (workspace: string, name: string) =>
      request<Workspace>('PATCH', `/workspaces/${id(workspace)}`, { name }),
    deleteWorkspace: (workspace: string, confirm: string) =>
      request<void>('DELETE', `/workspaces/${id(workspace)}`, { confirm }),
    members: (workspace: string) => list<Member>(`/workspaces/${id(workspace)}/members`),
    addMember: (workspace: string, email: string, role: string) =>
      request<Member>('POST', `/workspaces/${id(workspace)}/members`, { email, role }),
    updateMember: (workspace: string, user: string, role: string) =>
      request<Member>('PATCH', `/workspaces/${id(workspace)}/members/${id(user)}`, { role }),
    removeMember: (workspace: string, user: string) =>
      request<void>('DELETE', `/workspaces/${id(workspace)}/members/${id(user)}`),
    invitations: (workspace: string) => list<Invitation>(`/workspaces/${id(workspace)}/invitations`),
    invite: (workspace: string, email: string, role: string) =>
      request<Invitation & { url?: string; mail_error?: string }>('POST', `/workspaces/${id(workspace)}/invitations`, {
        email,
        role,
      }),
    revokeInvitation: (workspace: string, invitation: string) =>
      request<void>('DELETE', `/workspaces/${id(workspace)}/invitations/${id(invitation)}`),
    projects: (workspace: string) => list<Project>(`/projects${query({ workspace_id: workspace })}`),
    createProject: (input: { workspace_id: string; name: string; slug: string }) =>
      request<Project>('POST', '/projects', input),
    renameProject: (project: string, name: string) => request<Project>('PATCH', `/projects/${id(project)}`, { name }),
    deleteProject: (project: string, confirm: string) =>
      request<void>('DELETE', `/projects/${id(project)}`, { confirm }),
    environments: (project: string) => list<Environment>(`/projects/${id(project)}/environments`),
    streams: (project: string) => list<Stream>(`/projects/${id(project)}/streams`),
    createEnvironment: (project: string, input: { name: string } & Partial<Metadata>) =>
      request<Environment>('POST', `/projects/${id(project)}/environments`, input),
    createStream: (project: string, input: { name: string } & Partial<Metadata>) =>
      request<Stream>('POST', `/projects/${id(project)}/streams`, input),
    updateMetadata: (project: string, kind: 'streams' | 'environments', item: string, input: Partial<Metadata>) =>
      request<Environment>('PATCH', `/projects/${id(project)}/${kind}/${id(item)}`, input),
    deleteMetadata: (project: string, kind: 'streams' | 'environments', item: string) =>
      request<void>('DELETE', `/projects/${id(project)}/${kind}/${id(item)}`),
    tokens: (project: string) => list<Token>(`/tokens${query({ project_id: project })}`),
    createToken: (input: { project_id: string; name: string; scopes: string[]; expires_at: string | null }) =>
      request<{ token: string; api_token: Token }>('POST', '/tokens', input),
    revokeToken: (token: string) => request<void>('DELETE', `/tokens/${id(token)}`),
    storage: () => request<Storage>('GET', '/admin/storage'),
    createProvider: (input: ProviderInput) => request<Provider>('POST', '/admin/storage/providers', input),
    updateProvider: (provider: string, input: ProviderInput) =>
      request<Provider>('PUT', `/admin/storage/providers/${id(provider)}`, input),
    deleteProvider: (provider: string) => request<void>('DELETE', `/admin/storage/providers/${id(provider)}`),
    testProvider: (provider: string) => request<unknown>('POST', `/admin/storage/providers/${id(provider)}/test`),
    activateProvider: (provider: string | null) =>
      request<void>('PUT', '/admin/storage/active', { provider_id: provider }),
    secrets: () => list<string>('/admin/storage/secrets'),
    putSecret: (name: string, value: string) => request<void>('PUT', `/admin/storage/secrets/${id(name)}`, { value }),
    retention: () => list<Retention>('/admin/retention'),
    setRetention: (scope: string, scopeId: string, input: RetentionInput) =>
      request<Retention>('PUT', `/admin/retention/${id(scope)}/${id(scopeId)}`, input),
    resetRetention: (scope: string, scopeId: string) =>
      request<void>('DELETE', `/admin/retention/${id(scope)}/${id(scopeId)}`),
    audit: (workspace?: string, cursor?: string) =>
      request<Page<Audit>>(
        'GET',
        `${workspace ? `/workspaces/${id(workspace)}` : '/admin'}/audit${query({ cursor, limit: '100' })}`,
      ),
  };
}
export type AdminClient = ReturnType<typeof createAdminClient>;

export class AdminError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'AdminError';
    this.status = status;
  }
}
export function decodeResponse<T>(status: number, body: string): T {
  let value: unknown;
  if (body) {
    try {
      value = JSON.parse(body);
    } catch {
      throw new AdminError(status, `Invalid JSON response (${status})`);
    }
  }
  if (status < 200 || status >= 300) {
    const problem = typeof value === 'object' && value !== null ? value : {};
    const detail =
      'detail' in problem && typeof problem.detail === 'string'
        ? problem.detail
        : 'title' in problem && typeof problem.title === 'string'
          ? problem.title
          : `HTTP ${status}`;
    throw new AdminError(status, detail);
  }
  return value as T;
}
export function createFetchRequest(options: { baseUrl: string; token?: string; csrfToken?: () => string }): Request {
  const base = new URL(options.baseUrl);
  if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.search || base.hash)
    throw new Error('Enter an HTTP(S) instance URL without credentials, query or fragment');
  return async <T>(method: string, path: string, body?: unknown, signal?: AbortSignal) => {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (options.token) headers.Authorization = `Bearer ${options.token}`;
    const csrf = options.csrfToken?.();
    if (csrf && !['GET', 'HEAD'].includes(method)) headers['X-CSRF-Token'] = csrf;
    const response = await fetch(`${base.href.replace(/\/$/, '')}/api/v1${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: options.token ? 'omit' : 'include',
      signal: signal ?? AbortSignal.timeout(30000),
    });
    return decodeResponse<T>(response.status, await response.text());
  };
}
