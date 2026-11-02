import { useTranslation } from 'react-i18next';
import { useReadiness } from '../lib/useReadiness';
import { cn } from '../lib/cn';

const CHECK_ORDER = ['postgres', 'clickhouse', 'migrations_postgres', 'migrations_clickhouse'];

export default function Overview() {
  const { t } = useTranslation();
  const readiness = useReadiness();
  const checks = 'checks' in readiness ? readiness.checks : {};
  const tone = readiness.state === 'ready' ? 'ok' : readiness.state === 'loading' ? 'idle' : 'err';

  return (
    <div className="max-w-[640px]">
      <h1 className="text-[26px] font-extrabold tracking-tight">{t('overview.title')}</h1>
      <p className="text-ink2 mt-1.5 mb-6 max-w-[56ch] leading-relaxed">{t('overview.intro')}</p>

      <section aria-labelledby="status-title">
        <h2 id="status-title" className="sr-only">
          {t('status.title')}
        </h2>
        <p
          role="status"
          className={cn(
            'mb-3 inline-flex rounded-[10px] px-3 py-1.5 font-bold',
            tone === 'ok' && 'text-ok bg-[color-mix(in_srgb,var(--ok)_16%,var(--surface))]',
            tone === 'err' && 'text-err bg-[color-mix(in_srgb,var(--err)_16%,var(--surface))]',
            tone === 'idle' && 'text-ink2 bg-sunk',
          )}
        >
          {t(`status.${readiness.state}`)}
        </p>
        <ul className="bg-sunk rounded-card p-1.5">
          {CHECK_ORDER.map((name) => {
            const state = checks[name];
            return (
              <li key={name} className="odd:bg-transparent even:bg-surface flex justify-between rounded-control px-3.5 py-3">
                <span>{t(`status.checks.${name}`)}</span>
                <span className={cn('font-bold', state === 'ok' && 'text-ok', state === 'fail' && 'text-err', !state && 'text-ink3')}>
                  {state ? t(`status.${state}`) : '–'}
                </span>
              </li>
            );
          })}
        </ul>
      </section>
    </div>
  );
}
