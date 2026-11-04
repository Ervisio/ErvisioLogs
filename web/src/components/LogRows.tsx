import { Fragment } from 'react';
import { useTranslation } from 'react-i18next';
import type { LogItem } from '../lib/api';
import { cn } from '../lib/cn';
import { entityLabel, fullTimestamp, levelColor, logDay, logEntities, relativeTimestamp } from '../lib/logPresentation';

export default function LogRows({ items, selectedId, liveIds, now, onSelect }: {
  items: LogItem[];
  selectedId?: string;
  liveIds: ReadonlySet<string>;
  now: number;
  onSelect: (item: LogItem) => void;
}) {
  const { t, i18n } = useTranslation();
  return (
    <div aria-label={t('explorer.caption')}>
      {items.map((item, index) => {
        const day = logDay(item.timestamp, i18n.language);
        const newDay = index === 0 || day !== logDay(items[index - 1].timestamp, i18n.language);
        return (
          <Fragment key={item.id}>
            {newDay && <h2 className="bg-surface border-line text-ink2 border-b px-4 py-2 text-xs font-medium">{day}</h2>}
            <button
              type="button"
              onClick={() => onSelect(item)}
              aria-label={t('explorer.open', { time: fullTimestamp(item.timestamp, i18n.language) })}
              aria-expanded={selectedId === item.id}
              className={cn('border-line hover:bg-sunk grid min-h-16 w-full grid-cols-[64px_8px_minmax(0,1fr)] items-start gap-2 border-b px-3 py-3 text-left md:grid-cols-[76px_8px_minmax(0,1fr)] md:gap-3 md:px-4', selectedId === item.id && 'bg-sunk outline-hue outline-1 -outline-offset-1')}
            >
              <time dateTime={item.timestamp} title={fullTimestamp(item.timestamp, i18n.language)} className="text-ink2 pt-0.5 text-xs leading-5 tabular-nums">{relativeTimestamp(item.timestamp, i18n.language, now)}</time>
              <span className={cn('mt-2 size-1.5 rounded-full bg-current', levelColor(item.level))} title={t(`explorer.levelNames.${item.level}`, { defaultValue: item.level })} aria-label={t(`explorer.levelNames.${item.level}`, { defaultValue: item.level })} />
              <span className="grid min-w-0 gap-1">
                <span className="text-ink text-[14px] leading-5 break-words [overflow-wrap:anywhere]">{item.message || item.event}</span>
                <span className="text-ink2 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                  {item.event && item.message && <span className="min-w-0 truncate font-mono text-[11px]">{item.event}</span>}
                  {item.stream && <span className="min-w-0 truncate"># {item.stream}</span>}
                  {(item.resource || item.service) && <span className="min-w-0 truncate font-mono text-[11px]">{item.resource || item.service}</span>}
                  {logEntities(item).map(({ key, entity }) => <span key={key} className="border-line max-w-full truncate rounded border px-1.5 py-0.5" title={t(`logDetails.${key}`)}>{entityLabel(entity)}</span>)}
                  {item.tier === 'archive' && <span className="border-line rounded border px-1.5 py-0.5">{t('explorer.tier.archive')}</span>}
                  {liveIds.has(item.id) && <span className="border-hue text-hue rounded border px-1.5 py-0.5">{t('explorer.tier.live')}</span>}
                </span>
              </span>
            </button>
          </Fragment>
        );
      })}
    </div>
  );
}
