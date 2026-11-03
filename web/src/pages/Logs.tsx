import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useNavigate, useOutletContext, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Copy, Pause, Play, Search, X } from 'lucide-react';
import type { ShellContext } from '../components/Shell';
import { api, ApiError, tailUrl, type ExportRequest, type LogItem, type Project, type SavedFilter, type SearchResult } from '../lib/api';
import { useAuth } from '../lib/auth';
import { cn } from '../lib/cn';
import { formatQuery, parseQuery } from '../lib/query';
import LogViews from '../components/LogViews';
import FirstEvent from './FirstEvent';

const RANGES = [
  { key: '15m', ms: 15 * 60_000 },
  { key: '1h', ms: 3_600_000 },
  { key: '24h', ms: 86_400_000 },
  { key: '7d', ms: 7 * 86_400_000 },
  { key: '30d', ms: 30 * 86_400_000 },
] as const;
type RangeKey = (typeof RANGES)[number]['key'];

/** Severity order, highest first. The server stores these names. */
const LEVELS = ['FATAL', 'ERROR', 'WARN', 'NOTICE', 'INFO', 'DEBUG', 'TRACE'];
const PAGE = 100;
/** The table stops growing here; past it the user narrows the search. Keeps the DOM bounded. */
const MAX_ROWS = 1000;
/** Live rows kept above the search results. */
const LIVE_MAX = 500;

type Loaded =
  | { status: 'ready'; items: LogItem[]; next: string | null; total: number | null; counts: Record<string, number>; tiers: string[]; partial: boolean; anyEvents: boolean }
  | { status: 'failed'; message: string };
type LiveState = 'off' | 'connecting' | 'open' | 'retrying';

const levelClass = (level: string) =>
  level === 'FATAL' ? 'bg-err text-bg rounded px-1 font-semibold' : level === 'ERROR' ? 'text-err' : level === 'WARN' ? 'text-warn' : 'text-ink2';
const isBad = (level: string) => level === 'ERROR' || level === 'FATAL';

const time = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
};

/** "key value, key value" for the message column; nested objects stay JSON. */
function attrsInline(attrs: LogItem['attributes']): string {
  if (!attrs) return '';
  return Object.entries(attrs)
    .map(([k, v]) => `${k} ${typeof v === 'string' ? v : JSON.stringify(v)}`)
    .join(', ');
}

export default function Logs({ project, live: startLive }: { project: Project; live: boolean }) {
  const { t, i18n } = useTranslation();
  const { retry } = useAuth();
  const { streams, environment, topbar } = useOutletContext<ShellContext>();
  const { streamId } = useParams();
  const streamName = streams.find((s) => s.id === streamId)?.name ?? '';

  const [input, setInput] = useState('');
  const [applied, setApplied] = useState('');
  const [levels, setLevels] = useState<string[]>([]);
  const [range, setRange] = useState<RangeKey>(() => (sessionStorage.getItem('range') as RangeKey | null) ?? '24h');
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; result: Loaded } | null>(null);
  const [more, setMore] = useState<{ key: string; busy: boolean; error: boolean }>({ key: '', busy: false, error: false });
  const [live, setLive] = useState(startLive);
  const [liveState, setLiveState] = useState<LiveState>('off');
  const [liveRows, setLiveRows] = useState<{ key: string; items: LogItem[]; at: string | null }>({ key: '', items: [], at: null });
  const [selected, setSelected] = useState<LogItem | null>(null);
  const lastCursor = useRef<{ key: string; id: string } | null>(null);

  const parsed = useMemo(() => parseQuery(applied), [applied]);
  const envName = environment?.name ?? '';
  // Everything that changes the result set; a result counts only for the key that asked for it.
  const unfiltered = !applied && levels.length === 0 && !streamName && !envName;
  const key = JSON.stringify([project.id, streamName, envName, applied, levels, range, attempt]);

  const request = useCallback(
    (cursor?: string) => {
      const span = RANGES.find((r) => r.key === range)!.ms;
      return api.searchLogs({
        project_id: project.id,
        from: new Date(Date.now() - span).toISOString(),
        query: parsed.text || undefined,
        filters: parsed.filters.length ? parsed.filters : undefined,
        levels: levels.length ? levels : undefined,
        streams: streamName ? [streamName] : undefined,
        environments: envName ? [envName] : undefined,
        limit: PAGE,
        cursor,
        include_total: !cursor,
      });
    },
    [project.id, range, parsed, levels, streamName, envName],
  );

  const fail = useCallback(
    (err: unknown) => {
      if (err instanceof ApiError && err.status === 401) retry();
      return err instanceof ApiError && err.status < 500 ? err.message : t('explorer.failed');
    },
    [retry, t],
  );

  useEffect(() => {
    const ctrl = new AbortController();
    request(undefined)
      .then(async (r: SearchResult) => {
        const items = r.items ?? [];
        // An empty, unfiltered range may still hide older events: only a project with none at all gets the setup guide.
        let anyEvents = items.length > 0;
        if (!anyEvents && unfiltered) {
          const probe = await api.searchLogs({ project_id: project.id, from: new Date(Date.now() - RANGES[RANGES.length - 1].ms).toISOString(), limit: 1 }, ctrl.signal);
          anyEvents = (probe.items ?? []).length > 0;
        }
        setLoaded({
          key,
          result: { status: 'ready', items, next: r.next_cursor, total: r.total ?? null, counts: r.level_counts ?? {}, tiers: r.searched_tiers ?? [], partial: r.partial, anyEvents },
        });
      })
      .catch((err: unknown) => {
        if (!ctrl.signal.aborted) setLoaded({ key, result: { status: 'failed', message: fail(err) } });
      });
    return () => ctrl.abort();
  }, [key, request, fail, unfiltered, project.id]);

  const result = loaded?.key === key ? loaded.result : null;
  const items = result?.status === 'ready' ? result.items : [];
  const liveItems = liveRows.key === key ? liveRows.items : [];

  const loadMore = () => {
    if (result?.status !== 'ready' || !result.next) return;
    setMore({ key, busy: true, error: false });
    request(result.next)
      .then((r) => {
        setLoaded((cur) =>
          cur?.key === key && cur.result.status === 'ready'
            ? { key, result: { ...cur.result, items: [...cur.result.items, ...(r.items ?? [])], next: r.next_cursor } }
            : cur,
        );
        setMore({ key, busy: false, error: false });
      })
      .catch((err: unknown) => {
        fail(err);
        setMore({ key, busy: false, error: true });
      });
  };

  // Live tail: the server applies the same stream, environment, level and text filters.
  // Attribute filters are not supported by the tail, so live stays off while they are set.
  const liveAllowed = parsed.filters.length === 0 && typeof EventSource !== 'undefined';
  const liveOn = live && liveAllowed;
  useEffect(() => {
    if (!liveOn) return;
    const resume = lastCursor.current?.key === key ? lastCursor.current.id : undefined;
    const src = new EventSource(
      tailUrl({
        project_id: project.id,
        streams: streamName || undefined,
        environments: envName || undefined,
        levels: levels.join(',') || undefined,
        query: parsed.text || undefined,
        last_event_id: resume,
      }),
    );
    src.onopen = () => setLiveState('open');
    src.onerror = () => setLiveState('retrying');
    src.addEventListener('log', (ev) => {
      const msg = ev as MessageEvent<string>;
      let item: LogItem;
      try {
        item = JSON.parse(msg.data) as LogItem;
      } catch {
        return;
      }
      if (msg.lastEventId) lastCursor.current = { key, id: msg.lastEventId };
      setLiveRows((cur) => {
        const base = cur.key === key ? cur.items : [];
        if (base.some((x) => x.id === item.id)) return cur;
        return { key, items: [item, ...base].slice(0, LIVE_MAX), at: new Date().toISOString() };
      });
    });
    // The server lost track of this client: the search reloads, so nothing in between goes missing.
    src.addEventListener('reset', () => {
      lastCursor.current = null;
      setLiveRows({ key: '', items: [], at: null });
      setAttempt((n) => n + 1);
    });
    return () => {
      src.close();
      setLiveState('off');
    };
  }, [liveOn, key, project.id, streamName, envName, levels, parsed.text]);

  const reload = useCallback(() => setAttempt((n) => n + 1), [setAttempt]);
  const toggleLevel = (level: string) => setLevels((cur) => (cur.includes(level) ? cur.filter((l) => l !== level) : [...cur, level]));
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setApplied(input.trim());
  };
  const pickRange = (value: RangeKey) => {
    sessionStorage.setItem('range', value);
    setRange(value);
  };

  const navigate = useNavigate();
  const filter: SavedFilter = {
    query: parsed.text || undefined,
    filters: parsed.filters.length ? parsed.filters : undefined,
    levels: levels.length ? levels : undefined,
    streams: streamName ? [streamName] : undefined,
  };
  const applyView = (view: SavedFilter) => {
    const text = formatQuery(view.query ?? '', view.filters ?? []);
    setInput(text);
    setApplied(text);
    setLevels(view.levels ?? []);
    const known = RANGES.find((r) => r.key === view.range);
    if (known) pickRange(known.key);
    const target = view.streams?.length ? streams.find((s) => s.name === view.streams![0]) : undefined;
    if (target?.id !== streamId) navigate(target ? `/projects/${project.id}/streams/${target.id}` : `/projects/${project.id}`);
  };
  const exportRequest = (format: ExportRequest['format']): ExportRequest => ({
    ...filter,
    project_id: project.id,
    from: new Date(Date.now() - RANGES.find((r) => r.key === range)!.ms).toISOString(),
    environments: envName ? [envName] : undefined,
    format,
  });

  // Live rows can repeat an event the search already returned.
  const seen = new Set(liveItems.map((i) => i.id));
  const shown = liveItems.length ? [...liveItems, ...items.filter((i) => !seen.has(i.id))] : items;

  const counts = result?.status === 'ready' ? result.counts : {};
  const chipLevels = LEVELS.filter((l) => (counts[l] ?? 0) > 0 || levels.includes(l));
  const total = result?.status === 'ready' ? result.total : null;
  const errors = (counts.ERROR ?? 0) + (counts.FATAL ?? 0);
  const fmt = (n: number) => n.toLocaleString(i18n.language);
  const empty = result?.status === 'ready' && shown.length === 0;
  const hot = result?.status === 'ready' && result.tiers.includes('hot');
  const liveAt = liveRows.key === key && liveRows.at ? time(liveRows.at) : null;

  const title = streamName ? `# ${streamName}` : live ? t('nav.live') : t('explorer.allStreams');
  const header = (
    <div className="flex min-w-0 items-center gap-3">
      <h1 className="min-w-0 truncate text-base font-bold">{title}</h1>
      {total !== null && (
        <p className="text-ink2 hidden min-w-0 items-center gap-3 border-l border-line pl-3 text-[13px] whitespace-nowrap lg:flex">
          <span>{t('explorer.events', { count: total, n: fmt(total) })}</span>
          {errors > 0 && <span className="text-err">{t('explorer.errors', { count: errors, n: fmt(errors) })}</span>}
        </p>
      )}
      <div className="ml-auto flex flex-none items-center gap-2">
        {liveOn && (
          <span role="status" className="text-ink2 hidden items-center gap-1.5 text-[13px] whitespace-nowrap sm:flex">
            <span aria-hidden className={cn('size-2 rounded-full', liveState === 'open' ? 'bg-accent' : 'bg-field')} />
            {liveState === 'open' ? (liveAt ? t('live.at', { time: liveAt }) : t('live.on')) : t('live.connecting')}
          </span>
        )}
        <button
          type="button"
          onClick={() => setLive((v) => !v)}
          disabled={!liveAllowed}
          aria-pressed={liveOn}
          title={parsed.filters.length ? t('live.noFilters') : undefined}
          className="bg-sunk border-raised text-ink hover:bg-raised flex h-11 items-center gap-1.5 rounded-row border px-3 font-semibold whitespace-nowrap disabled:opacity-50 md:h-9"
        >
          {liveOn ? <Pause className="size-4" aria-hidden /> : <Play className="size-4" aria-hidden />}
          {liveOn ? t('live.pause') : t('live.start')}
        </button>
      </div>
    </div>
  );

  return (
    <>
      {topbar(header)}
      <div className="border-line flex flex-none flex-wrap items-center gap-2 border-b px-3 py-2.5 md:px-4">
        <form role="search" onSubmit={submit} className="min-w-[min(100%,240px)] flex-1">
          <label htmlFor="log-q" className="sr-only">
            {t('explorer.search')}
          </label>
          <div className="bg-sunk border-field focus-within:outline-hue flex h-11 items-center gap-2 rounded-row border px-3 focus-within:outline-2 md:h-10">
            <Search className="text-ink3 size-4 flex-none" aria-hidden />
            <input
              id="log-q"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={t('explorer.searchPlaceholder')}
              aria-describedby="log-q-hint"
              className="placeholder:text-ink3 min-w-0 flex-1 bg-transparent text-base outline-none md:text-sm"
            />
            {input && (
              <button type="button" aria-label={t('explorer.clear')} onClick={() => {
                  setInput('');
                  setApplied('');
                }} className="text-ink3 hover:text-ink -mr-2 grid size-9 place-items-center">
                <X className="size-4" aria-hidden />
              </button>
            )}
          </div>
          <p id="log-q-hint" className="sr-only">
            {t('explorer.searchHint')}
          </p>
        </form>
        {chipLevels.length > 0 && (
          <div role="group" aria-label={t('explorer.levels')} className="bg-sunk border-raised flex h-11 items-center gap-0.5 rounded-row border p-1 md:h-10">
            {chipLevels.map((l) => (
              <button
                key={l}
                type="button"
                aria-pressed={levels.includes(l)}
                onClick={() => toggleLevel(l)}
                className={cn('flex h-full items-center gap-1.5 rounded-[5px] px-2 text-[13px] whitespace-nowrap', levels.includes(l) ? 'bg-raised text-ink font-semibold' : 'text-ink2 hover:text-ink')}
              >
                {l.toLowerCase()}
                <span className="text-ink3 tabular-nums">{fmt(counts[l] ?? 0)}</span>
              </button>
            ))}
          </div>
        )}
        <label className="sr-only" htmlFor="log-range">
          {t('explorer.range')}
        </label>
        <select id="log-range" value={range} onChange={(e) => pickRange(e.target.value as RangeKey)} className="bg-sunk border-raised text-ink h-11 rounded-row border px-2 md:h-10">
          {RANGES.map((r) => (
            <option key={r.key} value={r.key}>
              {t(`explorer.ranges.${r.key}`)}
            </option>
          ))}
        </select>
        {!startLive && <LogViews project={project} current={{ ...filter, range }} onApply={applyView} exportRequest={exportRequest} />}
      </div>

      {result?.status === 'ready' && result.partial && (
        <p role="status" className="text-warn border-line border-b px-4 py-2 text-[13px]">
          {t('explorer.partial')}
        </p>
      )}

      <div className="relative min-h-0 flex-1 overflow-auto">
        {!result && (
          <p role="status" className="text-ink2 flex items-center gap-2 p-6">
            <span className="spinner" aria-hidden />
            {t('explorer.loading')}
          </p>
        )}
        {result?.status === 'failed' && (
          <div role="alert" className="p-6">
            <p className="text-ink2 mb-2">{result.message}</p>
            <button type="button" onClick={() => setAttempt((n) => n + 1)} className="text-hue min-h-11 font-semibold underline underline-offset-2">
              {t('common.retry')}
            </button>
          </div>
        )}
        {empty && !result.anyEvents && unfiltered && !liveOn && <FirstEvent project={project} onEvent={reload} />}
        {empty && (result.anyEvents || !unfiltered || liveOn) && (
          <div className="p-6 text-center">
            <h2 className="mb-1 text-[17px] font-bold">{liveOn ? t('live.waitingTitle') : t('explorer.noMatchTitle')}</h2>
            <p className="text-ink2">{liveOn ? t('live.waitingText') : t('explorer.noMatchText')}</p>
          </div>
        )}
        {shown.length > 0 && (
          <table className="w-full table-fixed border-collapse text-[13px]">
            <caption className="sr-only">{t('explorer.caption')}</caption>
            <thead className="bg-bg text-ink2 sticky top-0 z-10 text-left text-[12px]">
              <tr className="border-line border-b">
                <th scope="col" className="w-[76px] py-2 pl-4 font-medium md:w-[88px]">{t('explorer.col.time')}</th>
                <th scope="col" className="w-[64px] py-2 font-medium">{t('explorer.col.level')}</th>
                <th scope="col" className="hidden w-[120px] py-2 font-medium lg:table-cell">{t('explorer.col.stream')}</th>
                <th scope="col" className="hidden w-[130px] py-2 font-medium xl:table-cell">{t('explorer.col.resource')}</th>
                <th scope="col" className="hidden w-[180px] py-2 font-medium md:table-cell">{t('explorer.col.event')}</th>
                <th scope="col" className="py-2 font-medium">{t('explorer.col.message')}</th>
                <th scope="col" className="hidden w-[76px] py-2 pr-4 font-medium sm:table-cell">{t('explorer.col.source')}</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((item, i) => {
                const fromLive = i < liveItems.length;
                return (
                  <tr
                    key={item.id}
                    onClick={() => setSelected(item)}
                    className={cn('border-line hover:bg-sunk cursor-pointer border-b', isBad(item.level) && 'err-tint', selected?.id === item.id && 'outline-hue outline-1 -outline-offset-1')}
                  >
                    <td className="text-ink2 py-2.5 pl-4 font-mono tabular-nums">
                      <button type="button" onClick={() => setSelected(item)} className="text-left focus-visible:outline-offset-1" aria-label={t('explorer.open', { time: time(item.timestamp) })}>
                        {time(item.timestamp)}
                      </button>
                    </td>
                    <td className="py-2.5 font-mono">
                      <span className={levelClass(item.level)}>{item.level.toLowerCase()}</span>
                    </td>
                    <td className="text-ink2 hidden truncate py-2.5 pr-2 lg:table-cell">
                      <span aria-hidden className="text-ink3">#</span>
                      {item.stream}
                    </td>
                    <td className="text-ink2 hidden truncate py-2.5 pr-2 font-mono xl:table-cell">{item.resource || item.service}</td>
                    <td className="hidden truncate py-2.5 pr-2 font-mono md:table-cell">{item.event}</td>
                    <td className="truncate py-2.5 pr-3">
                      <span className="text-ink">{item.message}</span>
                      <span className="text-ink3 ml-2 font-mono text-[12px]">{attrsInline(item.attributes)}</span>
                    </td>
                    <td className="hidden py-2.5 pr-4 sm:table-cell">
                      <span className={cn('rounded px-1.5 py-0.5 text-[12px] font-semibold', fromLive ? 'text-hue border-hue border' : 'bg-raised text-ink')}>
                        {fromLive ? t('explorer.tier.live') : hot ? t('explorer.tier.hot') : t('explorer.tier.unknown')}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      {result?.status === 'ready' && shown.length > 0 && (
        <footer className="border-line flex flex-none items-center gap-3 border-t px-4 py-2.5">
          <p className="text-ink2 min-w-0 flex-1 truncate text-[13px] whitespace-nowrap">
            {total !== null ? t('explorer.shownOf', { shown: fmt(shown.length), total: fmt(total + liveItems.length) }) : t('explorer.shown', { shown: fmt(shown.length) })}
          </p>
          {more.key === key && more.error && <p role="alert" className="text-err text-[13px]">{t('explorer.moreFailed')}</p>}
          {result.next && items.length < MAX_ROWS && (
            <button type="button" onClick={loadMore} disabled={more.key === key && more.busy} className="bg-accent text-accent-ink flex h-11 items-center gap-2 rounded-row px-4 font-bold whitespace-nowrap hover:brightness-110 disabled:opacity-60 md:h-9">
              {more.key === key && more.busy && <span className="spinner" aria-hidden />}
              {t('explorer.more')}
            </button>
          )}
          {result.next && items.length >= MAX_ROWS && <p className="text-ink2 text-[13px]">{t('explorer.narrow')}</p>}
        </footer>
      )}

      {selected && <Detail item={selected} onClose={() => setSelected(null)} />}
    </>
  );
}

function Detail({ item, onClose }: { item: LogItem; onClose: () => void }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const closeRef = useRef<HTMLButtonElement>(null);
  const json = JSON.stringify(item, null, 2);

  useEffect(() => {
    closeRef.current?.focus();
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  const copy = () => {
    void navigator.clipboard?.writeText(json).then(() => setCopied(true));
  };
  const rows: [string, string][] = [
    [t('detail.time'), new Date(item.timestamp).toLocaleString()],
    [t('detail.received'), new Date(item.ingested_at).toLocaleString()],
    [t('explorer.col.level'), item.level.toLowerCase()],
    [t('explorer.col.stream'), item.stream],
    [t('detail.environment'), item.environment],
    [t('explorer.col.event'), item.event],
    [t('explorer.col.resource'), item.resource],
    [t('detail.service'), item.service],
    [t('detail.host'), item.host],
    [t('detail.trace'), item.trace_id],
    ['id', item.id],
  ];

  return (
    <>
      <div className="fixed inset-0 z-40 bg-black/50 md:hidden" onClick={onClose} aria-hidden />
      <aside
        role="dialog"
        aria-modal="false"
        aria-labelledby="detail-title"
        className="bg-surface border-line fixed inset-x-0 bottom-0 z-50 flex max-h-[85dvh] flex-col rounded-t-card border-t md:inset-y-0 md:right-0 md:left-auto md:max-h-none md:w-[440px] md:rounded-none md:border-t-0 md:border-l"
      >
        <header className="border-line flex flex-none items-center gap-2 border-b px-4 py-2">
          <h2 id="detail-title" className="min-w-0 flex-1 truncate font-bold">
            {item.event || t('detail.title')}
          </h2>
          <button type="button" onClick={copy} className="text-ink2 hover:text-ink flex h-11 items-center gap-1.5 px-2 whitespace-nowrap md:h-9">
            <Copy className="size-4" aria-hidden />
            {copied ? t('detail.copied') : t('detail.copy')}
          </button>
          <button ref={closeRef} type="button" aria-label={t('detail.close')} onClick={onClose} className="text-ink2 hover:text-ink grid size-11 place-items-center md:size-9">
            <X className="size-5" aria-hidden />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-auto p-4">
          <p className="mb-4 text-[15px] break-words">{item.message}</p>
          <dl className="mb-4 grid grid-cols-[110px_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-[13px]">
            {rows
              .filter(([, v]) => v)
              .map(([k, v]) => (
                <div key={k} className="contents">
                  <dt className="text-ink2">{k}</dt>
                  <dd className="font-mono break-all">{v}</dd>
                </div>
              ))}
          </dl>
          <h3 className="text-ink2 mb-1.5 text-[13px] font-semibold">{t('detail.attributes')}</h3>
          <pre className="bg-sunk overflow-auto rounded-row p-3 font-mono text-[12px] leading-relaxed">{JSON.stringify(item.attributes ?? {}, null, 2)}</pre>
        </div>
      </aside>
    </>
  );
}
