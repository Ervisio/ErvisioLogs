import { useCallback, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { request, type SearchRequest } from '../lib/api';
import { State, useRemote } from './Management';

export function FilterBuilder({ onAdd }: { onAdd: (term: string) => void }) {
  const { t } = useTranslation();
  const [field, setField] = useState('actor.id');
  const [op, setOp] = useState('=');
  const [value, setValue] = useState('');
  return (
    <form
      className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_100px_minmax(0,1fr)_auto]"
      onSubmit={(e) => {
        e.preventDefault();
        onAdd(op === 'has' ? `has:${field}` : `${field}${op}${JSON.stringify(value)}`);
        setValue('');
      }}
    >
      <label className="manage-field">
        <span className="text-xs">{t('manage.fieldPath')}</span>
        <input
          required
          pattern="[A-Za-z0-9_.-]+"
          className="manage-input"
          value={field}
          onChange={(e) => setField(e.target.value)}
        />
      </label>
      <label className="manage-field">
        <span className="text-xs">{t('manage.operator')}</span>
        <select className="manage-input" value={op} onChange={(e) => setOp(e.target.value)}>
          {['=', '!=', '~', '>', '>=', '<', '<=', 'has'].map((key) => (
            <option key={key}>{key}</option>
          ))}
        </select>
      </label>
      <label className="manage-field">
        <span className="text-xs">{t('manage.value')}</span>
        <input
          required={op !== 'has'}
          disabled={op === 'has'}
          className="manage-input"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
      </label>
      <button className="manage-button self-end">{t('manage.addFilter')}</button>
    </form>
  );
}
export type Bucket = { timestamp: string; count: number };
export function fillBuckets(
  items: Bucket[],
  from: string | undefined,
  to: string | undefined,
  interval: number,
): Bucket[] {
  if (!from || !to || !Number.isFinite(Date.parse(from)) || !Number.isFinite(Date.parse(to))) return items;
  const step = interval * 1000;
  const first = Math.floor(Date.parse(from) / step) * step;
  const count = Math.min(1000, Math.ceil((Date.parse(to) - first) / step));
  const counts = new Map(items.map((item) => [Math.floor(Date.parse(item.timestamp) / step) * step, item.count]));
  return Array.from({ length: Math.max(0, count) }, (_, index) => ({
    timestamp: new Date(first + index * step).toISOString(),
    count: counts.get(first + index * step) ?? 0,
  }));
}
export function Histogram({
  search,
  interval,
  onRange,
}: {
  search: SearchRequest;
  interval: number;
  onRange: (from: string, to: string) => void;
}) {
  const { t } = useTranslation();
  const serialized = JSON.stringify(search);
  const load = useCallback(
    (signal: AbortSignal) =>
      request<{ items: Bucket[]; partial: boolean }>(
        'POST',
        '/logs/histogram',
        { ...(JSON.parse(serialized) as SearchRequest), interval_seconds: interval },
        signal,
      ),
    [serialized, interval],
  );
  const state = useRemote(load);
  const start = useRef<number | null>(null);
  const [end, setEnd] = useState<number | null>(null);
  const [begin, setBegin] = useState<number | null>(null);
  const buckets = state.data ? fillBuckets(state.data.items, search.from, search.to, interval) : [];
  const max = Math.max(1, ...buckets.map((b) => b.count));
  const finish = (index: number) => {
    const items = buckets;
    const first = start.current;
    if (first === null || !items.length) return;
    const a = items[Math.min(first, index)];
    const b = items[Math.max(first, index)];
    if (a && b)
      onRange(
        new Date(Math.max(Date.parse(a.timestamp), Date.parse(search.from ?? a.timestamp))).toISOString(),
        new Date(
          Math.min(
            Date.parse(b.timestamp) + interval * 1000,
            Date.parse(search.to ?? new Date(Date.parse(b.timestamp) + interval * 1000).toISOString()),
          ),
        ).toISOString(),
      );
    start.current = null;
    setBegin(null);
    setEnd(null);
  };
  return (
    <section aria-label={t('manage.histogram')} className="border-line flex-none border-b px-4 py-3">
      <div className="text-ink3 mb-2 flex items-center justify-between text-xs">
        <h2>{t('manage.histogram')}</h2>
        <span className="hidden sm:inline">{t('manage.histogramHint')}</span>
      </div>
      <State {...state} retry={state.refresh} />
      {state.data?.partial && <p className="text-warn text-xs">{t('explorer.partial')}</p>}
      {state.data && (
        <div
          className="flex h-14 touch-none items-end gap-1"
          onPointerUp={(e) => {
            if (e.target === e.currentTarget) {
              start.current = null;
              setBegin(null);
              setEnd(null);
            }
          }}
          onPointerCancel={() => {
            start.current = null;
            setBegin(null);
            setEnd(null);
          }}
        >
          {buckets.map((bucket, index) => (
            <button
              type="button"
              key={bucket.timestamp}
              aria-label={`${new Date(bucket.timestamp).toLocaleString()}: ${bucket.count}`}
              title={`${new Date(bucket.timestamp).toLocaleString()}: ${bucket.count}`}
              className={`group relative flex h-full min-w-0 flex-1 items-end rounded-sm outline-offset-2 ${begin !== null && index >= Math.min(begin, end ?? begin) && index <= Math.max(begin, end ?? begin) ? 'bg-accent-soft' : 'hover:bg-sunk'}`}
              onPointerDown={() => {
                start.current = index;
                setBegin(index);
                setEnd(index);
              }}
              onPointerEnter={() => {
                if (start.current !== null) setEnd(index);
              }}
              onPointerUp={() => finish(index)}
              onClick={(e) => {
                if (e.detail === 0) {
                  start.current = index;
                  finish(index);
                }
              }}
            >
              <span
                className="bg-accent/65 group-hover:bg-accent w-full rounded-t-sm"
                style={{ height: `${bucket.count === 0 ? 0 : Math.max(3, (bucket.count / max) * 100)}%` }}
              />
            </button>
          ))}
        </div>
      )}
    </section>
  );
}
