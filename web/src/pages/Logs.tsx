import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useNavigate, useOutletContext, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Pause, Play, Search, SlidersHorizontal, X } from 'lucide-react';
import type { ShellContext } from '../components/Shell';
import { api, ApiError, tailUrl, type ExportRequest, type LogItem, type Project, type SavedFilter } from '../lib/api';
import { useAuth } from '../lib/auth';
import { cn } from '../lib/cn';
import { formatQuery, parseQuery } from '../lib/query';
import LogViews from '../components/LogViews';
import LogDetails from '../components/LogDetails';
import LogRows from '../components/LogRows';
import { LOG_LEVELS, levelColor } from '../lib/logPresentation';
import { projectPath } from '../lib/sections';
import FirstEvent from './FirstEvent';

const RANGES = [
  { key: '15m', ms: 15 * 60_000 },
  { key: '1h', ms: 3_600_000 },
  { key: '24h', ms: 86_400_000 },
  { key: '7d', ms: 7 * 86_400_000 },
  { key: '30d', ms: 30 * 86_400_000 },
] as const;
type RangeKey = (typeof RANGES)[number]['key'];

const PAGE = 100;
/** The table stops growing here; past it the user narrows the search. Keeps the DOM bounded. */
const MAX_ROWS = 1000;
/** Live rows kept above the search results. */
const LIVE_MAX = 500;

type Loaded =
  | { status: 'ready'; items: LogItem[]; next: string | null; from: string; total: number | null; allCounts: Record<string, number>; partial: boolean; anyEvents: boolean }
  | { status: 'failed'; message: string };
type LiveState = 'off' | 'connecting' | 'open' | 'retrying' | 'failed';

const time = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
};

const rangeStart = (key: RangeKey) => new Date(Date.now() - RANGES.find((r) => r.key === key)!.ms).toISOString();
/** A range left in sessionStorage by an older build or by hand is ignored. */
const storedRange = (): RangeKey => RANGES.find((r) => r.key === sessionStorage.getItem('range'))?.key ?? '24h';

export default function Logs({ project, live: startLive }: { project: Project; live: boolean }) {
  const { t, i18n } = useTranslation();
  const { retry } = useAuth();
  const { streams, environment, environments, setEnvironment, topbar } = useOutletContext<ShellContext>();
  const { streamId } = useParams();
  const streamName = streams.find((s) => s.id === streamId)?.name ?? '';

  const [input, setInput] = useState('');
  const [applied, setApplied] = useState('');
  const [levels, setLevels] = useState<string[]>([]);
  const [range, setRange] = useState<RangeKey>(storedRange);
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; countKey: string; result: Loaded } | null>(null);
  const [more, setMore] = useState<{ key: string; busy: boolean; error: boolean }>({ key: '', busy: false, error: false });
  const [live, setLive] = useState(startLive);
  const [liveState, setLiveState] = useState<LiveState>('off');
  const [liveRows, setLiveRows] = useState<{ key: string; items: LogItem[]; at: string | null }>({ key: '', items: [], at: null });
  const [selected, setSelected] = useState<LogItem | null>(null);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [now, setNow] = useState(Date.now);
  const lastCursor = useRef<{ key: string; id: string } | null>(null);
  const filtersRef = useRef<HTMLElement>(null);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!filtersOpen || !window.matchMedia?.('(max-width:720px)').matches) return;
    const opener = document.activeElement;
    const panel = filtersRef.current;
    const stops = panel?.querySelectorAll<HTMLElement>('button, select');
    const first = stops?.[0], last = stops?.[stops.length - 1];
    first?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setFiltersOpen(false);
      if (event.key !== 'Tab') return;
      if (!panel?.contains(document.activeElement) || (event.shiftKey && document.activeElement === first)) {
        event.preventDefault();
        (event.shiftKey ? last : first)?.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first?.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, [filtersOpen]);

  const parsed = useMemo(() => parseQuery(applied), [applied]);
  const envName = environment?.name ?? '';
  // Everything that changes the result set; a result counts only for the key that asked for it.
  const unfiltered = !applied && levels.length === 0 && !streamName && !envName;
  const key = JSON.stringify([project.id, streamName, envName, applied, levels, range, attempt]);
  const countKey = JSON.stringify([project.id, streamName, envName, applied, range, attempt]);

  // Every page of a search shares the `from` of its first one, so "load more" never moves the start of the range.
  const baseSearch = useCallback(
    (from: string) => ({
      project_id: project.id,
      from,
      query: parsed.text || undefined,
      filters: parsed.filters.length ? parsed.filters : undefined,
      streams: streamName ? [streamName] : undefined,
      environments: envName ? [envName] : undefined,
    }),
    [project.id, parsed, streamName, envName],
  );
  const request = useCallback(
    (from: string, cursor?: string) =>
      api.searchLogs({ ...baseSearch(from), levels: levels.length ? levels : undefined, limit: PAGE, cursor, include_total: !cursor }),
    [baseSearch, levels],
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
    const from = rangeStart(range);
    request(from)
      .then(async (r) => {
        const items = r.items ?? [];
        // An empty, unfiltered range may still hide older events: only a project with none at all gets the setup guide.
        let anyEvents = items.length > 0;
        if (!anyEvents && unfiltered) {
          const probe = await api.searchLogs({ project_id: project.id, from: rangeStart(RANGES[RANGES.length - 1].key), limit: 1 }, ctrl.signal);
          anyEvents = (probe.items ?? []).length > 0;
        }
        setLoaded({
          key,
          countKey,
          result: {
            status: 'ready',
            items,
            next: r.next_cursor,
            from,
            total: r.total ?? null,
            allCounts: r.level_counts ?? {},
            partial: r.partial,
            anyEvents,
          },
        });
      })
      .catch((err: unknown) => {
        if (!ctrl.signal.aborted) setLoaded({ key, countKey, result: { status: 'failed', message: fail(err) } });
      });
    return () => ctrl.abort();
  }, [key, countKey, request, range, fail, unfiltered, project.id]);

  const result = loaded?.key === key ? loaded.result : null;
  const items = result?.status === 'ready' ? result.items : [];
  const liveItems = liveRows.key === key ? liveRows.items : [];

  const loadMore = () => {
    if (result?.status !== 'ready' || !result.next) return;
    setMore({ key, busy: true, error: false });
    request(result.from, result.next)
      .then((r) => {
        setLoaded((cur) =>
          cur?.key === key && cur.result.status === 'ready'
            ? { ...cur, result: { ...cur.result, items: [...cur.result.items, ...(r.items ?? [])], next: r.next_cursor } }
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
    // A refusal (400, 403) closes the source for good; only a dropped connection is retried by the browser.
    src.onerror = () => setLiveState(src.readyState === EventSource.CLOSED ? 'failed' : 'retrying');
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
  const closeDetail = useCallback(() => setSelected(null), [setSelected]);
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
    environments: envName ? [envName] : undefined,
  };
  const applyView = (view: SavedFilter) => {
    const text = formatQuery(view.query ?? '', view.filters ?? []);
    setInput(text);
    setApplied(text);
    setLevels(view.levels ?? []);
    setEnvironment(environments.find((e) => e.name === view.environments?.[0])?.id ?? '');
    const known = RANGES.find((r) => r.key === view.range);
    if (known) pickRange(known.key);
    const target = view.streams?.length ? streams.find((s) => s.name === view.streams![0]) : undefined;
    if (target?.id !== streamId) navigate(projectPath(project.workspace_id, project.id, target ? `streams/${target.id}` : ''));
  };
  const exportRequest = (format: ExportRequest['format']): ExportRequest => ({
    ...filter,
    project_id: project.id,
    from: rangeStart(range),
    format,
  });

  // Live rows can repeat an event the search already returned; those stay with the search rows.
  const loadedIds = new Set(items.map((i) => i.id));
  const freshLive = liveItems.filter((i) => !loadedIds.has(i.id));
  const shown = freshLive.length ? [...freshLive, ...items] : items;

  const allCounts = loaded?.countKey === countKey && loaded.result.status === 'ready' ? loaded.result.allCounts : {};
  const total = result?.status === 'ready' ? result.total : null;
  const errors = (levels.length === 0 || levels.includes('ERROR') ? allCounts.ERROR ?? 0 : 0)
    + (levels.length === 0 || levels.includes('FATAL') ? allCounts.FATAL ?? 0 : 0);
  const fmt = (n: number) => n.toLocaleString(i18n.language);
  const empty = result?.status === 'ready' && shown.length === 0;
  const liveAt = liveRows.key === key && liveRows.at ? time(liveRows.at) : null;

  const title = streamName ? `# ${streamName}` : t('nav.explorer');
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
            {liveState === 'open' ? (liveAt ? t('live.at', { time: liveAt }) : t('live.on')) : liveState === 'failed' ? t('live.failed') : t('live.connecting')}
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
          <div className="bg-sunk border-field focus-within:outline-hue flex h-12 items-center gap-2 rounded-row border px-3 focus-within:outline-2 md:h-10">
            <Search className="text-ink3 size-4 flex-none" aria-hidden />
            <input
              id="log-q"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={t('explorer.searchPlaceholder')}
              aria-describedby="log-q-hint"
              className="placeholder:text-ink3 h-11 min-w-0 flex-1 bg-transparent text-base outline-none md:h-9 md:text-sm"
            />
            {input && (
              <button type="button" aria-label={t('explorer.clear')} onClick={() => {
                  setInput('');
                  setApplied('');
                }} className="text-ink3 hover:text-ink -mr-3 grid size-11 place-items-center md:size-9">
                <X className="size-4" aria-hidden />
              </button>
            )}
          </div>
          <p id="log-q-hint" className="sr-only">
            {t('explorer.searchHint')}
          </p>
        </form>
        <button type="button" onClick={() => setFiltersOpen(value => !value)} aria-expanded={filtersOpen} aria-controls="log-filters" className="bg-sunk border-raised text-ink hover:bg-raised flex min-h-11 items-center gap-2 rounded-row border px-3 font-semibold md:min-h-10">
          <SlidersHorizontal className="size-4" aria-hidden />
          {t('explorer.filters')}
          {levels.length > 0 && <span className="text-hue text-xs">{levels.length}</span>}
        </button>
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

      <div className="relative flex min-h-0 flex-1 overflow-hidden">
        {filtersOpen && (
          <aside ref={filtersRef} id="log-filters" aria-label={t('explorer.filters')} className="bg-surface border-line fixed inset-0 z-40 flex flex-col overflow-auto p-4 md:static md:z-auto md:w-[184px] md:flex-none md:border-r md:p-3">
            <div className="mb-4 flex items-center justify-between gap-2">
              <h2 className="text-sm font-semibold">{t('explorer.filters')}</h2>
              <button type="button" onClick={() => setFiltersOpen(false)} aria-label={t('explorer.closeFilters')} className="text-ink2 hover:text-ink grid size-11 place-items-center md:size-9"><X className="size-4" aria-hidden /></button>
            </div>
            <div role="group" aria-label={t('explorer.levels')} className="mb-6 grid gap-1">
              <h3 className="text-ink2 mb-1 px-2 text-xs font-semibold">{t('explorer.levels')}</h3>
              {LOG_LEVELS.map(level => (
                <button key={level} type="button" aria-pressed={levels.includes(level)} onClick={() => toggleLevel(level)} className={cn('flex min-h-11 items-center gap-2 rounded-row px-2 text-left text-[13px] md:min-h-9', levels.includes(level) ? 'bg-sunk text-ink font-semibold' : 'text-ink2 hover:bg-sunk')}>
                  <span className={cn('size-1.5 rounded-full bg-current', levelColor(level))} aria-hidden />
                  {t(`explorer.levelNames.${level}`)}
                  <span className="text-ink2 ml-auto font-mono text-xs tabular-nums">{fmt(allCounts[level] ?? 0)}</span>
                </button>
              ))}
            </div>
            <label className="text-ink2 grid gap-2 text-xs font-semibold">
              {t('nav.streams')}
              <select aria-label={t('nav.streams')} value={streamId ?? ''} onChange={event => navigate(projectPath(project.workspace_id, project.id, event.target.value ? `streams/${event.target.value}` : ''))} className="bg-sunk border-field text-ink min-h-11 w-full min-w-0 rounded-row border px-2 text-[13px] font-normal md:min-h-9">
                <option value="">{t('explorer.allStreams')}</option>
                {streams.map(stream => <option key={stream.id} value={stream.id}>{stream.name}</option>)}
              </select>
            </label>
          </aside>
        )}
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
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
              <LogRows items={shown} selectedId={selected?.id} liveIds={new Set(freshLive.map(item => item.id))} now={now} onSelect={setSelected} />
            )}
          </div>

          {result?.status === 'ready' && shown.length > 0 && (
            <footer className="border-line flex flex-none items-center gap-3 border-t px-4 py-2.5">
              <p className="text-ink2 min-w-0 flex-1 truncate text-[13px] whitespace-nowrap">
                {total !== null ? t('explorer.shownOf', { shown: fmt(shown.length), total: fmt(total + freshLive.length) }) : t('explorer.shown', { shown: fmt(shown.length) })}
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
        </div>
        {selected && <LogDetails key={selected.id} item={selected} onClose={closeDetail} onFilter={(field, value) => {
          const text = formatQuery(parsed.text, [...parsed.filters.filter(filter => filter.field !== field), { field, op: 'eq', value }]);
          setInput(text);
          setApplied(text);
          if (window.matchMedia?.('(max-width:720px)').matches) closeDetail();
        }} />}
      </div>
    </>
  );
}
