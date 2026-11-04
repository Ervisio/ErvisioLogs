import { request, type Session, type Workspace } from './api';
import type { UserSession } from '../../../packages/admin-client/src';

export type InvitationPreview = { workspace_name: string; email: string; role: string; expires_at: string; has_account: boolean };
export const account = {
  changePassword: (currentPassword: string, newPassword: string, signal?: AbortSignal) => request<void>('PUT', '/auth/password', { current_password: currentPassword, new_password: newPassword }, signal),
  sessions: async (signal?: AbortSignal) => (await request<{ items: UserSession[] | null }>('GET', '/auth/sessions', undefined, signal)).items ?? [],
  revokeSession: (id: string, signal?: AbortSignal) => request<void>('DELETE', `/auth/sessions/${encodeURIComponent(id)}`, undefined, signal),
  revokeOthers: (signal?: AbortSignal) => request<{ revoked: number }>('DELETE', '/auth/sessions', undefined, signal),
  resendVerification: (signal?: AbortSignal) => request<void>('POST', '/auth/email/verification', undefined, signal),
  forgotPassword: (email: string, signal?: AbortSignal) => request<void>('POST', '/auth/password/forgot', { email }, signal),
  resetPassword: (token: string, password: string, signal?: AbortSignal) => request<void>('POST', '/auth/password/reset', { token, password }, signal),
  verifyEmail: (token: string, signal?: AbortSignal) => request<void>('POST', '/auth/email/verify', { token }, signal),
  previewInvitation: (token: string, signal?: AbortSignal) => request<InvitationPreview>('POST', '/invitations/preview', { token }, signal),
  acceptInvitation: (token: string, signal?: AbortSignal) => request<Workspace>('POST', '/invitations/accept', { token }, signal),
  registerInvitation: (token: string, username: string, password: string, signal?: AbortSignal) => request<Session>('POST', '/invitations/register', { token, username, password }, signal),
};

type LinkKind = 'reset' | 'verification' | 'invitation';
const linkTokens: Partial<Record<LinkKind, string>> = {};

// Keep invitation continuity inside this SPA only; a refresh requires the email link again.
export function captureEmailToken(kind: LinkKind, hash: string): string {
  const token = new URLSearchParams(hash.replace(/^#/, '')).get('token');
  if (token) {
    linkTokens[kind] = token;
    window.history.replaceState(window.history.state, '', window.location.pathname + window.location.search);
  }
  return linkTokens[kind] ?? '';
}

export function clearEmailToken(kind: LinkKind): void { delete linkTokens[kind]; }
