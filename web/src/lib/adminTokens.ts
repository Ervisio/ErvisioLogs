import { request } from './api';

export const ADMIN_SCOPES = ['admin:read', 'admin:users', 'admin:settings', 'admin:tenancy', 'admin:storage'] as const;
export type AdminScope = (typeof ADMIN_SCOPES)[number];
export type AdminToken = {
  id: string;
  name: string;
  prefix: string;
  scopes: AdminScope[];
  created_by: string | null;
  created_at: string;
  expires_at: string;
  last_used_at: string | null;
  revoked_at: string | null;
};
export type NewAdminToken = { name: string; scopes: AdminScope[]; expires_at: string };

export const adminTokens = {
  list: async (signal?: AbortSignal) => (await request<{ items: AdminToken[] | null }>('GET', '/admin/tokens', undefined, signal)).items ?? [],
  create: (body: NewAdminToken, signal?: AbortSignal) => request<{ token: string; admin_token: AdminToken }>('POST', '/admin/tokens', body, signal),
  revoke: (id: string, signal?: AbortSignal) => request<void>('DELETE', `/admin/tokens/${encodeURIComponent(id)}`, undefined, signal),
};

export function adminTokenIssue(name: string, scopes: AdminScope[], expiresAt: string, now = Date.now()): string | null {
  const trimmed = name.trim();
  if (!trimmed || Array.from(trimmed).length > 100 || trimmed.includes('\0')) return 'instanceAdmin.tokens.invalidName';
  if (scopes.length === 0 || scopes.some((scope) => !ADMIN_SCOPES.includes(scope)) || new Set(scopes).size !== scopes.length) return 'instanceAdmin.tokens.invalidScopes';
  const expires = new Date(expiresAt).getTime();
  if (!Number.isFinite(expires) || expires <= now || expires > now + 365 * 24 * 60 * 60 * 1000) return 'instanceAdmin.tokens.invalidExpiry';
  return null;
}
