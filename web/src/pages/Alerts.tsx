import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { Link, useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Bell, BellOff } from 'lucide-react';
import type { ShellContext } from '../components/Shell';
import { api, ApiError, type AlertEvent, type AlertRule, type AlertRuleInput, type NotificationChannel, type Project, type Workspace } from '../lib/api';
import { useAuth } from '../lib/auth';
import { cn } from '../lib/cn';
import { formatQuery, parseQuery } from '../lib/query';
import { sectionPath } from '../lib/sections';

const LEVELS = ['FATAL', 'ERROR', 'WARN', 'NOTICE', 'INFO', 'DEBUG', 'TRACE'];
const WINDOWS = [300, 900, 3600, 21600, 86400];
const INTERVALS = [60, 300, 900, 3600];
/** Roles holding alerts:manage. */
const MANAGERS = new Set(['owner', 'admin', 'developer']);

type Loaded = { status: 'ready'; rules: AlertRule[]; channels: NotificationChannel[] } | { status: 'failed'; forbidden: boolean; unavailable: boolean };

const INPUT = 'bg-sunk border-field placeholder:text-ink3 h-12 rounded-row border px-3 text-base md:h-11 md:text-sm';
const LINK = 'text-hue min-h-11 font-semibold underline underline-offset-2';

export default function Alerts({ project, workspace }: { project: Project; workspace: Workspace }) {
  const { t, i18n } = useTranslation();
  const { retry } = useAuth();
  const { topbar } = useOutletContext<ShellContext>();
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; result: Loaded } | null>(null);
  const [editing, setEditing] = useState<AlertRule | 'new' | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const canManage = MANAGERS.has(workspace.role);
  const key = `${project.id}:${attempt}`;

  useEffect(() => {
    const ctrl = new AbortController();
    Promise.all([api.alertRules(project.id, ctrl.signal), api.channels(workspace.id, ctrl.signal)])
      .then(([rules, channels]) => setLoaded({ key, result: { status: 'ready', rules, channels } }))
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) retry();
        const status = err instanceof ApiError ? err.status : 0;
        setLoaded({ key, result: { status: 'failed', forbidden: status === 403, unavailable: status === 503 } });
      });
    return () => ctrl.abort();
  }, [key, project.id, workspace.id, retry]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const result = loaded?.key === key ? loaded.result : null;
  const rules = result?.status === 'ready' ? result.rules : [];
  const channels = result?.status === 'ready' ? result.channels : [];
  const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }) : '');

  const act = async (rule: AlertRule, run: () => Promise<unknown>, failed: string) => {
    setBusy(rule.id);
    setActionError(null);
    try {
      await run();
      reload();
    } catch (err) {
      setActionError(err instanceof ApiError && err.status < 500 ? err.message : t(failed));
    } finally {
      setBusy(null);
    }
  };
  const toggle = (rule: AlertRule) =>
    act(rule, () => api.updateAlertRule(rule.id, { ...inputOf(rule), enabled: !rule.enabled }), 'alerts.saveFailed');
  const remove = (rule: AlertRule) => {
    if (window.confirm(t('alerts.deleteConfirm', { name: rule.name }))) void act(rule, () => api.deleteAlertRule(rule.id), 'alerts.deleteFailed');
  };

  return (
    <>
      {topbar(<h1 className="truncate text-base font-bold">{t('nav.alerts')}</h1>)}
      <div className="flex-1 overflow-auto p-4 md:p-6">
        <div className="mx-auto grid max-w-[820px] grid-cols-[minmax(0,1fr)] gap-8">
          <p className="text-ink2 max-w-[60ch]">{t('alerts.intro')}</p>

          <section aria-labelledby="rules" className="grid gap-3">
            <div className="flex items-center gap-3">
              <h2 id="rules" className="flex-1 text-[17px] font-bold">
                {t('alerts.rules')}
              </h2>
              {canManage && result?.status === 'ready' && editing === null && (
                <button type="button" onClick={() => setEditing('new')} className="bg-accent text-accent-ink h-11 rounded-row px-4 font-bold hover:brightness-110 md:h-9">
                  {t('alerts.new')}
                </button>
              )}
            </div>
            {!result && (
              <p role="status" className="text-ink2 flex items-center gap-2">
                <span className="spinner" aria-hidden />
                {t('alerts.loading')}
              </p>
            )}
            {result?.status === 'failed' && (
              <div role="alert">
                <p className="text-ink2 mb-2">{result.forbidden ? t('alerts.forbidden') : result.unavailable ? t('alerts.unavailable') : t('alerts.loadFailed')}</p>
                {!result.forbidden && (
                  <button type="button" onClick={reload} className={LINK}>
                    {t('common.retry')}
                  </button>
                )}
              </div>
            )}
            {editing !== null && (
              <RuleForm
                project={project}
                channels={channels}
                rule={editing === 'new' ? null : editing}
                onDone={(saved) => {
                  setEditing(null);
                  if (saved) reload();
                }}
              />
            )}
            {result?.status === 'ready' && rules.length === 0 && editing === null && <p className="text-ink2">{t('alerts.none')}</p>}
            {rules.length > 0 && (
              <ul className="border-line divide-line grid divide-y rounded-row border">
                {rules.map((rule) => (
                  <li key={rule.id} className="grid gap-2 p-3">
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                      {rule.enabled ? <Bell className="text-ink3 size-4 flex-none" aria-hidden /> : <BellOff className="text-ink3 size-4 flex-none" aria-hidden />}
                      <div className="min-w-0 flex-1">
                        <p className="flex flex-wrap items-center gap-2">
                          <span className="truncate font-semibold">{rule.name}</span>
                          <StateBadge rule={rule} />
                        </p>
                        <p className="text-ink2 text-[13px]">
                          {t('alerts.summary', { threshold: rule.threshold, window: t(`alerts.windows.${rule.window_seconds}`, { defaultValue: `${rule.window_seconds}s` }) })}
                          {describeFilter(rule) && <code className="ml-2 font-mono">{describeFilter(rule)}</code>}
                        </p>
                        <p className="text-ink3 text-[12px]">
                          {rule.last_evaluated_at ? t('alerts.checked', { date: when(rule.last_evaluated_at), value: rule.last_value ?? 0 }) : t('alerts.notChecked')}
                          {' · '}
                          {t('alerts.channelsCount', { count: rule.channel_ids.length })}
                        </p>
                        {rule.state === 'error' && rule.last_error && <p className="text-err text-[13px]">{rule.last_error}</p>}
                      </div>
                      {canManage && (
                        <div className="flex flex-none items-center gap-1">
                          <button type="button" onClick={() => setEditing(rule)} disabled={editing !== null} className="hover:bg-sunk h-11 rounded-row px-3 font-semibold disabled:opacity-60 md:h-9">
                            {t('alerts.edit')}
                          </button>
                          <button type="button" onClick={() => void toggle(rule)} disabled={busy === rule.id} className="hover:bg-sunk h-11 rounded-row px-3 font-semibold disabled:opacity-60 md:h-9">
                            {rule.enabled ? t('alerts.pause') : t('alerts.resume')}
                          </button>
                          <button
                            type="button"
                            onClick={() => remove(rule)}
                            disabled={busy === rule.id}
                            aria-label={`${t('alerts.delete')} ${rule.name}`}
                            className="text-err hover:bg-sunk h-11 rounded-row px-3 font-semibold disabled:opacity-60 md:h-9"
                          >
                            {t('alerts.delete')}
                          </button>
                        </div>
                      )}
                    </div>
                    <History rule={rule} channels={channels} when={when} />
                  </li>
                ))}
              </ul>
            )}
            {actionError && (
              <p role="alert" className="text-err">
                {actionError}
              </p>
            )}
            {result?.status === 'ready' && channels.length === 0 && (
              <p className="text-ink2 text-[13px]">
                {t('alerts.noChannels')}{' '}
                <Link to={sectionPath(project.id, 'integrations')} className="text-hue font-semibold underline underline-offset-2">
                  {t('alerts.addChannel')}
                </Link>
              </p>
            )}
          </section>
        </div>
      </div>
    </>
  );
}

function inputOf(rule: AlertRule): AlertRuleInput {
  return {
    project_id: rule.project_id,
    name: rule.name,
    query: rule.query,
    window_seconds: rule.window_seconds,
    threshold: rule.threshold,
    interval_seconds: rule.interval_seconds,
    channel_ids: rule.channel_ids,
    enabled: rule.enabled,
  };
}

function describeFilter(rule: AlertRule): string {
  const q = rule.query;
  const parts = [formatQuery(q.query, q.filters)];
  if (q.levels?.length) parts.push(`level:${q.levels.join(',').toLowerCase()}`);
  if (q.streams?.length) parts.push(`#${q.streams.join(',#')}`);
  return parts.filter(Boolean).join(' ');
}

function StateBadge({ rule }: { rule: AlertRule }) {
  const { t } = useTranslation();
  if (!rule.enabled) return <span className="bg-raised text-ink2 rounded px-1.5 py-0.5 text-[12px] font-semibold">{t('alerts.state.paused')}</span>;
  const cls = rule.state === 'firing' ? 'bg-err text-bg' : rule.state === 'error' ? 'text-warn border-warn border' : 'bg-raised text-ink';
  return <span className={cn('rounded px-1.5 py-0.5 text-[12px] font-semibold', cls)}>{t(`alerts.state.${rule.state}`)}</span>;
}

function History({ rule, channels, when }: { rule: AlertRule; channels: NotificationChannel[]; when: (iso: string) => string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [events, setEvents] = useState<AlertEvent[] | 'loading' | 'failed'>('loading');

  useEffect(() => {
    if (!open) return;
    const ctrl = new AbortController();
    api
      .alertEvents(rule.id, ctrl.signal)
      .then(setEvents)
      .catch(() => !ctrl.signal.aborted && setEvents('failed'));
    return () => ctrl.abort();
  }, [open, rule.id, rule.updated_at, rule.last_evaluated_at]);

  const channelName = (id: string) => channels.find((c) => c.id === id)?.name ?? t('alerts.deletedChannel');
  return (
    <details onToggle={(e) => setOpen(e.currentTarget.open)} className="text-[13px]">
      <summary className="text-ink2 min-h-11 cursor-pointer content-center md:min-h-8">{t('alerts.history')}</summary>
      {events === 'loading' && <p className="text-ink2 py-1">{t('alerts.loading')}</p>}
      {events === 'failed' && <p className="text-err py-1">{t('alerts.historyFailed')}</p>}
      {Array.isArray(events) && events.length === 0 && <p className="text-ink2 py-1">{t('alerts.historyNone')}</p>}
      {Array.isArray(events) && events.length > 0 && (
        <ol className="grid gap-1.5 py-1 pl-4">
          {events.map((ev) => (
            <li key={ev.id}>
              <span className={cn('font-semibold', ev.status === 'firing' ? 'text-err' : ev.status === 'error' ? 'text-warn' : 'text-ink')}>{t(`alerts.event.${ev.status}`)}</span>
              <span className="text-ink2 ml-2">{when(ev.created_at)}</span>
              {ev.value !== null && <span className="text-ink2 ml-2">{t('alerts.eventValue', { value: ev.value })}</span>}
              {ev.detail && <span className="text-ink2 ml-2">{ev.detail}</span>}
              {ev.deliveries.length > 0 && (
                <span className="text-ink3 ml-2">
                  {ev.deliveries.map((d) => `${channelName(d.channel_id)}: ${d.ok ? t('alerts.sent') : d.error || t('alerts.notSent')}`).join('; ')}
                </span>
              )}
            </li>
          ))}
        </ol>
      )}
    </details>
  );
}

function RuleForm({ project, channels, rule, onDone }: { project: Project; channels: NotificationChannel[]; rule: AlertRule | null; onDone: (saved: boolean) => void }) {
  const { t } = useTranslation();
  const { streams } = useOutletContext<ShellContext>();
  const [name, setName] = useState(rule?.name ?? '');
  const [search, setSearch] = useState(rule ? formatQuery(rule.query.query, rule.query.filters) : '');
  const [levels, setLevels] = useState<string[]>(rule?.query.levels ?? ['ERROR', 'FATAL']);
  const [stream, setStream] = useState(rule?.query.streams?.[0] ?? '');
  const [windowSec, setWindowSec] = useState(rule?.window_seconds ?? 300);
  const [threshold, setThreshold] = useState(String(rule?.threshold ?? 0));
  const [intervalSec, setIntervalSec] = useState(rule?.interval_seconds ?? 60);
  const [picked, setPicked] = useState<string[]>(rule?.channel_ids ?? channels.filter((c) => c.enabled).map((c) => c.id));
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const value = name.trim();
    const n = Number(threshold);
    if (!value) return setError(t('alerts.nameRequired'));
    if (!Number.isInteger(n) || n < 0) return setError(t('alerts.thresholdInvalid'));
    const parsed = parseQuery(search.trim());
    // The form picks one stream; a rule saved with several keeps them until another stream is chosen.
    const saved = rule?.query.streams ?? [];
    const keptStreams = stream === (saved[0] ?? '') ? (saved.length ? saved : undefined) : stream ? [stream] : undefined;
    const body: AlertRuleInput = {
      project_id: project.id,
      name: value,
      query: {
        ...rule?.query,
        query: parsed.text || undefined,
        filters: parsed.filters.length ? parsed.filters : undefined,
        levels: levels.length ? levels : undefined,
        streams: keptStreams,
      },
      window_seconds: windowSec,
      threshold: n,
      interval_seconds: intervalSec,
      channel_ids: picked,
      enabled: rule?.enabled ?? true,
    };
    setBusy(true);
    setError(null);
    try {
      if (rule) await api.updateAlertRule(rule.id, body);
      else await api.createAlertRule(body);
      onDone(true);
    } catch (err) {
      setError(err instanceof ApiError && err.status < 500 ? err.message : t('alerts.saveFailed'));
    } finally {
      setBusy(false);
    }
  };

  const toggle = (list: string[], v: string) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  return (
    <form noValidate onSubmit={(e) => void submit(e)} aria-labelledby="rule-form" className="border-line grid gap-4 rounded-row border p-4">
      <h3 id="rule-form" className="font-bold">
        {rule ? t('alerts.editTitle', { name: rule.name }) : t('alerts.newTitle')}
      </h3>
      <div className="grid gap-1.5">
        <label htmlFor="rule-name" className="font-semibold">
          {t('alerts.name')}
        </label>
        <input id="rule-name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} placeholder={t('alerts.namePlaceholder')} className={INPUT} />
      </div>
      <div className="grid gap-1.5">
        <label htmlFor="rule-q" className="font-semibold">
          {t('alerts.match')}
        </label>
        <input id="rule-q" value={search} onChange={(e) => setSearch(e.target.value)} placeholder={t('explorer.searchPlaceholder')} aria-describedby="rule-q-hint" className={cn(INPUT, 'font-mono')} />
        <p id="rule-q-hint" className="text-ink3 text-[12px]">
          {t('alerts.matchHint')}
        </p>
      </div>
      <fieldset className="grid gap-1">
        <legend className="mb-1.5 font-semibold">{t('explorer.levels')}</legend>
        <div className="flex flex-wrap gap-x-4">
          {LEVELS.map((l) => (
            <label key={l} className="flex min-h-11 items-center gap-2 md:min-h-9">
              <input type="checkbox" checked={levels.includes(l)} onChange={() => setLevels((cur) => toggle(cur, l))} className="accent-accent size-4" />
              <span className="font-mono text-[13px]">{l.toLowerCase()}</span>
            </label>
          ))}
        </div>
      </fieldset>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="grid gap-1.5">
          <label htmlFor="rule-stream" className="font-semibold">
            {t('alerts.stream')}
          </label>
          <select id="rule-stream" value={stream} onChange={(e) => setStream(e.target.value)} className={INPUT}>
            <option value="">{t('explorer.allStreams')}</option>
            {streams.map((s) => (
              <option key={s.id} value={s.name}>
                # {s.name}
              </option>
            ))}
          </select>
        </div>
        <div className="grid gap-1.5">
          <label htmlFor="rule-window" className="font-semibold">
            {t('alerts.window')}
          </label>
          <select id="rule-window" value={windowSec} onChange={(e) => setWindowSec(Number(e.target.value))} className={INPUT}>
            {WINDOWS.map((w) => (
              <option key={w} value={w}>
                {t(`alerts.windows.${w}`)}
              </option>
            ))}
          </select>
        </div>
        <div className="grid gap-1.5">
          <label htmlFor="rule-threshold" className="font-semibold">
            {t('alerts.threshold')}
          </label>
          <input id="rule-threshold" type="number" min={0} step={1} inputMode="numeric" value={threshold} onChange={(e) => setThreshold(e.target.value)} aria-describedby="rule-threshold-hint" className={INPUT} />
          <p id="rule-threshold-hint" className="text-ink3 text-[12px]">
            {t('alerts.thresholdHint', { count: Number(threshold) || 0 })}
          </p>
        </div>
        <div className="grid gap-1.5">
          <label htmlFor="rule-interval" className="font-semibold">
            {t('alerts.interval')}
          </label>
          <select id="rule-interval" value={intervalSec} onChange={(e) => setIntervalSec(Number(e.target.value))} className={INPUT}>
            {INTERVALS.map((w) => (
              <option key={w} value={w}>
                {t(`alerts.intervals.${w}`)}
              </option>
            ))}
          </select>
        </div>
      </div>
      <fieldset className="grid gap-1">
        <legend className="mb-1.5 font-semibold">{t('alerts.notify')}</legend>
        {channels.length === 0 && <p className="text-ink2 text-[13px]">{t('alerts.noChannels')}</p>}
        {channels.map((c) => (
          <label key={c.id} className="flex min-h-11 items-center gap-3 md:min-h-9">
            <input type="checkbox" checked={picked.includes(c.id)} onChange={() => setPicked((cur) => toggle(cur, c.id))} className="accent-accent size-4" />
            <span>
              {c.name}
              <span className="text-ink2 ml-2 text-[13px]">{t(`channels.format.${c.format}`)}</span>
              {!c.enabled && <span className="text-ink3 ml-2 text-[13px]">{t('channels.disabled')}</span>}
            </span>
          </label>
        ))}
      </fieldset>
      {error && (
        <p role="alert" className="text-err">
          {error}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <button type="submit" disabled={busy} className="bg-accent text-accent-ink flex h-12 items-center justify-center gap-2 rounded-row px-5 font-bold hover:brightness-110 disabled:opacity-60 md:h-11">
          {busy && <span className="spinner" aria-hidden />}
          {rule ? t('alerts.save') : t('alerts.create')}
        </button>
        <button type="button" onClick={() => onDone(false)} className="hover:bg-sunk h-12 rounded-row px-4 font-semibold md:h-11">
          {t('alerts.cancel')}
        </button>
      </div>
    </form>
  );
}
