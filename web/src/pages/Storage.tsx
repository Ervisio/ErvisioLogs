import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Database } from 'lucide-react';
import type { ShellContext } from '../components/Shell';
import {
  api,
  ApiError,
  INSTANCE_SCOPE,
  type Project,
  type ProviderType,
  type RetentionPolicy,
  type StorageOverview,
  type StorageProvider,
  type StorageUsage,
} from '../lib/api';
import { useAuth, useSession } from '../lib/auth';
import { cn } from '../lib/cn';

type Loaded = { status: 'ready'; overview: StorageOverview; policies: RetentionPolicy[] } | { status: 'failed'; forbidden: boolean };

const GIB = 1024 ** 3;

export function formatBytes(n: number, locale: string) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: v < 10 && i > 0 ? 1 : 0 }).format(v)} ${units[i]}`;
}

const days = (hours: number | null | undefined) => (hours == null ? '' : String(Math.round((hours / 24) * 10) / 10));
const hours = (value: string) => (value.trim() === '' ? null : Math.round(Number(value) * 24));

/** Problem details from the server are written for people; anything else gets the fallback. */
function useProblem() {
  const { t } = useTranslation();
  return (err: unknown, fallback: string) =>
    err instanceof ApiError && err.status >= 400 && err.status < 500 && err.status !== 401 ? err.message : t(fallback);
}

/**
 * Retention and archive storage. Policies and destinations belong to the whole server, so only
 * the instance administrator sees this page; it opens on the current project's override.
 */
export default function Storage({ project }: { project: Project }) {
  const { t } = useTranslation();
  const { retry } = useAuth();
  const { user } = useSession();
  const { topbar } = useOutletContext<ShellContext>();
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; result: Loaded } | null>(null);
  const key = String(attempt);
  const admin = user.is_instance_admin;

  useEffect(() => {
    if (!admin) return;
    const ctrl = new AbortController();
    Promise.all([api.storage(ctrl.signal), api.retention(ctrl.signal)])
      .then(([overview, policies]) => setLoaded({ key, result: { status: 'ready', overview, policies } }))
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) retry();
        setLoaded({ key, result: { status: 'failed', forbidden: err instanceof ApiError && err.status === 403 } });
      });
    return () => ctrl.abort();
  }, [key, admin, retry]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  // A refresh after a save keeps the page on screen, so forms keep their confirmation message.
  const result = loaded && (loaded.key === key || loaded.result.status === 'ready') ? loaded.result : null;
  const title = topbar(<h1 className="truncate text-base font-bold">{t('nav.storage')}</h1>);

  if (!admin || result?.status === 'failed') {
    const forbidden = !admin || (result?.status === 'failed' && result.forbidden);
    return (
      <>
        {title}
        <div className="flex flex-1 items-center justify-center overflow-auto p-6">
          <div role={forbidden ? undefined : 'alert'} className="max-w-[400px] text-center">
            <Database className="text-ink3 mx-auto mb-3 size-7" aria-hidden />
            <h2 className="mb-1 text-[17px] font-bold">{forbidden ? t('storage.adminOnlyTitle') : t('storage.loadFailed')}</h2>
            {forbidden ? (
              <p className="text-ink2">{t('storage.adminOnly')}</p>
            ) : (
              <button type="button" onClick={reload} className="text-hue min-h-11 font-semibold underline underline-offset-2">
                {t('common.retry')}
              </button>
            )}
          </div>
        </div>
      </>
    );
  }

  const instance = result?.status === 'ready' ? result.policies.find((p) => p.scope_type === 'instance') : undefined;
  const override = result?.status === 'ready' ? result.policies.find((p) => p.scope_type === 'project' && p.scope_id === project.id) : undefined;

  return (
    <>
      {title}
      <div className="flex-1 overflow-auto p-4 md:p-6">
        <div className="mx-auto grid max-w-[820px] grid-cols-[minmax(0,1fr)] gap-10">
          {!result ? (
            <p role="status" className="text-ink2 flex items-center gap-2">
              <span className="spinner" aria-hidden />
              {t('storage.loading')}
            </p>
          ) : (
            result.status === 'ready' && (
              <>
                <Usage overview={result.overview} />
                <ProjectRetention project={project} instance={instance} override={override} onSaved={reload} />
                <Destinations providers={result.overview.providers ?? []} onChanged={reload} />
                <InstanceRetention instance={instance} onSaved={reload} />
              </>
            )
          )}
        </div>
      </div>
    </>
  );
}

function Section({ id, title, intro, children }: { id: string; title: string; intro?: ReactNode; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="grid gap-3">
      <h2 id={id} className="text-[17px] font-bold">
        {title}
      </h2>
      {intro && <p className="text-ink2 max-w-[64ch]">{intro}</p>}
      {children}
    </section>
  );
}

/** The one picture on the page: how close local disk is to the cap the archiver defends. */
function Usage({ overview }: { overview: StorageOverview }) {
  const { t, i18n } = useTranslation();
  const u: StorageUsage | undefined = overview.usage;
  const archived = overview.segments;
  const live = ['CATALOGED', 'HOT_EVICTABLE'].reduce((acc, s) => ({ count: acc.count + (archived[s]?.count ?? 0), bytes: acc.bytes + (archived[s]?.bytes ?? 0) }), {
    count: 0,
    bytes: 0,
  });
  const failed = archived.FAILED?.count ?? 0;
  const fmt = (n: number) => formatBytes(n, i18n.language);

  if (!overview.archiver_enabled) {
    return (
      <Section id="st-usage" title={t('storage.usage')}>
        <p className="border-line rounded-row border p-3">{t('storage.archiverOff')}</p>
      </Section>
    );
  }
  if (!u) {
    return (
      <Section id="st-usage" title={t('storage.usage')}>
        <p role="alert" className="text-ink2">
          {t('storage.usageUnavailable')}
        </p>
      </Section>
    );
  }
  const scale = Math.max(u.max_bytes, u.hot_bytes + u.cache_bytes);
  const pct = (n: number) => `${Math.min(100, (n / scale) * 100)}%`;
  const used = u.hot_bytes + u.cache_bytes;

  return (
    <Section id="st-usage" title={t('storage.usage')}>
      <div className="grid gap-2">
        <p className="text-[28px] leading-tight font-bold tabular-nums">
          {fmt(used)}
          <span className="text-ink2 ml-2 text-base font-normal">{t('storage.ofCap', { cap: fmt(u.max_bytes) })}</span>
        </p>
        <div
          role="img"
          aria-label={t('storage.meterLabel', { hot: fmt(u.hot_bytes), cache: fmt(u.cache_bytes), cap: fmt(u.max_bytes) })}
          className="bg-sunk relative h-4 overflow-hidden rounded-row"
        >
          <div className="bg-accent absolute inset-y-0 left-0" style={{ width: pct(u.hot_bytes) }} />
          <div className="bg-hue absolute inset-y-0 opacity-70" style={{ left: pct(u.hot_bytes), width: pct(u.cache_bytes) }} />
          {[u.low_pct, u.high_pct].map((w) => (
            <div key={w} aria-hidden className="bg-ink absolute inset-y-0 w-px opacity-60" style={{ left: pct((u.max_bytes * w) / 100) }} />
          ))}
        </div>
        <dl className="text-ink2 flex flex-wrap gap-x-6 gap-y-1 text-[13px]">
          <div className="flex items-center gap-1.5">
            <span aria-hidden className="bg-accent size-2.5 rounded-full" />
            <dt>{t('storage.hot')}</dt>
            <dd className="text-ink font-semibold tabular-nums">{fmt(u.hot_bytes)}</dd>
          </div>
          <div className="flex items-center gap-1.5">
            <span aria-hidden className="bg-hue size-2.5 rounded-full opacity-70" />
            <dt>{t('storage.cache')}</dt>
            <dd className="text-ink font-semibold tabular-nums">{fmt(u.cache_bytes)}</dd>
          </div>
          <div className="flex items-center gap-1.5">
            <dt>{t('storage.archived')}</dt>
            <dd className="text-ink font-semibold tabular-nums">{t('storage.segments', { count: live.count, size: fmt(live.bytes) })}</dd>
          </div>
        </dl>
        <p className="text-ink3 text-[13px]">{t('storage.watermarks', { high: u.high_pct, low: u.low_pct })}</p>
        {u.over_capacity && (
          <p role="alert" className="border-err text-err rounded-row border p-3 font-semibold">
            {t('storage.overCapacity')}
          </p>
        )}
        {failed > 0 && (
          <p role="alert" className="text-warn">
            {t('storage.failedSegments', { count: failed })}
          </p>
        )}
      </div>
    </Section>
  );
}

function DaysField(props: { id: string; label: string; value: string; placeholder?: string; onChange: (v: string) => void; hint?: string }) {
  return (
    <div className="grid gap-1.5">
      <label htmlFor={props.id} className="font-semibold">
        {props.label}
      </label>
      <input
        id={props.id}
        type="number"
        inputMode="decimal"
        min={0.05}
        step="any"
        value={props.value}
        placeholder={props.placeholder}
        aria-describedby={props.hint ? `${props.id}-h` : undefined}
        onChange={(e) => props.onChange(e.target.value)}
        className="bg-sunk border-field h-12 w-full rounded-row border px-3 text-base tabular-nums md:h-11 md:text-sm"
      />
      {props.hint && (
        <p id={`${props.id}-h`} className="text-ink3 text-[13px]">
          {props.hint}
        </p>
      )}
    </div>
  );
}

function SubmitRow({ busy, label, error, done, children }: { busy: boolean; label: string; error: string | null; done: string | null; children?: ReactNode }) {
  return (
    <>
      {error && (
        <p role="alert" className="text-err">
          {error}
        </p>
      )}
      {done && <p role="status">{done}</p>}
      <div className="flex flex-wrap gap-3">
        <button type="submit" disabled={busy} className="bg-accent text-accent-ink flex h-12 items-center justify-center gap-2 rounded-row px-5 font-bold hover:brightness-110 disabled:opacity-60 md:h-11">
          {busy && <span className="spinner" aria-hidden />}
          {label}
        </button>
        {children}
      </div>
    </>
  );
}

function ProjectRetention(props: { project: Project; instance?: RetentionPolicy; override?: RetentionPolicy; onSaved: () => void }) {
  const { t } = useTranslation();
  const problem = useProblem();
  const { project, instance, override, onSaved } = props;
  const [local, setLocal] = useState(days(override?.local_age_hours));
  const [global, setGlobal] = useState(days(override?.global_age_hours));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    const body = { local_age_hours: hours(local), global_age_hours: hours(global) };
    if (body.local_age_hours == null && body.global_age_hours == null) return setError(t('storage.overrideEmpty'));
    setBusy(true);
    setError(null);
    setDone(null);
    try {
      await api.putRetention('project', project.id, {
        ...(body.local_age_hours != null && { local_age_hours: body.local_age_hours }),
        ...(body.global_age_hours != null && { global_age_hours: body.global_age_hours }),
      });
      setDone(t('storage.saved'));
      onSaved();
    } catch (err) {
      setError(problem(err, 'storage.saveFailed'));
    } finally {
      setBusy(false);
    }
  };

  const reset = async () => {
    setBusy(true);
    setError(null);
    setDone(null);
    try {
      await api.deleteRetention('project', project.id);
      setLocal('');
      setGlobal('');
      setDone(t('storage.resetDone'));
      onSaved();
    } catch (err) {
      setError(problem(err, 'storage.saveFailed'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Section id="st-project" title={t('storage.projectTitle', { project: project.name })} intro={t('storage.projectIntro')}>
      <form noValidate onSubmit={(e) => void save(e)} className="grid max-w-[520px] gap-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <DaysField id="st-p-local" label={t('storage.localDays')} value={local} onChange={setLocal} placeholder={days(instance?.local_age_hours)} />
          <DaysField id="st-p-global" label={t('storage.globalDays')} value={global} onChange={setGlobal} placeholder={days(instance?.global_age_hours)} />
        </div>
        <p className="text-ink3 text-[13px]">{t('storage.inheritHint')}</p>
        <SubmitRow busy={busy} label={t('storage.save')} error={error} done={done}>
          {override && (
            <button type="button" disabled={busy} onClick={() => void reset()} className="hover:bg-sunk h-12 rounded-row px-4 font-semibold disabled:opacity-60 md:h-11">
              {t('storage.reset')}
            </button>
          )}
        </SubmitRow>
      </form>
    </Section>
  );
}

function InstanceRetention({ instance, onSaved }: { instance?: RetentionPolicy; onSaved: () => void }) {
  const { t } = useTranslation();
  const problem = useProblem();
  const [local, setLocal] = useState(days(instance?.local_age_hours));
  const [global, setGlobal] = useState(days(instance?.global_age_hours));
  const [cap, setCap] = useState(instance?.cache_max_bytes ? String(Math.round((instance.cache_max_bytes / GIB) * 10) / 10) : '');
  const [high, setHigh] = useState(String(instance?.high_pct ?? 90));
  const [low, setLow] = useState(String(instance?.low_pct ?? 75));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    const l = hours(local);
    const g = hours(global);
    const c = Number(cap);
    if (l == null || g == null || !(c > 0)) return setError(t('storage.instanceIncomplete'));
    if (g < l) return setError(t('storage.globalBeforeLocal'));
    setBusy(true);
    setError(null);
    setDone(null);
    try {
      await api.putRetention('instance', INSTANCE_SCOPE, {
        local_age_hours: l,
        global_age_hours: g,
        cache_max_bytes: Math.round(c * GIB),
        high_pct: Number(high),
        low_pct: Number(low),
      });
      setDone(t('storage.saved'));
      onSaved();
    } catch (err) {
      setError(problem(err, 'storage.saveFailed'));
    } finally {
      setBusy(false);
    }
  };

  const pctField = (id: string, label: string, value: string, set: (v: string) => void) => (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="font-semibold">
        {label}
      </label>
      <input
        id={id}
        type="number"
        min={1}
        max={100}
        value={value}
        onChange={(e) => set(e.target.value)}
        className="bg-sunk border-field h-12 w-full rounded-row border px-3 text-base tabular-nums md:h-11 md:text-sm"
      />
    </div>
  );

  return (
    <Section id="st-instance" title={t('storage.instanceTitle')} intro={t('storage.instanceIntro')}>
      <form noValidate onSubmit={(e) => void save(e)} className="grid max-w-[520px] gap-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <DaysField id="st-i-local" label={t('storage.localDays')} value={local} onChange={setLocal} hint={t('storage.localHint')} />
          <DaysField id="st-i-global" label={t('storage.globalDays')} value={global} onChange={setGlobal} hint={t('storage.globalHint')} />
        </div>
        <div className="grid gap-4 sm:grid-cols-3">
          <div className="grid gap-1.5">
            <label htmlFor="st-i-cap" className="font-semibold">
              {t('storage.capGib')}
            </label>
            <input
              id="st-i-cap"
              type="number"
              min={0.001}
              step="any"
              value={cap}
              onChange={(e) => setCap(e.target.value)}
              className="bg-sunk border-field h-12 w-full rounded-row border px-3 text-base tabular-nums md:h-11 md:text-sm"
            />
          </div>
          {pctField('st-i-high', t('storage.highPct'), high, setHigh)}
          {pctField('st-i-low', t('storage.lowPct'), low, setLow)}
        </div>
        <p className="text-ink3 text-[13px]">{t('storage.capHint')}</p>
        <SubmitRow busy={busy} label={t('storage.save')} error={error} done={done} />
      </form>
    </Section>
  );
}

const STATUS_DOT: Record<StorageProvider['status'], string> = { ok: 'bg-ok', error: 'bg-err', unknown: 'bg-ink3' };

function Destinations({ providers, onChanged }: { providers: StorageProvider[]; onChanged: () => void }) {
  const { t, i18n } = useTranslation();
  const problem = useProblem();
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const active = providers.find((p) => p.active);

  const run = async (id: string, fn: () => Promise<unknown>) => {
    setBusy(id);
    setError(null);
    try {
      await fn();
      onChanged();
    } catch (err) {
      setError(problem(err, 'storage.actionFailed'));
    } finally {
      setBusy(null);
    }
  };

  const when = (iso: string | null) =>
    iso ? new Date(iso).toLocaleString(i18n.language, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '';

  return (
    <Section id="st-dest" title={t('storage.destinations')} intro={active ? t('storage.activeIntro', { name: active.name }) : t('storage.noActive')}>
      {providers.length > 0 && (
        <ul className="border-line divide-line grid divide-y rounded-row border">
          {providers.map((p) => (
            <li key={p.id} className="grid gap-2 p-3">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span aria-hidden className={cn('size-2.5 flex-none rounded-full', STATUS_DOT[p.status])} />
                <span className="min-w-0 truncate font-semibold">{p.name}</span>
                <span className="bg-sunk rounded-row px-2 py-0.5 text-[12px] font-semibold uppercase">{p.type}</span>
                {p.active && <span className="text-ok text-[13px] font-semibold">{t('storage.receiving')}</span>}
                <span className="text-ink3 ml-auto text-[12px]">
                  {t(`storage.status.${p.status}`)}
                  {p.last_checked_at && ` ${when(p.last_checked_at)}`}
                </span>
              </div>
              <p className="text-ink2 truncate text-[13px]">{describe(p)}</p>
              {p.last_error && <p className="text-err text-[13px] break-words">{p.last_error}</p>}
              <div className="flex flex-wrap gap-2">
                <RowButton disabled={busy === p.id} onClick={() => void run(p.id, () => api.testProvider(p.id))}>
                  {t('storage.test')}
                </RowButton>
                {!p.active && (
                  <RowButton disabled={busy === p.id} onClick={() => void run(p.id, () => api.setActiveProvider(p.id))}>
                    {t('storage.makeActive')}
                  </RowButton>
                )}
                {p.active && (
                  <RowButton
                    disabled={busy === p.id}
                    onClick={() => window.confirm(t('storage.stopConfirm')) && void run(p.id, () => api.setActiveProvider(null))}
                  >
                    {t('storage.stop')}
                  </RowButton>
                )}
                {!p.active && (
                  <RowButton
                    danger
                    disabled={busy === p.id}
                    onClick={() => window.confirm(t('storage.deleteConfirm', { name: p.name })) && void run(p.id, () => api.deleteProvider(p.id))}
                  >
                    {t('storage.delete')}
                  </RowButton>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {error && (
        <p role="alert" className="text-err">
          {error}
        </p>
      )}
      <AddDestination onAdded={onChanged} />
    </Section>
  );
}

function describe(p: StorageProvider) {
  const c = p.config as Record<string, string | number | undefined>;
  if (p.type === 'local') return String(c.root ?? '');
  if (p.type === 's3') return [c.endpoint, c.bucket && `${c.bucket}/${c.prefix ?? ''}`].filter(Boolean).join(' ');
  return `${c.user ?? ''}@${c.host ?? ''}${c.port ? `:${c.port}` : ''} ${c.base_dir ?? ''}`.trim();
}

function RowButton({ danger, ...rest }: { danger?: boolean; disabled?: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      {...rest}
      className={cn('hover:bg-sunk border-line h-11 rounded-row border px-3 text-[13px] font-semibold disabled:opacity-60 md:h-9', danger && 'text-err')}
    />
  );
}

type FieldDef = { key: string; label: string; placeholder?: string; number?: boolean; check?: boolean; wide?: boolean; options?: { value: string; label: string }[] };

function AddDestination({ onAdded }: { onAdded: () => void }) {
  const { t } = useTranslation();
  const problem = useProblem();
  const [type, setType] = useState<ProviderType>('s3');
  const [name, setName] = useState('');
  const [values, setValues] = useState<Record<string, string | boolean>>({});
  const [secret, setSecret] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fields: Record<ProviderType, FieldDef[]> = {
    s3: [
      { key: 'endpoint', label: t('storage.f.endpoint'), placeholder: 'https://s3.eu-central-1.amazonaws.com', wide: true },
      { key: 'region', label: t('storage.f.region'), placeholder: 'eu-central-1' },
      { key: 'bucket', label: t('storage.f.bucket') },
      { key: 'prefix', label: t('storage.f.prefix'), placeholder: 'ervisio-logs/' },
      { key: 'path_style', label: t('storage.f.pathStyle'), check: true },
    ],
    sftp: [
      { key: 'host', label: t('storage.f.host') },
      { key: 'port', label: t('storage.f.port'), placeholder: '22', number: true },
      { key: 'user', label: t('storage.f.user') },
      {
        key: 'auth',
        label: t('storage.f.auth'),
        options: [
          { value: 'key', label: t('storage.auth.key') },
          { value: 'password', label: t('storage.auth.password') },
        ],
      },
      { key: 'base_dir', label: t('storage.f.baseDir'), placeholder: 'archive' },
      { key: 'host_key', label: t('storage.f.hostKey'), placeholder: 'ssh-ed25519 AAAA…', wide: true },
    ],
    local: [{ key: 'root', label: t('storage.f.root'), placeholder: '/var/lib/ervisio-logs/archive', wide: true }],
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return setError(t('storage.nameRequired'));
    const config: Record<string, unknown> = {};
    for (const f of fields[type]) {
      const v = values[f.key];
      if (f.check) {
        if (v) config[f.key] = true;
      } else if (f.options) {
        if (typeof v === 'string' && v !== f.options[0].value) config[f.key] = v;
      } else if (typeof v === 'string' && v.trim() !== '') config[f.key] = f.number ? Number(v) : v.trim();
    }
    setBusy(true);
    setError(null);
    try {
      const p = await api.createProvider({ name: name.trim(), type, config, secret_file: type === 'local' || !secret.trim() ? null : secret.trim() });
      setName('');
      setValues({});
      setSecret('');
      if (p.status === 'error') setError(t('storage.addedUnhealthy', { name: p.name }));
      onAdded();
    } catch (err) {
      setError(problem(err, 'storage.addFailed'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form noValidate onSubmit={(e) => void submit(e)} className="border-line mt-2 grid gap-4 rounded-row border p-4">
      <h3 className="font-bold">{t('storage.add')}</h3>
      <fieldset className="flex flex-wrap gap-2">
        <legend className="sr-only">{t('storage.kind')}</legend>
        {(['s3', 'sftp', 'local'] as const).map((k) => (
          <label
            key={k}
            className={cn(
              'flex min-h-11 cursor-pointer items-center gap-2 rounded-row border px-3 has-[:focus-visible]:outline-2 md:min-h-9',
              type === k ? 'border-accent font-semibold' : 'border-line',
            )}
          >
            <input type="radio" name="st-kind" value={k} checked={type === k} onChange={() => setType(k)} className="sr-only" />
            {t(`storage.kinds.${k}`)}
          </label>
        ))}
      </fieldset>
      <div className="grid gap-4 sm:grid-cols-2">
        <TextField id="st-a-name" label={t('storage.f.name')} value={name} onChange={setName} placeholder={t('storage.namePlaceholder')} wide />
        {fields[type].map((f) =>
          f.check ? (
            <label key={`${type}-${f.key}`} className="flex min-h-11 items-center gap-2 sm:col-span-2">
              <input type="checkbox" checked={!!values[f.key]} onChange={(e) => setValues({ ...values, [f.key]: e.target.checked })} className="size-4" />
              {f.label}
            </label>
          ) : f.options ? (
            <div key={`${type}-${f.key}`} className="grid gap-1.5">
              <label htmlFor={`st-a-${f.key}`} className="font-semibold">
                {f.label}
              </label>
              <select
                id={`st-a-${f.key}`}
                value={String(values[f.key] ?? f.options[0].value)}
                onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
                className="bg-sunk border-field h-12 w-full rounded-row border px-3 text-base md:h-11 md:text-sm"
              >
                {f.options.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </div>
          ) : (
            <TextField
              key={`${type}-${f.key}`}
              id={`st-a-${f.key}`}
              label={f.label}
              value={String(values[f.key] ?? '')}
              onChange={(v) => setValues({ ...values, [f.key]: v })}
              placeholder={f.placeholder}
              wide={f.wide}
              numeric={f.number}
            />
          ),
        )}
        {type !== 'local' && (
          <TextField id="st-a-secret" label={t('storage.f.secret')} value={secret} onChange={setSecret} placeholder={type === 's3' ? 's3-archive.json' : values.auth === 'password' ? 'storagebox-password' : 'storagebox-key'} hint={t(type === 'sftp' && values.auth === 'password' ? 'storage.secretHint.sftpPassword' : `storage.secretHint.${type}`)} wide />
        )}
      </div>
      <SubmitRow busy={busy} label={t('storage.addButton')} error={error} done={null} />
    </form>
  );
}

function TextField(props: { id: string; label: string; value: string; onChange: (v: string) => void; placeholder?: string; hint?: string; wide?: boolean; numeric?: boolean }) {
  return (
    <div className={cn('grid gap-1.5', props.wide && 'sm:col-span-2')}>
      <label htmlFor={props.id} className="font-semibold">
        {props.label}
      </label>
      <input
        id={props.id}
        type={props.numeric ? 'number' : 'text'}
        autoComplete="off"
        spellCheck={false}
        value={props.value}
        placeholder={props.placeholder}
        aria-describedby={props.hint ? `${props.id}-h` : undefined}
        onChange={(e) => props.onChange(e.target.value)}
        className="bg-sunk border-field h-12 w-full rounded-row border px-3 text-base md:h-11 md:text-sm"
      />
      {props.hint && (
        <p id={`${props.id}-h`} className="text-ink3 text-[13px]">
          {props.hint}
        </p>
      )}
    </div>
  );
}
