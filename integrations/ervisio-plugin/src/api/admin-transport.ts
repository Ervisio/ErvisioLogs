import type { HttpResponse } from '../sdk';
import type { RequestTransport } from '../../../../packages/admin-client/src/index';
export type ConnectionTarget = { type: 'docker' | 'standalone' } | { type: 'https'; origin: string; token: string };
export class AdminError extends Error {
  status: number;
  constructor(status: number, message: string) { super(message); this.name = 'AdminError'; this.status = status; }
}
export function httpsOrigin(value: string): string {
  const url = new URL(value);
  if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || url.pathname !== '/') throw new Error('Inserisci solo l’origine HTTPS, senza credenziali, percorsi o parametri.');
  return url.origin;
}
export function createAdminConnection(http: (name: string, request: { method: string; path: string; query?: string; headers?: Record<string, string>; body?: object | string }) => Promise<HttpResponse>, fetcher: typeof fetch = fetch) {
  let target: ConnectionTarget | null = null;
  let generation = 0;
  const pending = new Set<AbortController>();
  const disconnect = () => { target = null; generation++; for (const controller of pending) controller.abort(); pending.clear(); };
  const connect = (next: ConnectionTarget) => { disconnect(); target = next.type === 'https' ? { ...next, origin: httpsOrigin(next.origin) } : next; };
  const request: RequestTransport = async <T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> => {
    const active = target, version = generation;
    if (!active) throw new Error('Collega un’istanza per continuare.');
    signal?.throwIfAborted();
    const token = active.type === 'https' ? active.token : '';
    const redact = (text: string) => token ? text.replaceAll(token, '[REDACTED]') : text;
    const controller = new AbortController();
    pending.add(controller);
    const abort = () => controller.abort();
    signal?.addEventListener('abort', abort, { once: true });
    try {
      let status: number, data: unknown;
      if (active.type === 'https') {
        const response = await fetcher(`${active.origin}/api/v1${path}`, { method, signal: controller.signal, credentials: 'omit', redirect: 'error', headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) }, body: body === undefined ? undefined : JSON.stringify(body) });
        status = response.status; data = status === 204 ? undefined : await response.json().catch(() => null);
      } else {
        const url = new URL(`/api/v1${path}`, 'http://socket');
        const response = await http(active.type === 'docker' ? 'logs-admin-docker' : 'logs-admin-system', { method, path: url.pathname, query: url.search.slice(1), ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: body as object }) });
        status = response.status; data = status === 204 || !response.body ? undefined : response.json();
      }
      signal?.throwIfAborted();
      if (version !== generation) throw new Error('Il collegamento è cambiato.');
      if (status >= 400) throw new AdminError(status, redact((data as { detail?: string } | null)?.detail || `API HTTP ${status}`));
      return data as T;
    } catch (error) {
      if (error instanceof AdminError) throw error;
      if (active.type !== 'https') throw error;
      throw new Error(redact(error instanceof Error ? error.message : 'Richiesta HTTPS non riuscita.'));
    } finally {
      pending.delete(controller);
      signal?.removeEventListener('abort', abort);
    }
  };
  return { connect, disconnect, request };
}
