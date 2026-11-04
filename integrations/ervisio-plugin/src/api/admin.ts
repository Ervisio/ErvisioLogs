import { createAdminClient } from '../../../../packages/admin-client/src/index';
import { getSdk } from '../sdk';
import { createAdminConnection } from './admin-transport';
export function adminConnection() {
  const connection = createAdminConnection((name, request) => getSdk().api.http(name, request));
  return { ...connection, client: createAdminClient(connection.request) };
}
export type { AdminClient, SettingEntry, SettingsScope, SettingValue, User, UserSession, Workspace, Project, Stream, Environment, ApiToken, StorageProvider, ProviderInput, RetentionScope } from '../../../../packages/admin-client/src/index';
