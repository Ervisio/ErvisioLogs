import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useNavigate, useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { ShellContext } from '../components/Shell';
import SettingsField from '../components/SettingsFields';
import SettingsMetadata from '../components/SettingsMetadata';
import { api, ApiError, type Project, type SettingEntry, type SettingsScope } from '../lib/api';
import { useAuth, useSession } from '../lib/auth';
import { cn } from '../lib/cn';
import { projectPath, workspacePath } from '../lib/sections';
import { SETTING_GROUPS, parseSetting, settingChanged, settingDraft, settingGroup, type SettingDraft } from '../lib/settings';
import { useTenancy } from '../lib/tenancy';

export type SettingsProps = { scope: 'instance' | 'workspace' } | { scope: 'project'; project: Project };
const PROJECT_WRITERS = new Set(['owner', 'admin', 'developer']);

export default function Settings(props: SettingsProps) {
  const { t } = useTranslation();
  const { user } = useSession();
  const { workspace, projects } = useTenancy();
  const { topbar } = useOutletContext<ShellContext>();
  const navigate = useNavigate();
  const project = props.scope === 'project' ? props.project : undefined;
  const scope = useMemo<SettingsScope>(() => props.scope === 'instance'
    ? { type: 'instance' } : { type: props.scope, id: project?.id ?? workspace?.id ?? '' }, [props.scope, project?.id, workspace?.id]);
  const allowed = props.scope === 'instance' ? user.is_instance_admin : !!workspace && (!project || project.workspace_id === workspace.id);
  const canWrite = props.scope === 'instance' ? user.is_instance_admin : props.scope === 'workspace' ? workspace?.role === 'owner' : PROJECT_WRITERS.has(workspace?.role ?? '');
  const currentPath = props.scope === 'instance' ? '/admin/settings' : props.scope === 'workspace' && workspace ? workspacePath(workspace.id, 'settings') : project ? projectPath(project.workspace_id, project.id, 'settings') : '';

  return <>
    {topbar(<h1 className="truncate text-base font-bold">{t('settings.title')}</h1>)}
    {!allowed ? <p role="alert" className="text-ink2 p-6">{t('settings.forbidden')}</p> : <>
      <section className="border-line grid flex-none gap-3 border-b px-4 py-5 md:px-7">
        <p className="text-ink2 text-xs font-semibold">{t(`settings.scopes.${props.scope}`)}{props.scope !== 'instance' && (project || workspace) ? ` · ${project?.name ?? workspace?.name}` : ''}</p>
        <h2 className="text-xl font-bold">{t(`settings.headings.${props.scope}`)}</h2>
        <p className="text-ink2 max-w-[650px] text-[13px]">{t(`settings.intro.${props.scope}`)}</p>
        <label className="text-ink2 flex flex-wrap items-center gap-2 text-xs">{t('settings.scope')}
          <select value={currentPath} onChange={(event) => navigate(event.target.value)} className="bg-sunk border-field text-ink min-h-11 min-w-0 max-w-full rounded-row border px-2 font-medium md:min-h-9">
            {user.is_instance_admin && <option value="/admin/settings">{t('settings.scopes.instance')}</option>}
            {workspace && <option value={workspacePath(workspace.id, 'settings')}>{t('settings.scopes.workspace')} · {workspace.name}</option>}
            {projects.status === 'ready' && projects.projects.map((item) => <option key={item.id} value={projectPath(item.workspace_id, item.id, 'settings')}>{t('settings.scopes.project')} · {item.name}</option>)}
          </select>
        </label>
        {!canWrite && <p className="text-ink2 text-xs">{t('settings.readOnly')}</p>}
      </section>
      <SettingsContent key={`${scope.type}:${'id' in scope ? scope.id : ''}`} scope={scope} project={project} canWrite={canWrite} />
    </>}
  </>;
}

type Loaded = { entries: SettingEntry[] } | { error: string };

function SettingsContent({ scope, project, canWrite }: { scope: SettingsScope; project?: Project; canWrite: boolean }) {
  const { t } = useTranslation();
  const { retry } = useAuth();
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<{ attempt: number; data: Loaded } | null>(null);
  const [selected, setSelected] = useState(scope.type === 'instance' ? 'access' : 'collection');
  const [metadataVisited, setMetadataVisited] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, SettingDraft>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [saveError, setSaveError] = useState('');
  const mutation = useRef<AbortController | null>(null);
  const result = loaded?.attempt === attempt ? loaded.data : null;
  const entries = result && 'entries' in result ? result.entries : [];
  const groups: string[] = SETTING_GROUPS.filter((group) => entries.some((entry) => settingGroup(entry) === group));
  if (project) groups.push('metadata');
  const section = groups.includes(selected) ? selected : groups[0];
  const changed = entries.filter((entry) => settingChanged(entry, drafts[entry.key], scope));

  useEffect(() => {
    const ctrl = new AbortController();
    api.settings(scope, ctrl.signal).then((entries) => {
      if (!ctrl.signal.aborted) setLoaded({ attempt, data: { entries } });
    }).catch((err: unknown) => {
      if (ctrl.signal.aborted) return;
      if (err instanceof ApiError && err.status === 401) retry();
      else setLoaded({ attempt, data: { error: err instanceof ApiError && err.status === 403 ? t('settings.forbidden') : t('settings.loadFailed') } });
    });
    return () => ctrl.abort();
  }, [scope, attempt, retry, t]);

  useEffect(() => () => mutation.current?.abort(), []);

  const edit = (entry: SettingEntry, draft: SettingDraft) => {
    if (!canWrite || entry.locked || busy) return;
    setDrafts((cur) => ({ ...cur, [entry.key]: draft }));
    setErrors((cur) => { const next = { ...cur }; delete next[entry.key]; return next; });
    setNotice('');
    setSaveError('');
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canWrite || busy || changed.length === 0) return;
    const invalid: Record<string, string> = {};
    for (const entry of changed) {
      const draft = drafts[entry.key];
      if (draft.inherit) continue;
      const parsed = parseSetting(entry, draft);
      if (parsed.issue) invalid[entry.key] = String(t(parsed.issue.key, parsed.issue.values));
    }
    if (Object.keys(invalid).length > 0) {
      setErrors(invalid);
      setSelected(settingGroup(changed.find((entry) => invalid[entry.key])!));
      setSaveError(t('settings.checkValues'));
      return;
    }
    const ctrl = new AbortController();
    mutation.current = ctrl;
    setBusy(true);
    setErrors({});
    setNotice('');
    setSaveError('');
    let saved = 0;
    const acknowledged = new Map<string, SettingEntry>();
    const failed: Record<string, string> = {};
    for (const entry of changed) {
      if (ctrl.signal.aborted) return;
      const draft = drafts[entry.key];
      try {
        const updated = draft.inherit
          ? await api.resetSetting(scope, entry.key, ctrl.signal)
          : await api.putSetting(scope, entry.key, parseSetting(entry, draft).value!, ctrl.signal);
        if (ctrl.signal.aborted) return;
        saved++;
        acknowledged.set(updated.key, updated);
        setLoaded((cur) => cur && 'entries' in cur.data ? { ...cur, data: { entries: cur.data.entries.map((item) => item.key === updated.key ? updated : item) } } : cur);
        setDrafts((cur) => { const next = { ...cur }; delete next[entry.key]; return next; });
      } catch (err) {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) { retry(); return; }
        failed[entry.key] = err instanceof ApiError ? err.message : t('settings.saveFailed');
      }
    }
    try {
      const refreshed = await api.settings(scope, ctrl.signal);
      if (ctrl.signal.aborted) return;
      const confirmed = new Map(refreshed.map((entry) => [entry.key, entry]));
      for (const [key, entry] of acknowledged) confirmed.set(key, entry);
      setLoaded({ attempt, data: { entries: [...confirmed.values()] } });
    } catch (err) {
      if (ctrl.signal.aborted) return;
      if (err instanceof ApiError && err.status === 401) { retry(); return; }
      setSaveError(t('settings.refreshFailed'));
    }
    if (ctrl.signal.aborted) return;
    setErrors(failed);
    const failedKey = Object.keys(failed)[0];
    if (failedKey) {
      setSelected(settingGroup(changed.find((entry) => entry.key === failedKey)!));
      setSaveError(t('settings.partial', { saved, failed: Object.keys(failed).length }));
    } else setNotice(t('settings.saved', { count: saved }));
    setBusy(false);
  };

  if (!result) return <p role="status" className="text-ink2 flex items-center gap-2 p-6"><span className="spinner" aria-hidden />{t('settings.loading')}</p>;
  if ('error' in result) return <div role="alert" className="p-6"><p>{result.error}</p><button type="button" onClick={() => setAttempt((cur) => cur + 1)} className="text-hue min-h-11 underline">{t('common.retry')}</button></div>;
  return <div className="flex min-h-0 flex-1 flex-col md:flex-row">
    <nav aria-label={t('settings.sections')} className="border-line flex flex-none flex-wrap content-start gap-1 border-b p-4 md:w-[200px] md:flex-col md:border-r md:border-b-0 md:p-6">
      {groups.map((group) => <button key={group} type="button" disabled={busy} aria-current={section === group ? 'page' : undefined} onClick={() => { setSelected(group); if (group === 'metadata') setMetadataVisited(true); }} className={cn('min-h-11 rounded-row px-3 text-left text-xs disabled:opacity-60 md:min-h-9', section === group ? 'bg-sunk text-hue font-semibold' : 'text-ink2 hover:bg-sunk')}>{t(`settings.groups.${group}.label`)}</button>)}
    </nav>
    {project && (metadataVisited || section === 'metadata') && <div hidden={section !== 'metadata'} className="flex min-h-0 min-w-0 flex-1"><SettingsMetadata key={project.id} project={project} canWrite={canWrite} /></div>}
    <form noValidate hidden={section === 'metadata'} onSubmit={(event) => void submit(event)} className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 overflow-auto px-4 py-5 md:px-7">
        {section ? <><h2 className="text-[17px] font-bold">{t(`settings.groups.${section}.label`)}</h2><p className="text-ink2 mt-1 mb-1 text-[13px]">{t(`settings.groups.${section}.hint`)}</p></> : <p className="text-ink2">{t('settings.empty')}</p>}
        <div className="max-w-[800px]">{entries.filter((entry) => settingGroup(entry) === section).map((entry) => <SettingsField key={entry.key} entry={entry} scope={scope} draft={drafts[entry.key] ?? settingDraft(entry, scope)} disabled={!canWrite || busy} changed={settingChanged(entry, drafts[entry.key], scope)} error={errors[entry.key]} onChange={(draft) => edit(entry, draft)} />)}</div>
        {saveError && <p role="alert" className="text-err mt-4 text-[13px]">{saveError}</p>}
      </div>
      <footer className="bg-surface border-line flex flex-none flex-wrap items-center gap-2 border-t px-4 py-3 md:px-7">
        <p role="status" className="text-ink2 mr-auto basis-full text-xs md:basis-auto">{notice || t(!canWrite ? 'settings.readOnly' : changed.length ? 'settings.unsaved' : 'settings.noChanges')}</p>
        {canWrite && <><button type="button" disabled={busy || (Object.keys(drafts).length === 0 && Object.keys(errors).length === 0)} onClick={() => { setDrafts({}); setErrors({}); setSaveError(''); setNotice(''); }} className="border-line min-h-11 rounded-row border px-3 text-xs disabled:opacity-60 md:min-h-9">{t('settings.cancel')}</button><button type="submit" disabled={busy || changed.length === 0} className="bg-accent text-accent-ink min-h-11 rounded-row px-3 text-xs font-bold disabled:opacity-60 md:min-h-9">{t(busy ? 'settings.saving' : 'settings.save')}</button></>}
      </footer>
    </form>
  </div>;
}
