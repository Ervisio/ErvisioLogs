/** Thin client for /api/v1: cookie session, CSRF header on unsafe calls, RFC 7807 errors. */
import { slugFor } from './slug';

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
export type Metadata = { display_name?: string; description?: string; color?: string };
export type Environment = Metadata & { id: string; project_id: string; name: string; created_at: string };
export type Stream = Metadata & { id: string; project_id: string; name: string; type: string; created_at: string };
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
  tags?: string[];
  tier?: string;
};
export type AttrFilter = { field: string; op: string; value?: string };
export type SearchRequest = {
  project_id: string;
  from?: string;
  to?: string;
  query?: string;
  levels?: string[];
  streams?: string[];
  events?: string[];
  services?: string[];
  trace_id?: string;
  environments?: string[];
  filters?: AttrFilter[];
  tags?: string[];
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
export type ProviderType = 's3' | 'sftp' | 'local';
export type StorageProvider = {
  id: string;
  name: string;
  type: ProviderType;
  config: Record<string, unknown>;
  secret_file: string | null;
  active: boolean;
  status: 'unknown' | 'ok' | 'error';
  last_error: string | null;
  last_checked_at: string | null;
  created_at: string;
  updated_at: string;
};
export type StorageUsage = {
  hot_bytes: number;
  cache_bytes: number;
  max_bytes: number;
  high_pct: number;
  low_pct: number;
  over_capacity: boolean;
};
export type StorageOverview = {
  providers: StorageProvider[] | null;
  segments: Record<string, { count: number; bytes: number }>;
  archiver_enabled: boolean;
  usage?: StorageUsage;
};
export type ScopeType = 'instance' | 'workspace' | 'project' | 'stream';
export type RetentionPolicy = {
  scope_type: ScopeType;
  scope_id: string;
  local_age_hours: number | null;
  global_age_hours: number | null;
  cache_max_bytes?: number;
  high_pct?: number;
  low_pct?: number;
  updated_by: string | null;
  updated_at: string;
};
export type RetentionInput = Partial<
  Pick<RetentionPolicy, 'local_age_hours' | 'global_age_hours' | 'cache_max_bytes' | 'high_pct' | 'low_pct'>
>;
export type ProviderInput = {
  name: string;
  type?: ProviderType;
  config: Record<string, unknown>;
  secret_file: string | null;
};
export const INSTANCE_SCOPE = '00000000-0000-0000-0000-000000000000';

export type SavedFilter = {
  range?: string;
  query?: string;
  levels?: string[];
  streams?: string[];
  events?: string[];
  services?: string[];
  trace_id?: string;
  environments?: string[];
  filters?: AttrFilter[];
  tags?: string[];
};
export type SavedQuery = {
  id: string;
  workspace_id: string;
  project_id: string;
  name: string;
  query: SavedFilter;
  created_by: string | null;
  created_at: string;
  updated_at: string;
};
export type ChannelFormat = 'ervisio' | 'slack' | 'discord';
export type NotificationChannel = {
  id: string;
  workspace_id: string;
  name: string;
  type: 'webhook';
  format: ChannelFormat;
  enabled: boolean;
  url_hint: string;
  secret?: string;
  last_status: string | null;
  last_error: string | null;
  last_sent_at: string | null;
  created_at: string;
  updated_at: string;
};
export type AlertRuleInput = {
  project_id: string;
  name: string;
  query: SavedFilter;
  window_seconds: number;
  threshold: number;
  interval_seconds?: number;
  channel_ids?: string[];
  enabled?: boolean;
};
export type AlertRule = Required<AlertRuleInput> & {
  id: string;
  workspace_id: string;
  state: 'ok' | 'firing' | 'error';
  last_value: number | null;
  last_error: string | null;
  last_evaluated_at: string | null;
  next_eval_at: string;
  created_at: string;
  updated_at: string;
};
export type AlertEvent = {
  id: string;
  rule_id: string;
  status: 'firing' | 'resolved' | 'error';
  value: number | null;
  detail: string | null;
  deliveries: { channel_id: string; ok: boolean; error?: string }[];
  created_at: string;
};
export type ExportRequest = Omit<SearchRequest, 'limit' | 'cursor' | 'include_total'> & { format: 'ndjson' | 'csv' };

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

export async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
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
  login: (body: { login: string; password: string }) =>
    request<Session | { mfa_required: true; challenge: string }>('POST', '/auth/login', body),
  register: (body: { email: string; username: string; password: string; setup_code?: string }) =>
    request<Session>('POST', '/auth/register', body),
  logout: () => request<void>('POST', '/auth/logout'),
  createWorkspace: (name: string) =>
    request<Workspace>('POST', '/workspaces', { name, slug: slugFor(name, 'workspace') }),
  createProject: (workspaceId: string, name: string) =>
    request<Project>('POST', '/projects', { workspace_id: workspaceId, name, slug: slugFor(name, 'project') }),
  projects: (workspaceId: string, signal?: AbortSignal) =>
    list<Project>(`/projects?workspace_id=${encodeURIComponent(workspaceId)}`, signal),
  environments: (projectId: string, signal?: AbortSignal) =>
    list<Environment>(`/projects/${encodeURIComponent(projectId)}/environments`, signal),
  streams: (projectId: string, signal?: AbortSignal) =>
    list<Stream>(`/projects/${encodeURIComponent(projectId)}/streams`, signal),
  searchLogs: (body: SearchRequest, signal?: AbortSignal) =>
    request<SearchResult>('POST', '/logs/search', body, signal),
  tokens: (projectId: string, signal?: AbortSignal) =>
    list<ApiToken>(`/tokens?project_id=${encodeURIComponent(projectId)}`, signal),
  createToken: (body: { project_id: string; name: string; scopes: string[]; expires_at?: string }) =>
    request<{ token: string; api_token: ApiToken }>('POST', '/tokens', body),
  revokeToken: (id: string) => request<void>('DELETE', `/tokens/${encodeURIComponent(id)}`),
  members: (workspaceId: string, signal?: AbortSignal) =>
    list<Member>(`/workspaces/${encodeURIComponent(workspaceId)}/members`, signal),
  addMember: (workspaceId: string, body: { email: string; role: string }) =>
    request<Member>('POST', `/workspaces/${encodeURIComponent(workspaceId)}/members`, body),
  setMemberRole: (workspaceId: string, userId: string, role: string) =>
    request<void>('PATCH', `/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(userId)}`, {
      role,
    }),
  removeMember: (workspaceId: string, userId: string) =>
    request<void>('DELETE', `/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(userId)}`),
  storage: (signal?: AbortSignal) => request<StorageOverview>('GET', '/admin/storage', undefined, signal),
  createProvider: (body: ProviderInput) => request<StorageProvider>('POST', '/admin/storage/providers', body),
  updateProvider: (id: string, body: ProviderInput) =>
    request<StorageProvider>('PUT', `/admin/storage/providers/${encodeURIComponent(id)}`, body),
  deleteProvider: (id: string) => request<void>('DELETE', `/admin/storage/providers/${encodeURIComponent(id)}`),
  testProvider: (id: string) =>
    request<StorageProvider>('POST', `/admin/storage/providers/${encodeURIComponent(id)}/test`),
  setActiveProvider: (id: string | null) => request<void>('PUT', '/admin/storage/active', { provider_id: id }),
  secrets: (signal?: AbortSignal) => list<string>('/admin/storage/secrets', signal),
  putSecret: (name: string, value: string) =>
    request<void>('PUT', `/admin/storage/secrets/${encodeURIComponent(name)}`, { value }),
  retention: (signal?: AbortSignal) => list<RetentionPolicy>('/admin/retention', signal),
  putRetention: (scope: ScopeType, id: string, body: RetentionInput) =>
    request<RetentionPolicy>('PUT', `/admin/retention/${scope}/${encodeURIComponent(id)}`, body),
  deleteRetention: (scope: ScopeType, id: string) =>
    request<void>('DELETE', `/admin/retention/${scope}/${encodeURIComponent(id)}`),
  savedQueries: (projectId: string, signal?: AbortSignal) =>
    list<SavedQuery>(`/queries?project_id=${encodeURIComponent(projectId)}`, signal),
  createSavedQuery: (body: { project_id: string; name: string; query: SavedFilter }) =>
    request<SavedQuery>('POST', '/queries', body),
  deleteSavedQuery: (id: string) => request<void>('DELETE', `/queries/${encodeURIComponent(id)}`),
  channels: (workspaceId: string, signal?: AbortSignal) =>
    list<NotificationChannel>(`/notification-channels?workspace_id=${encodeURIComponent(workspaceId)}`, signal),
  createChannel: (body: { workspace_id: string; name: string; format: ChannelFormat; url: string }) =>
    request<NotificationChannel>('POST', '/notification-channels', body),
  updateChannel: (id: string, body: { name?: string; format?: ChannelFormat; url?: string; enabled?: boolean }) =>
    request<NotificationChannel>('PATCH', `/notification-channels/${encodeURIComponent(id)}`, body),
  deleteChannel: (id: string) => request<void>('DELETE', `/notification-channels/${encodeURIComponent(id)}`),
  testChannel: (id: string) =>
    request<{ ok: boolean; error?: string }>('POST', `/notification-channels/${encodeURIComponent(id)}/test`),
  rotateChannelSecret: (id: string) =>
    request<NotificationChannel>('POST', `/notification-channels/${encodeURIComponent(id)}/rotate-secret`),
  alertRules: (projectId: string, signal?: AbortSignal) =>
    list<AlertRule>(`/alerts?project_id=${encodeURIComponent(projectId)}`, signal),
  createAlertRule: (body: AlertRuleInput) => request<AlertRule>('POST', '/alerts', body),
  updateAlertRule: (id: string, body: AlertRuleInput) =>
    request<AlertRule>('PUT', `/alerts/${encodeURIComponent(id)}`, body),
  deleteAlertRule: (id: string) => request<void>('DELETE', `/alerts/${encodeURIComponent(id)}`),
  alertEvents: (id: string, signal?: AbortSignal) =>
    list<AlertEvent>(`/alerts/${encodeURIComponent(id)}/events?limit=50`, signal),
  /** Downloads the export as a Blob. Browsers do not expose HTTP trailers, so truncation is not reported here. */
  exportLogs: async (body: ExportRequest): Promise<Blob> => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (csrfToken) headers['X-CSRF-Token'] = csrfToken;
    const res = await fetch('/api/v1/logs/export', {
      method: 'POST',
      headers,
      credentials: 'same-origin',
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const data = (await res.json().catch(() => null)) as { detail?: string } | null;
      throw new ApiError(res.status, data?.detail ?? res.statusText, null);
    }
    return res.blob();
  },
};

/** URL of the live tail. EventSource sends the session cookie; GET needs no CSRF header. */
export const tailUrl = (params: Record<string, string | undefined>) => {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) q.set(k, v);
  return `/api/v1/logs/tail?${q.toString()}`;
};

/** Browser navigation, not fetch: the OAuth flow ends with a redirect back to the app. */
export const oauthStartUrl = (provider: string) => `/api/v1/auth/oauth/${encodeURIComponent(provider)}/start`;
