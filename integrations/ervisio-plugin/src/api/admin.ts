import {
  createAdminClient,
  createFetchRequest,
  decodeResponse,
  type Request,
} from '../../../../packages/admin-client/src/index';
import { getSdk } from '../sdk';
import type { Mode } from './logs';

export type Connection = { kind: 'local' } | { kind: 'remote'; url: string; token: string };
export function adminClient(mode: Mode, connection: Connection) {
  if (connection.kind === 'remote') {
    if (!connection.token.startsWith('evl_adm_'))
      throw new Error('An instance administrator token (evl_adm_) is required');
    return createAdminClient(createFetchRequest({ baseUrl: connection.url, token: connection.token }));
  }
  const request: Request = async <T>(method: string, path: string, body?: unknown) => {
    const url = new URL(path, 'http://localhost');
    const response = await getSdk().api.http(mode === 'docker' ? 'logs-admin-docker' : 'logs-admin-standalone', {
      method,
      path: `/api/v1${url.pathname}`,
      query: Object.fromEntries(url.searchParams),
      headers: { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    return decodeResponse<T>(response.status, response.body);
  };
  return createAdminClient(request);
}
