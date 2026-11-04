import { useEffect, useState } from 'react';
import { useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { ShellContext } from '../components/Shell';
import { ApiError } from '../lib/api';
import { adminHealth, type AdminHealth } from '../lib/adminHealth';
import { useAuth, useSession } from '../lib/auth';
import { cn } from '../lib/cn';

const POLL_MS = 30_000;

export default function AdminOverview() {
  const { t, i18n } = useTranslation();
  const { user } = useSession();
  const { retry } = useAuth();
  const { topbar } = useOutletContext<ShellContext>();
  const [attempt, setAttempt] = useState(0);
  const [snapshot, setSnapshot] = useState<{ user: string; health: AdminHealth; at: Date } | null>(null);
  const [error, setError] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!user.is_instance_admin) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let ctrl: AbortController;
    let live = true;
    const load = async () => {
      ctrl = new AbortController();
      setLoading(true);
      try {
        const health = await adminHealth.get(ctrl.signal);
        if (!live || ctrl.signal.aborted) return;
        setSnapshot({ user: user.id, health, at: new Date() });
        setError(false);
      } catch (err) {
        if (!live || ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) { retry(); return; }
        setError(true);
      } finally {
        if (live && !ctrl.signal.aborted) {
          setLoading(false);
          timer = setTimeout(() => void load(), POLL_MS);
        }
      }
    };
    void load();
    return () => { live = false; ctrl?.abort(); clearTimeout(timer); };
  }, [user.id, user.is_instance_admin, attempt, retry]);

  const data = snapshot?.user === user.id ? snapshot : null;
  const number = (value: number | null | undefined) => value == null ? t('instanceAdmin.unavailable') : new Intl.NumberFormat(i18n.language).format(value);
  const stamp = (value: string | null) => value ? new Date(value).toLocaleString(i18n.language) : t('instanceAdmin.unavailable');
  const health = data?.health;
  return <>
    {topbar(<h1 className="truncate text-base font-bold">{t('instanceAdmin.health.title')}</h1>)}
    {!user.is_instance_admin ? <p role="alert" className="p-6">{t('instanceAdmin.forbidden')}</p> : <div className="flex-1 overflow-auto p-4 md:p-7">
      <div className="mx-auto grid max-w-[960px] gap-6">
        <div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="text-xl font-bold">{t('instanceAdmin.health.title')}</h2><p className="text-ink2 mt-1 text-[13px]">{t('instanceAdmin.health.intro')}</p></div><button type="button" disabled={loading} onClick={() => setAttempt((value) => value + 1)} className="border-line min-h-11 rounded-row border px-3 text-xs font-semibold disabled:opacity-60 md:min-h-9">{t('instanceAdmin.refresh')}</button></div>
        {loading && !data && <p role="status" className="text-ink2 flex items-center gap-2"><span className="spinner" aria-hidden />{t('instanceAdmin.loading')}</p>}
        {error && <p role="alert" className="text-err">{t(data ? 'instanceAdmin.health.stale' : 'instanceAdmin.health.loadFailed')}</p>}
        {health && <>
          <div role="status" className="border-line flex flex-wrap items-center justify-between gap-3 border-y py-3"><span className={cn('font-semibold', health.status === 'ok' ? 'text-ink' : 'text-hue')}>{t(`instanceAdmin.health.status.${health.status === 'ok' ? 'ok' : health.status === 'degraded' ? 'degraded' : 'unknown'}`)}</span><span className="text-ink2 text-xs">{t('instanceAdmin.health.updated', { at: data.at.toLocaleString(i18n.language) })}</span></div>
          <section aria-labelledby="health-databases" className="grid gap-3"><h3 id="health-databases" className="text-base font-bold">{t('instanceAdmin.health.databases')}</h3><div className="grid gap-3 md:grid-cols-2">{(['postgres', 'clickhouse'] as const).map((key) => <div key={key} className="bg-surface border-line rounded-row border p-4"><h4 className="font-semibold">{key === 'postgres' ? 'PostgreSQL' : 'ClickHouse'}</h4><p className="text-ink2 mt-1 text-[13px]">{t(health[key].ok ? 'instanceAdmin.health.checkOk' : 'instanceAdmin.health.checkFailed')}</p>{health[key].error && <p className="text-ink2 mt-2 text-xs">{t('instanceAdmin.health.errorReported')}</p>}</div>)}</div></section>
          <section aria-labelledby="health-wal" className="grid gap-3"><h3 id="health-wal" className="text-base font-bold">{t('instanceAdmin.health.wal')}</h3>{!health.wal && <p className="text-ink2 text-[13px]">{t('instanceAdmin.health.walUnavailable')}</p>}<div className="grid grid-cols-2 gap-3 md:grid-cols-3"><Metric label={t('instanceAdmin.health.walBytes')} value={number(health.wal?.bytes)} unit="B" /><Metric label={t('instanceAdmin.health.walMax')} value={number(health.wal?.max_bytes)} unit="B" /><Metric label={t('instanceAdmin.health.segments')} value={number(health.wal?.segments)} /><Metric label={t('instanceAdmin.health.pending')} value={number(health.wal?.pending_records)} /><Metric label={t('instanceAdmin.health.lag')} value={number(health.wal?.lag_seconds)} unit={t('instanceAdmin.health.seconds')} /></div></section>
          <section aria-labelledby="health-archive" className="grid gap-3"><h3 id="health-archive" className="text-base font-bold">{t('instanceAdmin.health.archive')}</h3><div className="grid grid-cols-2 gap-3 md:grid-cols-4"><Metric label={t('instanceAdmin.health.waiting')} value={number(health.upload.waiting.count)} /><Metric label={t('instanceAdmin.health.waitingBytes')} value={number(health.upload.waiting.bytes)} unit="B" /><Metric label={t('instanceAdmin.health.failed')} value={number(health.upload.failed.count)} /><Metric label={t('instanceAdmin.health.failedBytes')} value={number(health.upload.failed.bytes)} unit="B" /></div>
            <dl className="border-line grid gap-3 border-t pt-4 text-[13px] md:grid-cols-2"><Detail label={t('instanceAdmin.health.archiver')} value={t(health.archive.enabled ? 'instanceAdmin.enabled' : 'instanceAdmin.disabled')} /><Detail label={t('instanceAdmin.health.destination')} value={health.archive.provider ? `${health.archive.provider.name} · ${health.archive.provider.type}` : t('instanceAdmin.health.noDestination')} /><Detail label={t('instanceAdmin.health.verified')} value={stamp(health.upload.last_verified_at)} /><Detail label={t('instanceAdmin.health.oldest')} value={stamp(health.upload.oldest_unarchived_event_at)} />{health.archive.provider && <><Detail label={t('instanceAdmin.health.destinationStatus')} value={health.archive.provider.status} /><Detail label={t('instanceAdmin.health.lastCheck')} value={stamp(health.archive.provider.last_checked_at)} /></>}</dl>
            {health.archive.provider?.last_error && <p role="alert" className="text-ink2 text-[13px]">{t('instanceAdmin.health.destinationError')}</p>}
          </section>
        </>}
      </div>
    </div>}
  </>;
}

function Metric({ label, value, unit }: { label: string; value: string; unit?: string }) {
  return <div className="bg-surface border-line min-w-0 rounded-row border p-3"><p className="text-ink2 text-xs">{label}</p><p className="mt-2 break-all font-mono text-base">{value}{unit && <span className="text-ink2 ml-1 text-xs">{unit}</span>}</p></div>;
}
function Detail({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0"><dt className="text-ink2 text-xs">{label}</dt><dd className="mt-1 [overflow-wrap:anywhere]">{value}</dd></div>;
}
