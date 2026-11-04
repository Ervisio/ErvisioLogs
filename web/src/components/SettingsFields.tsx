import { useTranslation } from 'react-i18next';
import type { SettingEntry, SettingsScope } from '../lib/api';
import { settingLabelKey, type SettingDraft } from '../lib/settings';

export const SETTINGS_INPUT = 'bg-sunk border-field min-h-11 w-full min-w-0 rounded-row border px-2 text-[13px] disabled:text-ink2 disabled:cursor-not-allowed md:min-h-9';

export function SettingDisplay({ entry, value }: { entry: SettingEntry; value: SettingEntry['value'] }) {
  const { t, i18n } = useTranslation();
  if (typeof value === 'boolean') return <>{t(value ? 'settings.enabled' : 'settings.disabled')}</>;
  if (entry.type === 'duration' && typeof value === 'number') {
    if (value === 0) return <>{t('settings.duration.seconds', { count: 0 })}</>;
    for (const [unit, factor] of [['days', 86400], ['hours', 3600], ['minutes', 60], ['seconds', 1]] as const) {
      if (value > 0 && value % factor === 0) return <>{t(`settings.duration.${unit}`, { count: value / factor })}</>;
    }
  }
  return <>{value === '' ? t('settings.notSet') : typeof value === 'number' ? new Intl.NumberFormat(i18n.language).format(value) : value}</>;
}

export default function SettingsField({ entry, scope, draft, disabled, changed, error, onChange }: {
  entry: SettingEntry;
  scope: SettingsScope;
  draft: SettingDraft;
  disabled: boolean;
  changed: boolean;
  error?: string;
  onChange: (draft: SettingDraft) => void;
}) {
  const { t } = useTranslation();
  const id = `setting-${entry.key.replaceAll('.', '-')}`;
  const label = settingLabelKey(entry);
  const value = draft.inherit ? entry.inherited : draft.value;
  const blocked = disabled || entry.locked;
  const field = { id, className: SETTINGS_INPUT, disabled: blocked || draft.inherit, 'aria-invalid': !!error, 'aria-describedby': `${id}-hint ${id}-source${error ? ` ${id}-error` : ''}` };
  return <div className="border-line grid gap-3 border-b py-5 md:grid-cols-[minmax(0,1fr)_minmax(180px,240px)] md:gap-6" data-setting={entry.key}>
    <div className="min-w-0">
      <label htmlFor={id} className="block text-[13px] font-bold">{t(`${label}.label`)}</label>
      <p id={`${id}-hint`} className="text-ink2 mt-1 text-xs leading-relaxed">{t(`${label}.hint`)}</p>
      <details className="text-ink2 mt-1 text-[11px]"><summary className="flex min-h-11 cursor-pointer items-center md:min-h-8">{t('settings.technical')}</summary>
        <code className="block break-all font-mono text-[10px]">{entry.key} · {entry.type}</code>
        {entry.env && <code className="mt-1 block break-all font-mono text-[10px]">{entry.env}</code>}
      </details>
    </div>
    <div className="grid min-w-0 content-start gap-2">
      <p id={`${id}-source`} className="text-ink2 text-[11px]" data-source={entry.source}>{t(`settings.sources.${entry.source}`)}{changed && <span className="text-hue"> · {t('settings.pending')}</span>}</p>
      {entry.type === 'bool'
        ? <select {...field} value={String(value)} onChange={(e) => onChange({ ...draft, value: e.target.value === 'true' })}><option value="true">{t('settings.enabled')}</option><option value="false">{t('settings.disabled')}</option></select>
        : entry.type === 'choice'
          ? <select {...field} value={String(value)} onChange={(e) => onChange({ ...draft, value: e.target.value })}>{entry.options?.map((option) => <option key={option} value={option}>{option}</option>)}</select>
          : <input {...field} type={entry.type === 'int' || entry.type === 'duration' ? 'number' : 'text'} min={entry.min} max={entry.max} step="1" value={String(value)} onChange={(e) => onChange({ ...draft, value: e.target.value })} autoComplete="off" />}
      {entry.type === 'duration' && <p className="text-ink2 text-[11px]">{t('settings.seconds')} · <SettingDisplay entry={entry} value={typeof value === 'number' ? value : Number(value)} /></p>}
      {entry.locked ? <p className="text-ink2 border-line border-l-2 pl-2 text-xs">{t('settings.locked')}</p> : <>
        <label className="text-ink2 flex min-h-11 cursor-pointer items-center gap-2 text-xs md:min-h-9">
          <input type="checkbox" className="size-5 flex-none accent-accent md:size-4" checked={draft.inherit} disabled={blocked} onChange={(e) => onChange({ ...draft, inherit: e.target.checked })} />
          <span>{t(scope.type === 'instance' ? 'settings.useDefault' : 'settings.useInherited')}<span className="mt-0.5 block text-[11px]"><SettingDisplay entry={entry} value={entry.inherited} /></span></span>
        </label>
        {entry.source === scope.type && !draft.inherit && <button type="button" disabled={blocked} onClick={() => onChange({ ...draft, inherit: true })} className="text-hue min-h-11 justify-self-start text-left text-[11px] underline underline-offset-2 disabled:opacity-60 md:min-h-8">{t('settings.resetValue')}</button>}
      </>}
      {error && <p id={`${id}-error`} role="alert" className="text-err text-xs">{error}</p>}
    </div>
  </div>;
}
