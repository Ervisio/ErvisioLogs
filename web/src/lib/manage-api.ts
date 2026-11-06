import {
  request,
  type Environment,
  type Metadata,
  type Project,
  type RetentionInput,
  type RetentionPolicy,
  type Stream,
  type User,
  type Workspace,
} from './api';
import { createAdminClient } from '../../../packages/admin-client/src/index';
export const adminClient = createAdminClient(request);

export type Setting = {
  key: string;
  type: 'bool' | 'int' | 'duration' | 'choice' | 'string';
  value: boolean | number | string;
  source: string;
  inherited: boolean | number | string;
  locked: boolean;
  env?: string;
  min?: number;
  max?: number;
  options?: string[];
};
export type Page<T> = { items: T[] | null; next_cursor?: string | null };
export type SessionInfo = {
  id: string;
  ip: string;
  user_agent: string;
  created_at: string;
  expires_at: string;
  last_seen_at: string | null;
  current: boolean;
};
export type Invitation = { id: string; email: string; role: string; expires_at: string; emailed: boolean };
export type Audit = {
  id: string;
  action: string;
  actor_name: string;
  actor_user_id: string | null;
  target_type: string;
  target_id: string;
  created_at: string;
  detail: unknown;
};
export type EventType = {
  name: string;
  title: string;
  template: string;
  icon: string;
  category: string;
  color: string;
  featured_fields: string[];
  discovered: boolean;
};
export type FieldRule = {
  field: string;
  label: string;
  hidden: boolean;
  format: 'text' | 'number' | 'currency' | 'datetime' | 'json';
  redact: boolean;
};
export type AdminToken = {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  expires_at: string;
  revoked_at: string | null;
};
export type Health = {
  status: string;
  wal: { bytes: number; max_bytes: number; pending_records: number; lag_seconds: number } | null;
  upload: {
    waiting: { count: number; bytes: number };
    failed: { count: number };
    last_verified_at: string | null;
    oldest_unarchived_event_at: string | null;
  };
  postgres: { ok: boolean; error: string | null };
  clickhouse: { ok: boolean; error: string | null };
  archive: { enabled: boolean; provider: { name: string; status: string; last_error: string | null } | null };
};
export type PurgeJob = { id: string; status: string; from: string; to: string; error?: string };
const enc = encodeURIComponent;
export const items = async <T>(path: string, signal?: AbortSignal): Promise<T[]> =>
  (await request<Page<T>>('GET', path, undefined, signal)).items ?? [];
export const management = {
  settings: (path: string, signal?: AbortSignal) => items<Setting>(`${path}/settings`, signal),
  putSetting: (path: string, key: string, value: Setting['value']) =>
    request<Setting>('PUT', `${path}/settings/${enc(key)}`, { value }),
  resetSetting: (path: string, key: string) => request<Setting>('DELETE', `${path}/settings/${enc(key)}`),
  putRetention: (scope: 'instance' | 'workspace' | 'project', id: string, body: RetentionInput) =>
    request<RetentionPolicy>(
      'PUT',
      scope === 'instance'
        ? `/admin/retention/instance/${enc(id)}`
        : `/${scope === 'workspace' ? 'workspaces' : 'projects'}/${enc(id)}/retention`,
      body,
    ),
  deleteRetention: (scope: 'instance' | 'workspace' | 'project', id: string) =>
    request<void>(
      'DELETE',
      scope === 'instance'
        ? `/admin/retention/instance/${enc(id)}`
        : `/${scope === 'workspace' ? 'workspaces' : 'projects'}/${enc(id)}/retention`,
    ),
  renameWorkspace: (id: string, name: string) => request<Workspace>('PATCH', `/workspaces/${enc(id)}`, { name }),
  deleteWorkspace: (id: string, confirm: string) => request<void>('DELETE', `/workspaces/${enc(id)}`, { confirm }),
  renameProject: (id: string, name: string) => request<Project>('PATCH', `/projects/${enc(id)}`, { name }),
  deleteProject: (id: string, confirm: string) => request<void>('DELETE', `/projects/${enc(id)}`, { confirm }),
  createMetadata: (id: string, kind: 'streams' | 'environments', name: string) =>
    request<Stream | Environment>('POST', `/projects/${enc(id)}/${kind}`, { name }),
  updateMetadata: (id: string, kind: 'streams' | 'environments', item: string, body: Metadata) =>
    request<Stream | Environment>('PATCH', `/projects/${enc(id)}/${kind}/${enc(item)}`, body),
  deleteMetadata: (id: string, kind: 'streams' | 'environments', item: string) =>
    request<void>('DELETE', `/projects/${enc(id)}/${kind}/${enc(item)}`),
  invitations: (id: string, signal?: AbortSignal) => items<Invitation>(`/workspaces/${enc(id)}/invitations`, signal),
  invite: (id: string, email: string, role: string) =>
    request<Invitation & { url?: string; mail_error?: string }>('POST', `/workspaces/${enc(id)}/invitations`, {
      email,
      role,
    }),
  cancelInvitation: (id: string, invitation: string) =>
    request<void>('DELETE', `/workspaces/${enc(id)}/invitations/${enc(invitation)}`),
  users: (q: string, cursor = '', signal?: AbortSignal) =>
    request<Page<User>>('GET', `/admin/users?q=${enc(q)}&cursor=${enc(cursor)}&limit=50`, undefined, signal),
  createUser: adminClient.createUser,
  updateUser: adminClient.updateUser,
  deleteUser: adminClient.deleteUser,
  resetPassword: adminClient.resetPassword,
  sessions: (path = '/auth', signal?: AbortSignal) => items<SessionInfo>(`${path}/sessions`, signal),
  revokeSession: (path: string, id?: string) => request<void>('DELETE', `${path}/sessions${id ? `/${enc(id)}` : ''}`),
  changePassword: (current_password: string, new_password: string) =>
    request<void>('PUT', '/auth/password', { current_password, new_password }),
  verification: () => request<void>('POST', '/auth/email/verification'),
  health: (signal?: AbortSignal) => request<Health>('GET', '/admin/health', undefined, signal),
  audit: (path: string, cursor = '', signal?: AbortSignal) =>
    request<Page<Audit>>('GET', `${path}/audit?limit=50&cursor=${enc(cursor)}`, undefined, signal),
  eventTypes: (id: string, signal?: AbortSignal) => items<EventType>(`/projects/${enc(id)}/event-types`, signal),
  putEventType: (id: string, body: EventType, create = false) =>
    request<EventType>(
      create ? 'POST' : 'PATCH',
      `/projects/${enc(id)}/event-types${create ? '' : `/${enc(body.name)}`}`,
      {
        ...(create ? { name: body.name } : {}),
        title: body.title,
        template: body.template,
        icon: body.icon,
        category: body.category,
        color: body.color,
        featured_fields: body.featured_fields,
      },
    ),
  deleteEventType: (id: string, name: string) =>
    request<void>('DELETE', `/projects/${enc(id)}/event-types/${enc(name)}`),
  preset: (id: string) => request<void>('POST', `/projects/${enc(id)}/event-types/presets/fivem`),
  fieldRules: (id: string, signal?: AbortSignal) => items<FieldRule>(`/projects/${enc(id)}/field-rules`, signal),
  putFieldRules: (id: string, body: { items: FieldRule[] }) =>
    request<void>('PUT', `/projects/${enc(id)}/field-rules`, body),
  adminTokens: (signal?: AbortSignal) => items<AdminToken>('/admin/tokens', signal),
  createAdminToken: (body: { name: string; scopes: string[]; expires_at: string }) =>
    request<{ token: string; admin_token: AdminToken }>('POST', '/admin/tokens', body),
  revokeAdminToken: (id: string) => request<void>('DELETE', `/admin/tokens/${enc(id)}`),
  purges: (id: string, signal?: AbortSignal) => items<PurgeJob>(`/projects/${enc(id)}/purges`, signal),
  purge: (id: string, body: { confirm: string; from?: string; to?: string; stream?: string }) =>
    request<PurgeJob>('POST', `/projects/${enc(id)}/purges`, body),
};
