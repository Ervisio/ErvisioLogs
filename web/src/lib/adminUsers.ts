import { request } from './api';

export type { UserSession as AdminUserSession, UserInput as NewAdminUser } from '../../../packages/admin-client/src';
import { createAdminClient } from '../../../packages/admin-client/src';

const client = createAdminClient(request).adminUsers;
export const adminUsers = {
  ...client,
  list: async (query: string, cursor?: string, signal?: AbortSignal) => {
    const page = await client.list(query, cursor, signal);
    return { ...page, items: page.items ?? [] };
  },
};

export { validPassword as validAdminPassword } from './password';
