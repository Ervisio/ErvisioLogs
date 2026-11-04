export type RequestTransport = <T>(method: string, path: string, body?: unknown, signal?: AbortSignal) => Promise<T>;
export type SettingsScope = { type: 'instance' } | { type: 'workspace' | 'project'; id: string };
export type SettingValue = string | number | boolean;
export type SettingEntry = { key: string; type: 'bool' | 'int' | 'duration' | 'choice' | 'string'; value: SettingValue; source: 'env' | 'instance' | 'workspace' | 'project' | 'default'; inherited: SettingValue; locked: boolean; env?: string; min?: number; max?: number; options?: string[] };
export type User = { id: string; email: string; username: string; email_verified: boolean; is_instance_admin: boolean; status: string; created_at: string };
export type UserInput = { email: string; username: string; password?: string };
export type UserPatch = { status?: 'active' | 'disabled'; is_instance_admin?: boolean };
export type UserSession = { id: string; created_at: string; expires_at: string; last_seen_at: string | null; ip: string; user_agent: string; current: boolean };
export type Page<T> = { items: T[] | null; next_cursor: string | null };
export type Workspace = { id: string; name: string; slug: string; role: string; created_at: string };
export type Project = { id: string; workspace_id: string; name: string; slug: string; created_at: string };
export type Member = { user_id: string; username: string; email: string; role: string; joined_at: string };
export type MetadataPatch = { display_name?: string; description?: string; color?: string };
export type Environment = MetadataPatch & { id: string; project_id: string; name: string; created_at: string };
export type Stream = Environment & { type: string };
export type ApiToken = { id: string; project_id: string; name: string; prefix: string; scopes: string[]; created_at: string; expires_at: string | null; last_used_at: string | null; revoked_at: string | null };
export type ProviderType = 's3' | 'sftp' | 'local';
export type ProviderInput = { name: string; type?: ProviderType; config: Record<string, unknown>; secret_file: string | null };
export type StorageProvider = ProviderInput & { id: string; type: ProviderType; active: boolean; status: 'unknown' | 'ok' | 'error'; last_error: string | null; last_checked_at: string | null; created_at: string; updated_at: string };
export type StorageOverview = { providers: StorageProvider[] | null; segments: Record<string, { count: number; bytes: number }>; archiver_enabled: boolean; usage?: { hot_bytes: number; cache_bytes: number; max_bytes: number; high_pct: number; low_pct: number; over_capacity: boolean } };
export type RetentionScope = 'instance' | 'workspace' | 'project' | 'stream';
export type RetentionInput = { local_age_hours?: number | null; global_age_hours?: number | null; cache_max_bytes?: number; high_pct?: number; low_pct?: number };
export type RetentionPolicy = RetentionInput & { scope_type: RetentionScope; scope_id: string; updated_by: string | null; updated_at: string };
export type AuditEvent = { id: string; workspace_id: string | null; actor_user_id: string | null; actor_token_id: string | null; actor_name: string; action: string; target_type: string; target_id: string; request_id: string; ip: string; detail: Record<string, unknown>; created_at: string };
export type Health = { status: string; wal: { bytes: number; max_bytes: number; segments: number; pending_records: number; lag_seconds: number } | null; upload: { waiting: { count: number; bytes: number }; failed: { count: number; bytes: number }; last_verified_at: string | null; oldest_unarchived_event_at: string | null }; archive: { enabled: boolean; provider: Pick<StorageProvider, 'name' | 'type' | 'status' | 'last_error' | 'last_checked_at'> | null }; postgres: { ok: boolean; error: string | null }; clickhouse: { ok: boolean; error: string | null } };
const encoded = encodeURIComponent;
const settingsPath = (scope: SettingsScope) => scope.type === 'instance' ? '/admin/settings' : `/${scope.type === 'workspace' ? 'workspaces' : 'projects'}/${encoded(scope.id)}/settings`;
export function createAdminClient(request: RequestTransport) {
  const list = async <T>(path: string, signal?: AbortSignal) => (await request<{ items: T[] | null }>('GET', path, undefined, signal)).items ?? [];
  const userPath = (id: string) => `/admin/users/${encoded(id)}`;
  const metadataPath = (project: string, kind: 'streams' | 'environments', id?: string) => `/projects/${encoded(project)}/${kind}${id ? `/${encoded(id)}` : ''}`;
  return {
    settings: (scope: SettingsScope, signal?: AbortSignal) => list<SettingEntry>(settingsPath(scope), signal),
    putSetting: (scope: SettingsScope, key: string, value: SettingValue, signal?: AbortSignal) => request<SettingEntry>('PUT', `${settingsPath(scope)}/${encoded(key)}`, { value }, signal),
    resetSetting: (scope: SettingsScope, key: string, signal?: AbortSignal) => request<SettingEntry>('DELETE', `${settingsPath(scope)}/${encoded(key)}`, undefined, signal),
    adminUsers: {
      list: (q = '', cursor?: string, signal?: AbortSignal) => { const query = new URLSearchParams({ q }); if (cursor) query.set('cursor', cursor); return request<Page<User>>('GET', `/admin/users?${query}`, undefined, signal); },
      get: (id: string, signal?: AbortSignal) => request<User>('GET', userPath(id), undefined, signal),
      create: (body: UserInput, signal?: AbortSignal) => request<User>('POST', '/admin/users', body, signal),
      update: (id: string, body: UserPatch, signal?: AbortSignal) => request<void>('PATCH', userPath(id), body, signal),
      remove: (id: string, signal?: AbortSignal) => request<void>('DELETE', userPath(id), undefined, signal),
      resetPassword: (id: string, password: string, signal?: AbortSignal) => request<void>('PUT', `${userPath(id)}/password`, { password }, signal),
      sessions: (id: string, signal?: AbortSignal) => list<UserSession>(`${userPath(id)}/sessions`, signal),
      revokeSession: (id: string, sessionId: string, signal?: AbortSignal) => request<void>('DELETE', `${userPath(id)}/sessions/${encoded(sessionId)}`, undefined, signal),
      revokeSessions: (id: string, signal?: AbortSignal) => request<{ revoked: number }>('DELETE', `${userPath(id)}/sessions`, undefined, signal),
    },
    health: (signal?: AbortSignal) => request<Health>('GET', '/admin/health', undefined, signal),
    audit: (workspace?: string, cursor?: string, signal?: AbortSignal) => request<Page<AuditEvent>>('GET', `${workspace ? `/workspaces/${encoded(workspace)}/audit` : '/admin/audit'}${cursor ? `?cursor=${encoded(cursor)}` : ''}`, undefined, signal),
    workspaces: (signal?: AbortSignal) => list<Workspace>('/workspaces', signal),
    createWorkspace: (body: { name: string; slug: string; owner_id?: string }) => request<Workspace>('POST', '/workspaces', body),
    updateWorkspace: (id: string, body: { name: string }) => request<Workspace>('PATCH', `/workspaces/${encoded(id)}`, body),
    deleteWorkspace: (id: string, confirm: string) => request<void>('DELETE', `/workspaces/${encoded(id)}`, { confirm }),
    projects: (workspace: string, signal?: AbortSignal) => list<Project>(`/projects?workspace_id=${encoded(workspace)}`, signal),
    createProject: (body: { workspace_id: string; name: string; slug: string }) => request<Project>('POST', '/projects', body),
    updateProject: (id: string, body: { name: string }) => request<Project>('PATCH', `/projects/${encoded(id)}`, body),
    deleteProject: (id: string, confirm: string) => request<void>('DELETE', `/projects/${encoded(id)}`, { confirm }),
    members: (workspace: string, signal?: AbortSignal) => list<Member>(`/workspaces/${encoded(workspace)}/members`, signal),
    addMember: (workspace: string, body: { email: string; role: string }) => request<Member>('POST', `/workspaces/${encoded(workspace)}/members`, body),
    setMemberRole: (workspace: string, id: string, role: string) => request<void>('PATCH', `/workspaces/${encoded(workspace)}/members/${encoded(id)}`, { role }),
    removeMember: (workspace: string, id: string) => request<void>('DELETE', `/workspaces/${encoded(workspace)}/members/${encoded(id)}`),
    streams: (project: string, signal?: AbortSignal) => list<Stream>(metadataPath(project, 'streams'), signal),
    environments: (project: string, signal?: AbortSignal) => list<Environment>(metadataPath(project, 'environments'), signal),
    createStream: (project: string, body: { name: string } & MetadataPatch) => request<Stream>('POST', metadataPath(project, 'streams'), body),
    createEnvironment: (project: string, body: { name: string } & MetadataPatch) => request<Environment>('POST', metadataPath(project, 'environments'), body),
    updateStream: (project: string, id: string, body: MetadataPatch, signal?: AbortSignal) => request<Stream>('PATCH', metadataPath(project, 'streams', id), body, signal),
    updateEnvironment: (project: string, id: string, body: MetadataPatch, signal?: AbortSignal) => request<Environment>('PATCH', metadataPath(project, 'environments', id), body, signal),
    deleteStream: (project: string, id: string) => request<void>('DELETE', metadataPath(project, 'streams', id)),
    deleteEnvironment: (project: string, id: string) => request<void>('DELETE', metadataPath(project, 'environments', id)),
    tokens: (project: string, signal?: AbortSignal) => list<ApiToken>(`/tokens?project_id=${encoded(project)}`, signal),
    createToken: (body: { project_id: string; name: string; scopes: string[]; expires_at?: string }) => request<{ token: string; api_token: ApiToken }>('POST', '/tokens', body),
    revokeToken: (id: string) => request<void>('DELETE', `/tokens/${encoded(id)}`),
    storage: (signal?: AbortSignal) => request<StorageOverview>('GET', '/admin/storage', undefined, signal),
    createProvider: (body: ProviderInput) => request<StorageProvider>('POST', '/admin/storage/providers', body),
    updateProvider: (id: string, body: Omit<ProviderInput, 'type'>) => request<StorageProvider>('PUT', `/admin/storage/providers/${encoded(id)}`, body),
    deleteProvider: (id: string) => request<void>('DELETE', `/admin/storage/providers/${encoded(id)}`),
    testProvider: (id: string) => request<StorageProvider>('POST', `/admin/storage/providers/${encoded(id)}/test`),
    setActiveProvider: (id: string | null) => request<void>('PUT', '/admin/storage/active', { provider_id: id }),
    secrets: (signal?: AbortSignal) => list<string>('/admin/storage/secrets', signal),
    putSecret: (name: string, value: string) => request<void>('PUT', `/admin/storage/secrets/${encoded(name)}`, { value }),
    retention: (signal?: AbortSignal) => list<RetentionPolicy>('/admin/retention', signal),
    putRetention: (type: RetentionScope, id: string, body: RetentionInput) => request<RetentionPolicy>('PUT', `/admin/retention/${type}/${encoded(id)}`, body),
    deleteRetention: (type: RetentionScope, id: string) => request<void>('DELETE', `/admin/retention/${type}/${encoded(id)}`),
  };
}
export type AdminClient = ReturnType<typeof createAdminClient>;
