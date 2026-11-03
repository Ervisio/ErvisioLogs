import { useTranslation } from 'react-i18next';
import { useOutletContext } from 'react-router-dom';
import type { ShellContext } from '../components/Shell';
import { useReadiness } from '../lib/useReadiness';
import { cn } from '../lib/cn';

const CHECK_ORDER = ['postgres', 'clickhouse', 'migrations_postgres', 'migrations_clickhouse'];

/** System status: what /readyz reports for this server. */
export default function Overview() {
  const { t } = useTranslation();
  const { topbar } = useOutletContext<ShellContext>();
  const readiness = useReadiness();
  const checks = 'checks' in readiness ? readiness.checks : {};
  const tone = readiness.state === 'ready' ? 'ok' : readiness.state === 'loading' ? 'idle' : 'err';

  return (
    <>
      {topbar(<h1 className="truncate text-base font-bold">{t('status.title')}</h1>)}
      <div className="flex-1 overflow-auto p-5 md:p-8">
        <div className="max-w-[640px]">
          <p className="text-ink2 mb-6 max-w-[56ch] leading-relaxed">{t('status.intro')}</p>
          <p
            role="status"
            className={cn(
              'mb-3 inline-flex rounded-[10px] px-3 py-1.5 font-bold',
              tone === 'ok' && 'text-ok bg-[color-mix(in_srgb,var(--ok)_6%,var(--surface))]',
              tone === 'err' && 'text-err bg-[color-mix(in_srgb,var(--err)_6%,var(--surface))]',
              tone === 'idle' && 'text-ink2 bg-sunk',
            )}
          >
            {t(`status.${readiness.state}`)}
          </p>
          <ul className="bg-sunk rounded-card p-1.5">
            {CHECK_ORDER.map((name) => {
              const state = checks[name];
              return (
                <li key={name} className="even:bg-surface flex justify-between rounded-control px-3.5 py-3 odd:bg-transparent">
                  <span>{t(`status.checks.${name}`)}</span>
                  <span className={cn('font-bold', state === 'ok' && 'text-ok', state === 'fail' && 'text-err', !state && 'text-ink3')}>
                    {state ? t(`status.${state}`) : '–'}
                  </span>
                </li>
              );
            })}
          </ul>
        </div>
      </div>
    </>
  );
}
