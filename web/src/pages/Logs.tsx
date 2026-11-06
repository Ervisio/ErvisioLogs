import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useNavigate, useOutletContext, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Pause, Play, Search, SlidersHorizontal, X } from 'lucide-react';
import type { ShellContext } from '../components/Shell';
import {
  api,
  ApiError,
  tailUrl,
  type ExportRequest,
  type LogItem,
  type Project,
  type SavedFilter,
  type SearchResult,
} from '../lib/api';
import { useAuth } from '../lib/auth';
import { cn } from '../lib/cn';
import { formatQuery, parseQuery } from '../lib/query';
import LogViews from '../components/LogViews';
import FirstEvent from './FirstEvent';
import {
  CatalogIcon,
  EventDetail,
  eventMessage,
  relativeTime,
  attributeValue,
  hiddenField,
} from '../components/LogEvent';
import { FilterBuilder, Histogram } from '../components/LogFilters';
import { management } from '../lib/manage-api';
import { useRemote } from '../components/Management';
import { projectPath } from '../lib/paths';

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
  | {
      status: 'ready';
      items: LogItem[];
      next: string | null;
      from: string;
      total: number | null;
      counts: Record<string, number>;
      allCounts: Record<string, number>;
      partial: boolean;
      anyEvents: boolean;
    }
  | { status: 'failed'; message: string };
type LiveState = 'off' | 'connecting' | 'open' | 'retrying' | 'failed';

const levelClass = (level: string) =>
  ({
    FATAL: 'text-critical',
    ERROR: 'text-err',
    WARN: 'text-warn',
    NOTICE: 'text-notice',
    INFO: 'text-info',
    DEBUG: 'text-debug',
    TRACE: 'text-trace',
  })[level] ?? 'text-ink2';
const time = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? ''
    : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
};
const dateTime = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? ''
    : d.toLocaleString([], {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false,
      });
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
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [event, setEvent] = useState('');
  const [category, setCategory] = useState('');
  const [service, setService] = useState('');
  const [trace, setTrace] = useState('');
  const [customRange, setCustomRange] = useState<{ from: string; to: string } | null>(null);
  const [clock, setClock] = useState(Date.now);
  const [scroll, setScroll] = useState(0);
  const viewport = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ height: 600, row: window.innerWidth < 721 ? 54 : 42 });
  const loadCatalog = useCallback(
    async (signal: AbortSignal) => ({
      types: await management.eventTypes(project.id, signal),
      rules: await management.fieldRules(project.id, signal),
    }),
    [project.id],
  );
  const catalog = useRemote(loadCatalog);
  useEffect(() => {
    const timer = window.setInterval(() => setClock(Date.now()), 30000);
    const resize = () =>
      setSize({ height: viewport.current?.clientHeight || 600, row: window.innerWidth < 721 ? 54 : 42 });
    window.addEventListener('resize', resize);
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(resize);
    if (viewport.current) observer?.observe(viewport.current);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('resize', resize);
      observer?.disconnect();
    };
  }, []);
  const [applied, setApplied] = useState('');
  const [levels, setLevels] = useState<string[]>([]);
  const [range, setRange] = useState<RangeKey>(storedRange);
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; result: Loaded } | null>(null);
  const [more, setMore] = useState<{ key: string; busy: boolean; error: boolean }>({
    key: '',
    busy: false,
    error: false,
  });
  const [live, setLive] = useState(startLive);
  const [liveState, setLiveState] = useState<LiveState>('off');
  const [liveRows, setLiveRows] = useState<{ key: string; items: LogItem[]; at: string | null }>({
    key: '',
    items: [],
    at: null,
  });
  const [selected, setSelected] = useState<LogItem | null>(null);
  const lastCursor = useRef<{ key: string; id: string } | null>(null);

  const parsed = useMemo(() => parseQuery(applied), [applied]);
  const envName = environment?.name ?? '';
  // Everything that changes the result set; a result counts only for the key that asked for it.
  const unfiltered =
    !applied &&
    levels.length === 0 &&
    !streamName &&
    !envName &&
    !event &&
    !category &&
    !service &&
    !trace &&
    !customRange;
  const key = JSON.stringify([
    project.id,
    streamName,
    envName,
    applied,
    levels,
    range,
    attempt,
    customRange,
    event,
    category,
    service,
    trace,
  ]);
  const [anchor, setAnchor] = useState(() => ({ key, from: rangeStart(range) }));
  if (anchor.key !== key) setAnchor({ key, from: rangeStart(range) });

  // Every page of a search shares the `from` of its first one, so "load more" never moves the start of the range.
  const baseSearch = useCallback(
    (from: string) => ({
      project_id: project.id,
      from: customRange?.from ?? from,
      to:
        customRange?.to ??
        new Date(Date.parse(from) + (RANGES.find((r) => r.key === range)?.ms ?? 86400000)).toISOString(),
      events: event
        ? [event]
        : category
          ? catalog.data?.types.filter((type) => type.category === category).map((type) => type.name)
          : undefined,
      tags: parsed.tags,
      services: service ? [service] : undefined,
      trace_id: trace || undefined,
      query: parsed.text || undefined,
      filters: parsed.filters.length ? parsed.filters : undefined,
      streams: streamName ? [streamName] : undefined,
      environments: envName ? [envName] : undefined,
    }),
    [project.id, parsed, streamName, envName, customRange, event, category, catalog.data, service, trace, range],
  );
  const request = useCallback(
    (from: string, cursor?: string, signal?: AbortSignal) =>
      api.searchLogs(
        {
          ...baseSearch(from),
          levels: levels.length ? levels : undefined,
          limit: PAGE,
          cursor,
          include_total: !cursor,
        },
        signal,
      ),
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
    if (anchor.key !== key) return;
    const ctrl = new AbortController();
    const from = anchor.from;
    // The server counts levels with the level filter applied; the chips need the counts without it.
    Promise.all([
      request(from, undefined, ctrl.signal),
      levels.length ? api.searchLogs({ ...baseSearch(from), limit: 1, include_total: true }, ctrl.signal) : null,
    ])
      .then(async ([r, unleveled]: [SearchResult, SearchResult | null]) => {
        if (ctrl.signal.aborted) return;
        const items = r.items ?? [];
        // An empty, unfiltered range may still hide older events: only a project with none at all gets the setup guide.
        let anyEvents = items.length > 0;
        if (!anyEvents && unfiltered) {
          const probe = await api.searchLogs(
            { project_id: project.id, from: rangeStart(RANGES[RANGES.length - 1].key), limit: 1 },
            ctrl.signal,
          );
          anyEvents = (probe.items ?? []).length > 0;
        }
        if (ctrl.signal.aborted) return;
        setLoaded({
          key,
          result: {
            status: 'ready',
            items,
            next: r.next_cursor,
            from,
            total: r.total ?? null,
            counts: r.level_counts ?? {},
            allCounts: (unleveled ?? r).level_counts ?? {},
            partial: r.partial,
            anyEvents,
          },
        });
      })
      .catch((err: unknown) => {
        if (!ctrl.signal.aborted) setLoaded({ key, result: { status: 'failed', message: fail(err) } });
      });
    return () => ctrl.abort();
  }, [key, request, baseSearch, range, levels, fail, unfiltered, project.id, anchor]);

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
  const liveAllowed = typeof EventSource !== 'undefined';
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
        tags: parsed.tags?.join(','),
        filters: parsed.filters.length ? JSON.stringify(parsed.filters) : undefined,
        events:
          event ||
          (category
            ? catalog.data?.types
                .filter((type) => type.category === category)
                .map((type) => type.name)
                .join(',')
            : undefined),
        services: service || undefined,
        trace_id: trace || undefined,
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
        setLiveState('failed');
        src.close();
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
  }, [liveOn, key, project.id, streamName, envName, levels, parsed, event, category, catalog.data, service, trace]);

  const reload = useCallback(() => setAttempt((n) => n + 1), [setAttempt]);
  const closeDetail = useCallback(() => setSelected(null), [setSelected]);
  const toggleLevel = (level: string) =>
    setLevels((cur) => (cur.includes(level) ? cur.filter((l) => l !== level) : [...cur, level]));
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setApplied(input.trim());
  };
  const pickRange = (value: RangeKey) => {
    sessionStorage.setItem('range', value);
    setRange(value);
    setCustomRange(null);
  };

  const navigate = useNavigate();
  const filter: SavedFilter = {
    query: parsed.text || undefined,
    filters: parsed.filters.length ? parsed.filters : undefined,
    tags: parsed.tags,
    services: service ? [service] : undefined,
    trace_id: trace || undefined,
    events: event ? [event] : undefined,
    levels: levels.length ? levels : undefined,
    streams: streamName ? [streamName] : undefined,
    environments: envName ? [envName] : undefined,
  };
  const applyView = (view: SavedFilter) => {
    const text = formatQuery(view.query ?? '', view.filters ?? [], view.tags);
    setInput(text);
    setApplied(text);
    setLevels(view.levels ?? []);
    setEvent(view.events?.[0] ?? '');
    setService(view.services?.[0] ?? '');
    setTrace(view.trace_id ?? '');
    setCategory('');
    setEnvironment(environments.find((e) => e.name === view.environments?.[0])?.id ?? '');
    const known = RANGES.find((r) => r.key === view.range);
    if (known) pickRange(known.key);
    const target = view.streams?.length ? streams.find((s) => s.name === view.streams![0]) : undefined;
    if (target?.id !== streamId)
      navigate(projectPath(project.workspace_id, project.id, target ? `streams/${target.id}` : ''));
  };
  const exportRequest = (format: ExportRequest['format']): ExportRequest => ({
    ...filter,
    project_id: project.id,
    from: customRange?.from ?? rangeStart(range),
    to: customRange?.to,
    format,
  });

  // Live rows can repeat an event the search already returned; those stay with the search rows.
  const loadedIds = new Set(items.map((i) => i.id));
  const freshLive = liveItems.filter((i) => !loadedIds.has(i.id));
  const shown = freshLive.length ? [...freshLive, ...items] : items;
  const virtualStart = Math.max(0, Math.min(shown.length - 1, Math.floor(scroll / size.row)) - 6);
  const virtualEnd = Math.min(shown.length, virtualStart + Math.ceil(size.height / size.row) + 14);

  const counts = result?.status === 'ready' ? result.counts : {};
  const allCounts = result?.status === 'ready' ? result.allCounts : {};
  const chipLevels = LEVELS;
  const total = result?.status === 'ready' ? result.total : null;
  const errors = (counts.ERROR ?? 0) + (counts.FATAL ?? 0);
  const fmt = (n: number) => n.toLocaleString(i18n.language);
  const empty = result?.status === 'ready' && shown.length === 0;
  const liveAt = liveRows.key === key && liveRows.at ? time(liveRows.at) : null;

  const title = streamName ? `# ${streamName}` : live ? t('nav.live') : project.name;
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
            {liveState === 'open'
              ? liveAt
                ? t('live.at', { time: liveAt })
                : t('live.on')
              : liveState === 'failed'
                ? t('live.failed')
                : t('live.connecting')}
          </span>
        )}
        <button
          type="button"
          onClick={() => setLive((v) => !v)}
          disabled={!liveAllowed}
          aria-pressed={liveOn}
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
              <button
                type="button"
                aria-label={t('explorer.clear')}
                onClick={() => {
                  setInput('');
                  setApplied('');
                }}
                className="text-ink3 hover:text-ink -mr-2 grid size-9 place-items-center"
              >
                <X className="size-4" aria-hidden />
              </button>
            )}
          </div>
          <p id="log-q-hint" className="sr-only">
            {t('explorer.searchHint')}
          </p>
        </form>
        {filtersOpen && chipLevels.length > 0 && (
          <div
            role="group"
            aria-label={t('explorer.levels')}
            className="bg-sunk border-raised flex min-h-11 max-w-full flex-wrap items-center gap-0.5 rounded-row border p-1"
          >
            {chipLevels.map((l) => (
              <button
                key={l}
                type="button"
                aria-pressed={levels.includes(l)}
                onClick={() => toggleLevel(l)}
                className={cn(
                  'flex h-full items-center gap-1.5 rounded-[5px] px-2 text-[13px] whitespace-nowrap',
                  levels.includes(l) ? 'bg-raised text-ink font-semibold' : 'text-ink2 hover:text-ink',
                )}
              >
                {t(`levelNames.${l}`)}
                <span className="text-ink3 tabular-nums">{fmt(allCounts[l] ?? 0)}</span>
              </button>
            ))}
          </div>
        )}
        <label className="sr-only" htmlFor="log-range">
          {t('explorer.range')}
        </label>
        <select
          id="log-range"
          value={range}
          onChange={(e) => pickRange(e.target.value as RangeKey)}
          className="bg-sunk border-raised text-ink h-11 rounded-row border px-2 md:h-10"
        >
          {RANGES.map((r) => (
            <option key={r.key} value={r.key}>
              {t(`explorer.ranges.${r.key}`)}
            </option>
          ))}
        </select>
        <button
          type="button"
          aria-expanded={filtersOpen}
          onClick={() => setFiltersOpen((v) => !v)}
          className="manage-button"
        >
          <SlidersHorizontal className="size-4" />
          {t('manage.filters')}
        </button>
        {customRange && (
          <button type="button" onClick={() => setCustomRange(null)} className="manage-button">
            {t('manage.resetRange')}
          </button>
        )}
        {!startLive && (
          <LogViews
            project={project}
            current={{ ...filter, range }}
            onApply={applyView}
            exportRequest={exportRequest}
          />
        )}
      </div>

      {(service || trace) && (
        <div className="border-line flex gap-2 border-b p-3">
          {service && (
            <button type="button" className="manage-button" onClick={() => setService('')}>
              service: {service} ×
            </button>
          )}
          {trace && (
            <button type="button" className="manage-button" onClick={() => setTrace('')}>
              trace: {trace} ×
            </button>
          )}
        </div>
      )}
      {filtersOpen && (
        <section aria-label={t('manage.filters')} className="border-line grid max-h-[40dvh] flex-none gap-3 overflow-y-auto border-b px-4 py-3">
          {catalog.error && (
            <p role="alert" className="text-err text-xs">
              {catalog.error}
              <button className="manage-button ml-2" type="button" onClick={catalog.refresh}>
                {t('common.retry')}
              </button>
            </p>
          )}
          <div className="grid gap-3 sm:grid-cols-3">
            <label className="manage-field">
              <span className="text-xs">{t('nav.environment')}</span>
              <select
                className="manage-input"
                value={environment?.id ?? ''}
                onChange={(e) => setEnvironment(e.target.value)}
              >
                <option value="">{t('nav.allEnvironments')}</option>
                {environments.map((env) => (
                  <option value={env.id} key={env.id}>
                    {env.display_name || env.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="manage-field">
              <span className="text-xs">{t('nav.streams')}</span>
              <select
                className="manage-input"
                value={streamId ?? ''}
                onChange={(e) =>
                  navigate(
                    projectPath(project.workspace_id, project.id, e.target.value ? `streams/${e.target.value}` : ''),
                  )
                }
              >
                <option value="">{t('explorer.allStreams')}</option>
                {streams.map((s) => (
                  <option value={s.id} key={s.id}>
                    {s.display_name || s.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="manage-field">
              <span className="text-xs">{t('manage.filterEvents')}</span>
              <select className="manage-input" value={event} onChange={(e) => setEvent(e.target.value)}>
                <option value="">{t('manage.allEvents')}</option>
                {catalog.data?.types.map((type) => (
                  <option key={type.name} value={type.name}>
                    {type.title || type.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="manage-field">
              <span className="text-xs">{t('manage.category')}</span>
              <select
                className="manage-input"
                value={category}
                onChange={(e) => {
                  setCategory(e.target.value);
                  setEvent('');
                }}
              >
                <option value="">{t('manage.allEvents')}</option>
                {[...new Set(catalog.data?.types.map((type) => type.category).filter(Boolean))].map((value) => (
                  <option key={value}>{value}</option>
                ))}
              </select>
            </label>
          </div>
          <FilterBuilder
            onAdd={(term) => {
              const next = [applied, term].filter(Boolean).join(' ');
              setInput(next);
              setApplied(next);
            }}
          />
        </section>
      )}
      {anchor.key === key && (
        <Histogram
          search={{ ...baseSearch(customRange?.from ?? anchor.from), levels: levels.length ? levels : undefined }}
          interval={range === '15m' || range === '1h' ? 60 : range === '24h' ? 3600 : range === '7d' ? 21600 : 86400}
          onRange={(from, to) => setCustomRange({ from, to })}
        />
      )}
      {result?.status === 'ready' && result.partial && (
        <p role="status" className="text-warn border-line border-b px-4 py-2 text-[13px]">
          {t('explorer.partial')}
        </p>
      )}

      <div
        ref={viewport}
        onScroll={(e) => setScroll(e.currentTarget.scrollTop)}
        className="relative min-h-0 flex-1 overflow-auto"
      >
        {!result && (
          <p role="status" className="text-ink2 flex items-center gap-2 p-6">
            <span className="spinner" aria-hidden />
            {t('explorer.loading')}
          </p>
        )}
        {result?.status === 'failed' && (
          <div role="alert" className="p-6">
            <p className="text-ink2 mb-2">{result.message}</p>
            <button
              type="button"
              onClick={() => setAttempt((n) => n + 1)}
              className="text-hue min-h-11 font-semibold underline underline-offset-2"
            >
              {t('common.retry')}
            </button>
          </div>
        )}
        {empty && !result.anyEvents && unfiltered && !liveOn && <FirstEvent project={project} onEvent={reload} />}
        {empty && (result.anyEvents || !unfiltered || liveOn) && (
          <div className="p-6 text-center">
            <h2 className="mb-1 text-[17px] font-bold">
              {liveOn ? t('live.waitingTitle') : t('explorer.noMatchTitle')}
            </h2>
            <p className="text-ink2">{liveOn ? t('live.waitingText') : t('explorer.noMatchText')}</p>
          </div>
        )}
        {shown.length > 0 && (
          <table className="w-full table-fixed border-collapse text-[13px]">
            <caption className="sr-only">{t('explorer.caption')}</caption>
            <colgroup>
              <col className="w-[88px] md:w-[105px]" />
              <col className="w-5" />
              <col />
              <col className="w-0 md:w-[100px]" />
            </colgroup>
            <thead className="sr-only">
              <tr>
                <th>{t('explorer.col.time')}</th>
                <th>{t('explorer.col.level')}</th>
                <th>{t('explorer.col.message')}</th>
                <th>{t('explorer.col.stream')}</th>
              </tr>
            </thead>
            <tbody>
              {virtualStart > 0 && (
                <tr aria-hidden>
                  <td colSpan={4} style={{ height: virtualStart * size.row }} />
                </tr>
              )}
              {shown.slice(virtualStart, virtualEnd).map((item, offset) => {
                const index = virtualStart + offset;
                const type = catalog.data?.types.find((type) => type.name === item.event);
                const previous = shown[index - 1];
                const day = new Date(item.timestamp).toLocaleDateString(i18n.language);
                const changedDay = !previous || new Date(previous.timestamp).toLocaleDateString(i18n.language) !== day;
                const entities = ['actor', 'target']
                  .filter(
                    (key) => !['name', 'id'].some((field) => hiddenField(`${key}.${field}`, catalog.data?.rules ?? [])),
                  )
                  .map(
                    (key) =>
                      attributeValue(item.attributes, `${key}.name`) ?? attributeValue(item.attributes, `${key}.id`),
                  )
                  .filter((value): value is string | number => typeof value === 'string' || typeof value === 'number');
                return (
                  <tr
                    key={item.id}
                    className={cn(
                      'border-line hover:bg-sunk/60 cursor-pointer border-b',
                      selected?.id === item.id && 'bg-accent-soft',
                    )}
                    onClick={() => setSelected(item)}
                    style={{ height: size.row }}
                  >
                    <td
                      className="text-ink3 w-[88px] truncate pl-3 pr-2 text-xs md:w-[105px]"
                      title={new Date(item.timestamp).toLocaleString()}
                    >
                      <time dateTime={item.timestamp}>{relativeTime(item.timestamp, i18n.language, clock)}</time>
                      {changedDay && <span className="block text-[10px]">{day}</span>}
                    </td>
                    <td className="w-5">
                      <span
                        className={cn('font-bold', levelClass(item.level))}
                        aria-label={t(`levelNames.${item.level}`)}
                        title={t(`levelNames.${item.level}`)}
                      >
                        ●
                      </span>
                    </td>
                    <td className="min-w-0 pr-3">
                      <button
                        type="button"
                        className="block min-h-[41px] w-full text-left max-md:min-h-[53px]"
                        onClick={() => setSelected(item)}
                        aria-label={t('explorer.open', { time: dateTime(item.timestamp) })}
                      >
                        <span className="flex min-w-0 items-center gap-2">
                          <CatalogIcon name={type?.icon} color={type?.color} />
                          <strong className="truncate font-semibold">{type?.title || item.event}</strong>
                          {entities.slice(0, 2).map((entity, i) => (
                            <span
                              key={i}
                              className="bg-sunk text-ink2 hidden max-w-24 truncate rounded px-1.5 py-0.5 text-[11px] lg:inline"
                            >
                              {entity}
                            </span>
                          ))}
                          {item.tags?.slice(0, 2).map((tag) => (
                            <span key={tag} className="text-hue hidden text-[11px] xl:inline">
                              #{tag}
                            </span>
                          ))}
                        </span>
                        <span className="text-ink2 block truncate text-xs">
                          {eventMessage(item, type, catalog.data?.rules)}
                        </span>
                      </button>
                    </td>
                    <td className="text-ink3 w-0 truncate text-right text-xs md:w-[100px] md:pr-4">
                      {item.tier === 'archive' ? (
                        <span className="text-hue rounded border px-1 py-0.5">{t('explorer.tier.archive')}</span>
                      ) : (
                        <span className="hidden md:inline">#{item.stream}</span>
                      )}
                    </td>
                  </tr>
                );
              })}
              {shown.length > virtualEnd && (
                <tr aria-hidden>
                  <td colSpan={4} style={{ height: (shown.length - virtualEnd) * size.row }} />
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>

      {result?.status === 'ready' && shown.length > 0 && (
        <footer className="border-line flex flex-none items-center gap-3 border-t px-4 py-2.5">
          <p className="text-ink2 min-w-0 flex-1 truncate text-[13px] whitespace-nowrap">
            {total !== null
              ? t('explorer.shownOf', { shown: fmt(shown.length), total: fmt(total + freshLive.length) })
              : t('explorer.shown', { shown: fmt(shown.length) })}
          </p>
          {more.key === key && more.error && (
            <p role="alert" className="text-err text-[13px]">
              {t('explorer.moreFailed')}
            </p>
          )}
          {result.next && items.length < MAX_ROWS && (
            <button
              type="button"
              onClick={loadMore}
              disabled={more.key === key && more.busy}
              className="bg-accent text-accent-ink flex h-11 items-center gap-2 rounded-row px-4 font-bold whitespace-nowrap hover:brightness-110 disabled:opacity-60 md:h-9"
            >
              {more.key === key && more.busy && <span className="spinner" aria-hidden />}
              {t('explorer.more')}
            </button>
          )}
          {result.next && items.length >= MAX_ROWS && <p className="text-ink2 text-[13px]">{t('explorer.narrow')}</p>}
        </footer>
      )}

      {selected && (
        <EventDetail
          item={selected}
          type={catalog.data?.types.find((type) => type.name === selected.event)}
          rules={catalog.data?.rules ?? []}
          onClose={closeDetail}
          onFilter={(field, value) => {
            if (field === 'event') setEvent(value);
            else if (field === 'service') setService(value);
            else if (field === 'trace_id') setTrace(value);
            else if (field === 'level') setLevels([value]);
            else if (field === 'stream') {
              const stream = streams.find((s) => s.name === value);
              if (stream) navigate(projectPath(project.workspace_id, project.id, `streams/${stream.id}`));
            } else if (field === 'environment') setEnvironment(environments.find((e) => e.name === value)?.id ?? '');
            else {
              const term = field === 'tag' ? `tag:${value}` : `${field}=${JSON.stringify(value)}`;
              const next = [applied, term].filter(Boolean).join(' ');
              setInput(next);
              setApplied(next);
            }
            closeDetail();
          }}
        />
      )}
    </>
  );
}
