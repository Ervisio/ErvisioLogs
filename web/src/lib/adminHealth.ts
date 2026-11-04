import { createAdminClient } from '../../../packages/admin-client/src';
import { request } from './api';

export type { Health as AdminHealth } from '../../../packages/admin-client/src';
export const adminHealth = { get: createAdminClient(request).health };
