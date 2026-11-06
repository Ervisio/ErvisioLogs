import { request, type ApiToken, type ChannelFormat, type NotificationChannel, type SavedFilter, type SavedQuery } from './api';
import { items } from './manage-api';

export type ChannelType = 'webhook' | 'email' | 'telegram';
export type Channel = Omit<NotificationChannel, 'type'> & { type: ChannelType };
export type ChannelInput = { workspace_id: string; name: string; type: ChannelType; format?: ChannelFormat; url?: string; recipient?: string; bot_token?: string; chat_id?: string };
export type ChannelDelivery = { id: string; status: 'ok' | 'error'; error: string | null; created_at: string };
const enc = encodeURIComponent;
export const notifications = {
  channels: (workspace: string, signal?: AbortSignal) => items<Channel>(`/notification-channels?workspace_id=${enc(workspace)}`, signal),
  createChannel: (body: ChannelInput) => request<Channel>('POST', '/notification-channels', body),
  deliveries: (id: string, signal?: AbortSignal) => items<ChannelDelivery>(`/notification-channels/${enc(id)}/deliveries?limit=50`, signal),
  rotateToken: (id: string) => request<{ token: string; api_token: ApiToken }>('POST', `/tokens/${enc(id)}/rotate`),
  updateSavedQuery: (view: SavedQuery, query: SavedFilter) => request<SavedQuery>('PUT', `/queries/${enc(view.id)}`, { project_id: view.project_id, name: view.name, query }),
};

export function futureExpiry(value: string, now = Date.now()): string | null {
  if (!value) return null;
  const expires = new Date(value);
  return Number.isFinite(expires.getTime()) && expires.getTime() > now ? expires.toISOString() : null;
}
