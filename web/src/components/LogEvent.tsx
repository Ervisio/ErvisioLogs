import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Activity,
  ArrowRightLeft,
  Briefcase,
  Copy,
  Package,
  PackagePlus,
  ScrollText,
  Shield,
  ShoppingCart,
  UserMinus,
  UserPlus,
  Wallet,
  X,
} from 'lucide-react';
import type { LogItem } from '../lib/api';
import type { EventType, FieldRule } from '../lib/manage-api';

export function CatalogIcon({ name, color }: { name?: string; color?: string }) {
  const icons = {
    activity: Activity,
    'arrow-right-left': ArrowRightLeft,
    briefcase: Briefcase,
    package: Package,
    'package-plus': PackagePlus,
    shield: Shield,
    'shopping-cart': ShoppingCart,
    'user-minus': UserMinus,
    'user-plus': UserPlus,
    wallet: Wallet,
  };
  const Icon = icons[name as keyof typeof icons] ?? ScrollText;
  return <Icon className="text-ink3 size-3.5 flex-none" style={color ? { color } : undefined} aria-hidden />;
}

export function attributeValue(attrs: Record<string, unknown> | null, path: string): unknown {
  return path
    .replace(/^attributes\./, '')
    .split('.')
    .reduce<unknown>(
      (value, key) =>
        typeof value === 'object' && value !== null && !Array.isArray(value)
          ? (value as Record<string, unknown>)[key]
          : undefined,
      attrs,
    );
}
export function hiddenField(path: string, rules: FieldRule[]) {
  const normalized = path.replace(/^attributes\./, '');
  return rules.some((rule) => {
    const field = rule.field.replace(/^attributes\./, '');
    return rule.hidden && (normalized === field || normalized.startsWith(`${field}.`));
  });
}
function attributeLeaves(value: Record<string, unknown>, prefix = ''): [string, unknown][] {
  return Object.entries(value).flatMap(([key, item]): [string, unknown][] => {
    const path = prefix ? `${prefix}.${key}` : key;
    return item !== null && typeof item === 'object' && !Array.isArray(item)
      ? attributeLeaves(item as Record<string, unknown>, path)
      : [[path, item]];
  });
}
function visibleAttribute(value: unknown, path: string, rules: FieldRule[]): unknown {
  if (hiddenField(path, rules)) return undefined;
  if (Array.isArray(value)) return value.map((item, index) => visibleAttribute(item, `${path}.${index}`, rules));
  if (value !== null && typeof value === 'object')
    return Object.fromEntries(
      Object.entries(value)
        .map(([key, item]) => [key, visibleAttribute(item, `${path}.${key}`, rules)])
        .filter(([, item]) => item !== undefined),
    );
  return value;
}
const stringValue = (value: unknown) =>
  value == null ? '' : typeof value === 'object' ? JSON.stringify(value) : String(value);
export function eventMessage(item: LogItem, type?: EventType, rules: FieldRule[] = []) {
  return type?.template
    ? type.template.replace(/\{([a-zA-Z0-9_.-]+)\}/g, (_, path: string) =>
        stringValue(visibleAttribute(attributeValue(item.attributes, path), path, rules)),
      )
    : item.message;
}
export function relativeTime(iso: string, locale: string, now: number) {
  const seconds = (new Date(iso).getTime() - now) / 1000;
  const unit: Intl.RelativeTimeFormatUnit =
    Math.abs(seconds) < 60
      ? 'second'
      : Math.abs(seconds) < 3600
        ? 'minute'
        : Math.abs(seconds) < 86400
          ? 'hour'
          : 'day';
  const divisor = unit === 'second' ? 1 : unit === 'minute' ? 60 : unit === 'hour' ? 3600 : 86400;
  return new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(Math.round(seconds / divisor), unit);
}
export function EventDetail({
  item,
  type,
  rules,
  onClose,
  onFilter,
}: {
  item: LogItem;
  type?: EventType;
  rules: FieldRule[];
  onClose: () => void;
  onFilter: (field: string, value: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const panel = useRef<HTMLElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    const opener = document.activeElement;
    close.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
      if (e.key !== 'Tab' || !panel.current) return;
      const stops = Array.from(panel.current.querySelectorAll<HTMLElement>('button:not(:disabled), summary, a[href]'));
      const first = stops[0];
      const last = stops[stops.length - 1];
      if (!panel.current.contains(document.activeElement) || (e.shiftKey && document.activeElement === first)) {
        e.preventDefault();
        (e.shiftKey ? last : first)?.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first?.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      if (opener instanceof HTMLElement) opener.focus();
    };
  }, [onClose]);
  const field = (path: string, value: unknown, attribute: boolean) => {
    const rule = attribute
      ? rules.find((r) => r.field.replace(/^attributes\./, '') === path.replace(/^attributes\./, ''))
      : undefined;
    if ((attribute && hiddenField(path, rules)) || value == null || value === '') return null;
    let display = stringValue(attribute ? visibleAttribute(value, path, rules) : value);
    if (rule?.format === 'number' && Number.isFinite(Number(value)))
      display = Number(value).toLocaleString(i18n.language);
    if (rule?.format === 'currency' && Number.isFinite(Number(value)))
      display = new Intl.NumberFormat(i18n.language, { style: 'currency', currency: 'EUR' }).format(Number(value));
    if (rule?.format === 'datetime' && Number.isFinite(new Date(String(value)).getTime()))
      display = new Date(String(value)).toLocaleString(i18n.language);
    const filterable = attribute || !['timestamp', 'ingested_at', 'id', 'host', 'resource', 'span_id'].includes(path);
    return (
      <div key={path} className="grid grid-cols-[100px_minmax(0,1fr)] items-start gap-3">
        <dt className="text-ink3 break-words text-xs">{rule?.label || path}</dt>
        <dd>
          {filterable ? (
            <button
              type="button"
              className="hover:text-hue min-h-11 w-full text-left break-words"
              aria-label={t('manage.filterBy', { field: path, value: display })}
              onClick={() => onFilter(attribute ? `attributes.${path}` : path, stringValue(value))}
            >
              {display}
            </button>
          ) : (
            <span className="block min-h-11 content-center break-words">{display}</span>
          )}
        </dd>
      </div>
    );
  };
  const attrs = item.attributes ?? {};
  const groups: { key: string; entries: [string, unknown][] }[] = [
    {
      key: 'who',
      entries: ['actor', 'target'].flatMap((key) => {
        const entity = attributeValue(attrs, key);
        return entity && typeof entity === 'object'
          ? Object.entries(entity).map(([field, value]): [string, unknown] => [`${key}.${field}`, value])
          : entity
            ? [[key, entity]]
            : [];
      }),
    },
    {
      key: 'what',
      entries: attributeLeaves(
        Object.fromEntries(Object.entries(attrs).filter(([key]) => key !== 'actor' && key !== 'target')),
      ),
    },
    {
      key: 'where',
      entries: [
        ['stream', item.stream],
        ['environment', item.environment],
        ['resource', item.resource],
        ['service', item.service],
        ['host', item.host],
      ],
    },
    {
      key: 'when',
      entries: [
        ['timestamp', new Date(item.timestamp).toLocaleString(i18n.language)],
        ['ingested_at', new Date(item.ingested_at).toLocaleString(i18n.language)],
      ],
    },
    {
      key: 'technical',
      entries: [
        ['event', item.event],
        ['level', item.level],
        ['trace_id', item.trace_id],
        ['span_id', item.span_id],
        ['id', item.id],
      ],
    },
  ];
  return (
    <>
      <div className="fixed inset-0 z-40 bg-black/40" onClick={onClose} aria-hidden />
      <aside
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby="event-title"
        className="bg-surface border-line fixed inset-y-2 right-2 z-50 flex w-[min(480px,calc(100vw-16px))] flex-col overflow-hidden rounded-card border shadow-xl"
      >
        <header className="border-line flex min-h-14 items-center gap-2 border-b px-4">
          <h2 id="event-title" className="min-w-0 flex-1 truncate font-bold">
            {type?.title || item.event || t('detail.title')}
          </h2>
          <button
            type="button"
            aria-label={t('detail.copy')}
            className="manage-button"
            onClick={() => {
              (navigator.clipboard
                ? navigator.clipboard.writeText(JSON.stringify(item, null, 2))
                : Promise.reject(new Error('Clipboard unavailable'))
              )
                .then(() => {
                  setCopied(true);
                  setError('');
                })
                .catch(() => setError(t('manage.copyFailed')));
            }}
          >
            <Copy className="size-4" />
            {copied && t('detail.copied')}
          </button>
          <button
            ref={close}
            type="button"
            aria-label={t('detail.close')}
            onClick={onClose}
            className="grid size-11 place-items-center rounded-row"
          >
            <X className="size-5" />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-auto p-5">
          <p className="mb-4 break-words text-base">{eventMessage(item, type, rules)}</p>
          {error && (
            <p role="alert" className="text-err">
              {error}
            </p>
          )}
          <div className="mb-5 flex flex-wrap gap-2">
            {item.tags?.map((tag) => (
              <button type="button" className="manage-button text-xs" key={tag} onClick={() => onFilter('tag', tag)}>
                #{tag}
              </button>
            ))}
          </div>
          {groups.map((group) => (
            <section key={group.key} className="border-line mb-5 border-b pb-3">
              <h3 className="mb-3 font-semibold">{t(`manage.${group.key}`)}</h3>
              <dl className="grid gap-1">
                {group.entries.map(([key, value]) => field(key, value, group.key === 'who' || group.key === 'what'))}
              </dl>
            </section>
          ))}
          <details>
            <summary className="text-ink2 min-h-11 cursor-pointer content-center font-semibold">
              {t('manage.original')}
            </summary>
            <pre className="bg-sunk overflow-auto rounded-row p-3 text-xs">{JSON.stringify(item, null, 2)}</pre>
          </details>
        </div>
      </aside>
    </>
  );
}
