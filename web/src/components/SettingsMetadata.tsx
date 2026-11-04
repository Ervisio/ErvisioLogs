import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError, type Environment, type MetadataPatch, type Project, type Stream } from '../lib/api';
import { useAuth } from '../lib/auth';
import { SETTINGS_INPUT } from './SettingsFields';

type Item = { kind: 'stream' | 'environment'; value: Stream | Environment };
type Fields = Required<MetadataPatch>;
const fields = (item: Item): Fields => ({ display_name: item.value.display_name ?? '', description: item.value.description ?? '', color: item.value.color ?? '' });
const itemKey = (item: Item) => `${item.kind}:${item.value.id}`;
type Loaded = { items: Item[] } | { error: string };

export default function SettingsMetadata({ project, canWrite }: { project: Project; canWrite: boolean }) {
  const { t } = useTranslation();
  const { retry } = useAuth();
  const [loaded, setLoaded] = useState<{ key: string; data: Loaded } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [selected, setSelected] = useState('');
  const [drafts, setDrafts] = useState<Record<string, Fields>>({});
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const mutation = useRef<AbortController | null>(null);
  const key = `${project.id}:${attempt}`;
  const result = loaded?.key === key ? loaded.data : null;
  const items = result && 'items' in result ? result.items : [];
  const current = items.find((item) => itemKey(item) === selected) ?? items[0];
  const currentKey = current ? itemKey(current) : '';
  const value = current ? drafts[currentKey] ?? fields(current) : null;
  const dirty = current && value ? JSON.stringify(value) !== JSON.stringify(fields(current)) : false;

  useEffect(() => {
    const ctrl = new AbortController();
    Promise.all([api.streams(project.id, ctrl.signal), api.environments(project.id, ctrl.signal)])
      .then(([streams, environments]) => {
        if (!ctrl.signal.aborted) setLoaded({ key, data: { items: [...streams.map((value): Item => ({ kind: 'stream', value })), ...environments.map((value): Item => ({ kind: 'environment', value }))] } });
      })
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) retry();
        else setLoaded({ key, data: { error: err instanceof ApiError && err.status === 403 ? t('settings.forbidden') : t('settings.metadata.loadFailed') } });
      });
    return () => ctrl.abort();
  }, [project.id, key, retry, t]);

  useEffect(() => () => mutation.current?.abort(), []);

  const change = (field: keyof Fields, text: string) => {
    if (!canWrite || !value) return;
    setDrafts((cur) => ({ ...cur, [currentKey]: { ...value, [field]: text } }));
    setError('');
    setNotice('');
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canWrite || busy || !current || !value || !dirty) return;
    const patch = { display_name: value.display_name.trim(), description: value.description.trim(), color: value.color.trim().toLowerCase() };
    if (Array.from(patch.display_name).length > 100 || patch.display_name.includes('\0')) return setError(t('settings.metadata.invalidName'));
    if (Array.from(patch.description).length > 500 || patch.description.includes('\0')) return setError(t('settings.metadata.invalidDescription'));
    if (patch.color !== '' && !/^#[0-9a-f]{6}$/.test(patch.color)) return setError(t('settings.metadata.invalidColor'));
    const ctrl = new AbortController();
    mutation.current = ctrl;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const updated = current.kind === 'stream'
        ? await api.updateStream(project.id, current.value.id, patch, ctrl.signal)
        : await api.updateEnvironment(project.id, current.value.id, patch, ctrl.signal);
      if (ctrl.signal.aborted) return;
      setLoaded({ key, data: { items: items.map((item) => itemKey(item) === currentKey ? { ...item, value: updated } : item) } });
      setDrafts((cur) => { const next = { ...cur }; delete next[currentKey]; return next; });
      setNotice(t('settings.metadata.saved'));
    } catch (err) {
      if (ctrl.signal.aborted) return;
      if (err instanceof ApiError && err.status === 401) retry();
      else setError(err instanceof ApiError ? err.message : t('settings.saveFailed'));
    } finally {
      if (!ctrl.signal.aborted) setBusy(false);
    }
  };

  return <form noValidate onSubmit={(event) => void submit(event)} className="flex min-h-0 min-w-0 flex-1 flex-col">
    <div className="min-h-0 flex-1 overflow-auto px-4 py-5 md:px-7">
      <h2 className="text-[17px] font-bold">{t('settings.groups.metadata.label')}</h2>
      <p className="text-ink2 mt-1 text-[13px]">{t('settings.groups.metadata.hint')}</p>
      {!result && <p role="status" className="text-ink2 mt-5">{t('settings.loading')}</p>}
      {result && 'error' in result && <div role="alert" className="mt-5"><p>{result.error}</p><button type="button" onClick={() => setAttempt((cur) => cur + 1)} className="text-hue min-h-11 underline">{t('common.retry')}</button></div>}
      {result && 'items' in result && items.length === 0 && <p className="text-ink2 mt-5">{t('settings.metadata.empty')}</p>}
      {current && value && <div className="mt-5 grid max-w-[580px] gap-4">
        <label className="grid gap-1 text-xs">{t('settings.metadata.choose')}<select value={currentKey} disabled={busy} onChange={(event) => { setSelected(event.target.value); setError(''); setNotice(''); }} className={SETTINGS_INPUT}>{items.map((item) => <option key={itemKey(item)} value={itemKey(item)}>{t(`settings.metadata.${item.kind}`)} · {item.value.display_name || item.value.name}</option>)}</select></label>
        <label className="grid gap-1 text-[13px] font-semibold">{t('settings.metadata.technicalName')}<input className={SETTINGS_INPUT} value={current.value.name} readOnly /><span className="text-ink2 text-xs font-normal">{t('settings.metadata.immutable')}</span></label>
        <label className="grid gap-1 text-[13px] font-semibold">{t('settings.metadata.displayName')}<input className={SETTINGS_INPUT} value={value.display_name} disabled={!canWrite || busy} onChange={(event) => change('display_name', event.target.value)} /></label>
        <label className="grid gap-1 text-[13px] font-semibold">{t('settings.metadata.description')}<textarea className={`${SETTINGS_INPUT} min-h-24 py-2`} value={value.description} disabled={!canWrite || busy} onChange={(event) => change('description', event.target.value)} /></label>
        <label className="grid gap-1 text-[13px] font-semibold">{t('settings.metadata.color')}<input className={SETTINGS_INPUT} value={value.color} disabled={!canWrite || busy} placeholder="#ffb547" onChange={(event) => change('color', event.target.value)} /><span className="text-ink2 text-xs font-normal">{t('settings.metadata.colorHint')}</span></label>
        <p className="text-ink2 text-xs">{t('settings.metadata.noInheritance')}</p>
      </div>}
      {error && <p role="alert" className="text-err mt-4 text-[13px]">{error}</p>}
    </div>
    <footer className="bg-surface border-line flex flex-wrap items-center gap-2 border-t px-4 py-3 md:px-7">
      <p role="status" className="text-ink2 mr-auto basis-full text-xs md:basis-auto">{notice || t(!canWrite ? 'settings.readOnly' : dirty ? 'settings.unsaved' : 'settings.noChanges')}</p>
      {canWrite && <><button type="button" disabled={busy || !dirty} onClick={() => { setDrafts((cur) => { const next = { ...cur }; delete next[currentKey]; return next; }); setError(''); setNotice(''); }} className="border-line min-h-11 rounded-row border px-3 text-xs disabled:opacity-60 md:min-h-9">{t('settings.cancel')}</button><button type="submit" disabled={busy || !dirty} className="bg-accent text-accent-ink min-h-11 rounded-row px-3 text-xs font-bold disabled:opacity-60 md:min-h-9">{t(busy ? 'settings.saving' : 'settings.save')}</button></>}
    </footer>
  </form>;
}
