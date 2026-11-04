import type { SettingEntry, SettingValue, SettingsScope } from './api';

export const SETTING_GROUPS = ['access', 'collection', 'quotas', 'email', 'archive', 'advanced'] as const;
export type SettingGroup = (typeof SETTING_GROUPS)[number];
export type SettingDraft = { value: string | boolean; inherit: boolean };
export type SettingIssue = { key: string; values?: Record<string, string | number> };

const groups: Record<string, SettingGroup> = {
  'auth.open_registration': 'access', 'auth.session_ttl': 'access',
  'ingest.auto_create_streams': 'collection', 'ingest.max_streams': 'collection',
  'ingest.rate_per_token': 'collection', 'ingest.rate_per_project': 'collection', 'ingest.rate_per_workspace': 'collection',
  'ingest.rate_burst_seconds': 'collection', 'ingest.max_future_skew': 'collection', 'ingest.max_past_age': 'collection',
  'ingest.project_quota_events': 'quotas', 'ingest.project_quota_bytes': 'quotas',
  'ingest.workspace_quota_events': 'quotas', 'ingest.workspace_quota_bytes': 'quotas',
  'email.smtp_host': 'email', 'email.smtp_port': 'email', 'email.smtp_security': 'email',
  'email.smtp_username': 'email', 'email.smtp_password_file': 'email', 'email.from': 'email',
  'archive.interval': 'archive', 'archive.grace': 'archive', 'archive.late_interval': 'archive',
  'alerts.interval': 'archive', 'alerts.history_retention': 'archive', 'alerts.webhook_allow_private': 'archive',
  'export.max_rows': 'advanced', 'server.log_level': 'advanced',
};

export const settingGroup = (entry: SettingEntry) => groups[entry.key] ?? 'advanced';
export const settingLabelKey = (entry: SettingEntry) => groups[entry.key] ? `settings.fields.${entry.key.replaceAll('.', '_')}` : 'settings.unknown';

export function settingDraft(entry: SettingEntry, scope: SettingsScope): SettingDraft {
  return { value: typeof entry.value === 'boolean' ? entry.value : String(entry.value), inherit: entry.source !== scope.type };
}

export function parseSetting(entry: SettingEntry, draft: SettingDraft): { value: SettingValue; issue?: never } | { issue: SettingIssue; value?: never } {
  if (entry.type === 'bool') return typeof draft.value === 'boolean' ? { value: draft.value } : { issue: { key: 'settings.invalidBoolean' } };
  if (entry.type === 'int' || entry.type === 'duration') {
    const value = Number(draft.value);
    if (draft.value === '' || !Number.isSafeInteger(value) || value < (entry.min ?? 0) || value > (entry.max ?? Number.MAX_SAFE_INTEGER)) {
      return { issue: { key: 'settings.invalidNumber', values: { min: entry.min ?? 0, max: entry.max ?? Number.MAX_SAFE_INTEGER } } };
    }
    return { value };
  }
  const value = String(draft.value);
  if (entry.type === 'choice' && !entry.options?.includes(value)) return { issue: { key: 'settings.invalidChoice' } };
  const hasControl = Array.from(value).some((char) => { const code = char.codePointAt(0)!; return code < 32 || (code >= 127 && code <= 159); });
  if (entry.type === 'string' && (new TextEncoder().encode(value).length > (entry.max ?? Infinity) || hasControl)) {
    return { issue: { key: 'settings.invalidText', values: { max: entry.max ?? 0 } } };
  }
  return { value: entry.type === 'string' ? value.trim() : value };
}

export function settingChanged(entry: SettingEntry, draft: SettingDraft | undefined, scope: SettingsScope): boolean {
  if (!draft || entry.locked) return false;
  if (draft.inherit) return entry.source === scope.type;
  if (entry.source !== scope.type) return true;
  const parsed = parseSetting(entry, draft);
  return parsed.issue !== undefined || parsed.value !== entry.value;
}
