import type { LogItem } from './api';

export const LOG_LEVELS = ['FATAL', 'ERROR', 'WARN', 'NOTICE', 'INFO', 'DEBUG', 'TRACE'] as const;

export const levelColor = (level: string) => ({
  FATAL: 'text-err', ERROR: 'text-err', WARN: 'text-warn', NOTICE: 'text-hue',
  INFO: 'text-ok', DEBUG: 'text-ink2', TRACE: 'text-ink3',
})[level] ?? 'text-ink2';

export function fullTimestamp(value: string, language: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'long' }).format(date);
}

export function relativeTimestamp(value: string, language: string, now: number): string {
  const seconds = (new Date(value).getTime() - now) / 1000;
  if (!Number.isFinite(seconds)) return value;
  const [size, unit]: [number, Intl.RelativeTimeFormatUnit] = Math.abs(seconds) < 60
    ? [1, 'second'] : Math.abs(seconds) < 3600 ? [60, 'minute'] : Math.abs(seconds) < 86400 ? [3600, 'hour'] : [86400, 'day'];
  return new Intl.RelativeTimeFormat(language, { numeric: 'auto' }).format(Math.round(seconds / size), unit);
}

export function logDay(value: string, language: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(language, { dateStyle: 'long' }).format(date);
}

export type LogEntity = { type?: string; id?: string | number; name?: string };

export function logEntity(value: unknown): LogEntity | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const entity = value as Record<string, unknown>;
  const name = typeof entity.name === 'string' ? entity.name : undefined;
  const id = typeof entity.id === 'string' || typeof entity.id === 'number' ? entity.id : undefined;
  const type = typeof entity.type === 'string' ? entity.type : undefined;
  return name || id !== undefined ? { name, id, type } : null;
}

export function entityLabel(entity: LogEntity): string {
  return entity.name || String(entity.id);
}

export function attributeText(value: unknown): string {
  return typeof value === 'string' ? value : JSON.stringify(value) ?? String(value);
}

export function filterableValue(value: unknown): string | null {
  if (!['string', 'number', 'boolean'].includes(typeof value)) return null;
  const text = String(value);
  return text || null;
}

export function logEntities(item: LogItem) {
  return (['actor', 'target'] as const).flatMap(key => {
    const entity = logEntity(item.attributes?.[key]);
    return entity ? [{ key, entity }] : [];
  });
}
