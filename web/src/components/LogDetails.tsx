import { useEffect, useRef, useState } from 'react';
import { Copy, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { LogItem } from '../lib/api';
import { attributeText, filterableValue, fullTimestamp, logEntities } from '../lib/logPresentation';

type Field = { label: string; value: unknown; filter?: string };

export default function LogDetails({ item, onClose, onFilter }: {
  item: LogItem;
  onClose: () => void;
  onFilter: (field: string, value: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const panelRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const json = JSON.stringify(item, null, 2);
  const entities = logEntities(item);

  useEffect(() => {
    const opener = document.activeElement;
    closeRef.current?.focus();
    return () => { if (opener instanceof HTMLElement && opener.isConnected) opener.focus(); };
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') return onClose();
      if (event.key !== 'Tab' || !panelRef.current) return;
      const stops = [...panelRef.current.querySelectorAll<HTMLElement>('button:not([disabled]), summary')]
        .filter(element => !element.closest('details:not([open])') || element.tagName === 'SUMMARY');
      const first = stops[0], last = stops[stops.length - 1];
      const inside = panelRef.current.contains(document.activeElement);
      if (!inside || (event.shiftKey && document.activeElement === first)) {
        event.preventDefault();
        (event.shiftKey ? last : first)?.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first?.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const copy = async () => {
    try {
      if (!navigator.clipboard) throw new Error('clipboard unavailable');
      await navigator.clipboard.writeText(json);
      setCopyState('copied');
    } catch { setCopyState('failed'); }
  };
  const fields = (title: string, rows: Field[]) => {
    const visible = rows.filter(row => row.value !== undefined && row.value !== null && row.value !== '');
    if (!visible.length) return null;
    return (
      <section className="grid gap-2" aria-label={title}>
        <h3 className="text-ink2 text-xs font-semibold">{title}</h3>
        <dl className="grid grid-cols-[90px_minmax(0,1fr)] items-start gap-x-3 gap-y-1 text-[13px]">
          {visible.map(row => {
            const value = filterableValue(row.value);
            return (
              <div key={row.label} className="contents">
                <dt className="text-ink2 min-w-0 py-2 break-words">{row.label}</dt>
                <dd className="min-w-0 font-mono text-xs [overflow-wrap:anywhere]">
                  {row.filter && value !== null ? <button type="button" onClick={() => onFilter(row.filter!, value)} aria-label={t('logDetails.filterValue', { field: row.label })} className="text-hue hover:underline min-h-11 min-w-11 max-w-full py-2 text-left [overflow-wrap:anywhere] md:min-h-9 md:min-w-0">{attributeText(row.value)}</button> : <span className="block py-2 whitespace-pre-wrap">{attributeText(row.value)}</span>}
                </dd>
              </div>
            );
          })}
        </dl>
      </section>
    );
  };

  return (
    <aside ref={panelRef} role="dialog" aria-modal="true" aria-labelledby="detail-title" className="bg-surface border-line fixed inset-0 z-50 flex min-w-0 flex-col md:static md:z-auto md:w-[340px] md:flex-none md:border-l xl:w-[380px]">
      <header className="border-line flex min-h-16 flex-none items-center gap-2 border-b px-4 py-2">
        <h2 id="detail-title" className="min-w-0 flex-1 truncate text-sm font-bold">{t('detail.title')}</h2>
        <button type="button" onClick={() => void copy()} className="text-ink2 hover:text-ink flex min-h-11 items-center gap-1.5 px-2 whitespace-nowrap md:min-h-9"><Copy className="size-4" aria-hidden />{copyState === 'copied' ? t('detail.copied') : t('detail.copy')}</button>
        <button ref={closeRef} type="button" aria-label={t('detail.close')} onClick={onClose} className="text-ink2 hover:text-ink grid size-11 flex-none place-items-center md:size-9"><X className="size-5" aria-hidden /></button>
      </header>
      <div className="min-h-0 flex-1 space-y-5 overflow-auto p-4">
        {copyState === 'failed' && <p role="status" className="text-err text-xs">{t('logDetails.copyFailed')}</p>}
        <p className="text-base leading-relaxed [overflow-wrap:anywhere]">{item.message || item.event}</p>
        {fields(t('logDetails.who'), entities.flatMap(({ key, entity }) => [
          { label: t(`logDetails.${key}`), value: entity.name || entity.id, filter: `${key}.${entity.name ? 'name' : 'id'}` },
          ...(entity.name && entity.id !== undefined ? [{ label: `${t(`logDetails.${key}`)} · id`, value: entity.id, filter: `${key}.id` }] : []),
          ...(entity.type ? [{ label: `${t(`logDetails.${key}`)} · type`, value: entity.type, filter: `${key}.type` }] : []),
        ]))}
        {fields(t('logDetails.what'), Object.entries(item.attributes ?? {}).filter(([key]) => !entities.some(entity => entity.key === key)).map(([key, value]) => ({ label: key, value, filter: key })))}
        {fields(t('logDetails.where'), [
          { label: t('explorer.col.stream'), value: item.stream }, { label: t('detail.environment'), value: item.environment },
          { label: t('explorer.col.resource'), value: item.resource }, { label: t('detail.service'), value: item.service }, { label: t('detail.host'), value: item.host },
        ])}
        {fields(t('logDetails.when'), [
          { label: t('detail.time'), value: fullTimestamp(item.timestamp, i18n.language) },
          { label: t('detail.received'), value: item.ingested_at ? fullTimestamp(item.ingested_at, i18n.language) : '' },
        ])}
        {fields(t('logDetails.technical'), [
          { label: t('explorer.col.event'), value: item.event }, { label: t('explorer.col.level'), value: t(`explorer.levelNames.${item.level}`, { defaultValue: item.level }) },
          { label: t('detail.trace'), value: item.trace_id }, { label: 'span_id', value: item.span_id }, { label: 'id', value: item.id },
        ])}
        <details className="border-line border-t pt-2"><summary className="text-ink2 min-h-11 cursor-pointer py-3 text-[13px] md:min-h-9">{t('logDetails.original')}</summary><pre className="bg-sunk rounded-row p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere]">{json}</pre></details>
      </div>
    </aside>
  );
}
