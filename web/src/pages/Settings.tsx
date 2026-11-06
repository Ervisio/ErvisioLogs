import { useCallback, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  api,
  request,
  type Environment,
  type Project,
  type RetentionInput,
  type RetentionPolicy,
  type Stream,
} from '../lib/api';
import { management, type Setting } from '../lib/manage-api';
import { useTenancy } from '../lib/tenancy';
import { projectPath, workspacePath } from '../lib/paths';
import { ActionForm, ConfirmDelete, Field, Page, State, useAction, useRemote } from '../components/Management';
import Tokens from './Tokens';
import Users from './Users';
import Channels from './Channels';
import { AuditPage } from './Administration';
import { Catalog } from './catalog';

export function RuntimeSettings({
  path,
  writable = true,
  group,
}: {
  path: string;
  writable?: boolean;
  group?: string;
}) {
  const load = useCallback((signal: AbortSignal) => management.settings(path, signal), [path]);
  const state = useRemote(load);
  return (
    <>
      <State {...state} retry={state.refresh} />
      {state.data
        ?.filter((s) => !group || s.key.startsWith(`${group}.`))
        .map((setting) => (
          <SettingRow
            key={`${path}:${setting.key}:${JSON.stringify(setting.value)}`}
            setting={setting}
            path={path}
            writable={writable}
            refresh={state.refresh}
          />
        ))}
    </>
  );
}
function SettingRow({
  setting: s,
  path,
  writable,
  refresh,
}: {
  setting: Setting;
  path: string;
  writable: boolean;
  refresh: () => void;
}) {
  const { t } = useTranslation();
  const [value, setValue] = useState(s.value);
  const action = useAction(refresh);
  const label = t(`settingLabels.${s.key.replaceAll('.', '_')}`, { defaultValue: s.key });
  return (
    <section className="manage-section">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="font-semibold">{label}</h2>
          <p className="text-ink3 text-xs">
            {s.locked
              ? t('manage.locked', { env: s.env })
              : t('manage.source', { source: t(`manage.sources.${s.source}`, { defaultValue: s.source }) })}
            {s.type === 'duration' ? ` · ${t('manage.seconds')}` : ''}
          </p>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <label className="min-w-0 flex-1">
          <span className="sr-only">{label}</span>
          {s.type === 'bool' ? (
            <input
              type="checkbox"
              checked={Boolean(value)}
              disabled={s.locked || !writable || action.busy}
              onChange={(e) => setValue(e.target.checked)}
              className="size-5 accent-[var(--accent)]"
            />
          ) : s.options?.length ? (
            <select
              className="manage-input"
              value={String(value)}
              disabled={s.locked || !writable || action.busy}
              onChange={(e) => setValue(e.target.value)}
            >
              {s.options.map((opt) => (
                <option key={opt}>{opt}</option>
              ))}
            </select>
          ) : (
            <input
              className="manage-input"
              type={s.type === 'int' || s.type === 'duration' ? 'number' : 'text'}
              value={String(value)}
              min={s.min}
              max={s.max}
              disabled={s.locked || !writable || action.busy}
              onChange={(e) =>
                setValue(s.type === 'int' || s.type === 'duration' ? Number(e.target.value) : e.target.value)
              }
            />
          )}
        </label>
        {writable && !s.locked && (
          <>
            <button
              type="button"
              className="manage-button manage-primary"
              disabled={action.busy || value === s.value}
              onClick={() => void action.run(() => management.putSetting(path, s.key, value))}
            >
              {t('manage.save')}
            </button>
            <button
              type="button"
              className="manage-button"
              disabled={action.busy || s.source === 'default' || s.source === 'env'}
              onClick={() => void action.run(() => management.resetSetting(path, s.key))}
            >
              {t('manage.inherit')}
            </button>
          </>
        )}
      </div>
      {action.message}
    </section>
  );
}
export function ProjectSettings({ project }: { project: Project }) {
  const { t } = useTranslation();
  const { tab = 'general' } = useParams();
  const { workspace } = useTenancy();
  const canWrite = !!workspace && ['owner', 'admin', 'developer'].includes(workspace.role);
  const tabs = ['general', 'environments', 'streams', 'tokens', 'catalog', 'retention', 'danger'];
  return (
    <Page
      title={t('manage.projectSettings')}
      subtitle={project.name}
      tabs={tabs
        .filter((key) =>
          key === 'tokens' ? canWrite : key !== 'danger' || workspace?.role === 'owner' || workspace?.role === 'admin',
        )
        .map((key) => ({
          path: projectPath(project.workspace_id, project.id, `settings/${key}`),
          label: t(`manage.${key}`),
        }))}
    >
      {tab === 'tokens' && canWrite && <Tokens project={project} embedded />}
      {tab === 'general' && (
        <>
          <Rename key={project.id} scope="project" id={project.id} name={project.name} writable={canWrite} />
          <RuntimeSettings path={`/projects/${project.id}`} writable={canWrite} />
        </>
      )}
      {(tab === 'streams' || tab === 'environments') && (
        <>
          <MetadataList key={`${project.id}:${tab}`} project={project} kind={tab} writable={canWrite} />
          {tab === 'streams' && <RuntimeSettings path={`/projects/${project.id}`} group="ingest" writable={canWrite} />}
        </>
      )}
      {tab === 'catalog' && <Catalog key={project.id} project={project} writable={canWrite} />}
      {tab === 'retention' && <Retention scope="project" id={project.id} writable={canWrite} />}
      {tab === 'danger' && (workspace?.role === 'owner' || workspace?.role === 'admin') && (
        <ProjectDanger project={project} />
      )}
    </Page>
  );
}
export function WorkspaceSettings() {
  const { t } = useTranslation();
  const { tab = 'general' } = useParams();
  const { workspace, refreshWorkspaces } = useTenancy();
  const navigate = useNavigate();
  if (!workspace)
    return (
      <Page title={t('manage.workspaceSettings')}>
        <p>{t('notFound.title')}</p>
      </Page>
    );
  const writable = workspace.role === 'owner';
  const manager = ['owner', 'admin'].includes(workspace.role);
  const alertManager = ['owner', 'admin', 'developer'].includes(workspace.role);
  const tabs = [
    'general',
    ...(manager ? ['members'] : []),
    ...(alertManager ? ['channels'] : []),
    'retention',
    ...(manager ? ['audit'] : []),
    ...(writable ? ['danger'] : []),
  ];
  if (tab === 'audit' && manager) return <AuditPage path={`/workspaces/${workspace.id}`} />;
  return (
    <Page
      title={t('manage.workspaceSettings')}
      subtitle={workspace.name}
      tabs={tabs.map((key) => ({ path: workspacePath(workspace.id, key), label: t(`manage.${key}`) }))}
    >
      {tab === 'members' && manager && <Users workspace={workspace} embedded />}
      {tab === 'channels' && alertManager && <Channels workspace={workspace} embedded />}
      {tab === 'general' && (
        <>
          <Rename key={workspace.id} scope="workspace" id={workspace.id} name={workspace.name} writable={writable} />
          <RuntimeSettings path={`/workspaces/${workspace.id}`} writable={writable} />
        </>
      )}
      {tab === 'retention' && <Retention scope="workspace" id={workspace.id} writable={writable} />}
      {tab === 'danger' && writable && (
        <ConfirmDelete
          slug={workspace.slug}
          action={async () => {
            await management.deleteWorkspace(workspace.id, workspace.slug);
            await refreshWorkspaces();
            navigate('/projects');
          }}
        />
      )}
    </Page>
  );
}
function Rename({
  scope,
  id,
  name,
  writable,
}: {
  scope: 'workspace' | 'project';
  id: string;
  name: string;
  writable: boolean;
}) {
  const { t } = useTranslation();
  const { refreshWorkspaces } = useTenancy();
  const [value, setValue] = useState(name);
  const action = useAction();
  return (
    <ActionForm
      busy={action.busy || !writable || value === name}
      submit={() =>
        action.run(async () => {
          if (scope === 'workspace') await management.renameWorkspace(id, value.trim());
          else await management.renameProject(id, value.trim());
          await refreshWorkspaces();
        })
      }
    >
      <Field label={t('manage.name')}>
        <input
          required
          maxLength={100}
          className="manage-input"
          value={value}
          disabled={!writable}
          onChange={(e) => setValue(e.target.value)}
        />
      </Field>
      {action.message}
    </ActionForm>
  );
}
export function MetadataList({
  project,
  kind,
  writable,
}: {
  project: Project;
  kind: 'streams' | 'environments';
  writable: boolean;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const load = useCallback(
    (signal: AbortSignal) =>
      kind === 'streams' ? api.streams(project.id, signal) : api.environments(project.id, signal),
    [kind, project.id],
  );
  const state = useRemote<Stream[] | Environment[]>(load);
  const action = useAction(state.refresh);
  return (
    <>
      <State {...state} retry={state.refresh} />
      {state.data?.map((item) => (
        <MetadataEditor
          key={item.id + JSON.stringify(item)}
          item={item}
          project={project.id}
          kind={kind}
          writable={writable}
          refresh={state.refresh}
        />
      ))}
      {writable && (
        <ActionForm
          busy={action.busy}
          label={t('manage.create')}
          submit={() =>
            action.run(async () => {
              await management.createMetadata(project.id, kind, name.trim());
              setName('');
            })
          }
        >
          <Field label={t('manage.technicalName')}>
            <input
              required
              pattern={kind === 'streams' ? '[a-z0-9][a-z0-9._\\-]{0,62}' : '[a-z0-9][a-z0-9\\-]{0,38}'}
              maxLength={kind === 'streams' ? 63 : 39}
              className="manage-input"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          {action.message}
        </ActionForm>
      )}
    </>
  );
}
function MetadataEditor({
  item,
  project,
  kind,
  writable,
  refresh,
}: {
  item: Stream | Environment;
  project: string;
  kind: 'streams' | 'environments';
  writable: boolean;
  refresh: () => void;
}) {
  const { t } = useTranslation();
  const [display, setDisplay] = useState(item.display_name ?? '');
  const [description, setDescription] = useState(item.description ?? '');
  const [color, setColor] = useState(item.color ?? '');
  const action = useAction(refresh);
  return (
    <details className="border-line rounded-row border p-4">
      <summary className="cursor-pointer font-semibold">
        {item.display_name || item.name}
        <span className="text-ink3 ml-3 font-mono text-xs">{item.name}</span>
      </summary>
      <ActionForm
        className="mt-4"
        busy={action.busy || !writable}
        submit={() =>
          action.run(() =>
            management.updateMetadata(project, kind, item.id, { display_name: display, description, color }),
          )
        }
      >
        <Field label={t('manage.displayName')}>
          <input
            className="manage-input"
            value={display}
            maxLength={100}
            disabled={!writable}
            onChange={(e) => setDisplay(e.target.value)}
          />
        </Field>
        <Field label={t('manage.description')}>
          <textarea
            className="manage-input"
            value={description}
            maxLength={500}
            disabled={!writable}
            onChange={(e) => setDescription(e.target.value)}
          />
        </Field>
        <Field label={t('manage.color')}>
          <input
            type="color"
            className="manage-input max-w-24"
            value={color || '#7798ff'}
            disabled={!writable}
            onChange={(e) => setColor(e.target.value)}
          />
          {writable && (
            <button
              type="button"
              className="manage-button"
              disabled={!color || action.busy}
              onClick={() => setColor('')}
            >
              {t('manage.inherit')}
            </button>
          )}
        </Field>
        {action.message}
      </ActionForm>
      {writable && (
        <button
          type="button"
          className="manage-button text-err mt-3"
          disabled={action.busy}
          onClick={() => {
            if (window.confirm(t('manage.deleteNamed', { name: item.name })))
              void action.run(() => management.deleteMetadata(project, kind, item.id));
          }}
        >
          {t('manage.delete')}
        </button>
      )}
    </details>
  );
}
export function Retention({
  scope,
  id,
  writable,
}: {
  scope: 'instance' | 'workspace' | 'project';
  id: string;
  writable: boolean;
}) {
  const path =
    scope === 'instance' ? '/admin/retention' : `/${scope === 'workspace' ? 'workspaces' : 'projects'}/${id}/retention`;
  const load = useCallback(
    (signal: AbortSignal) => request<{ items: RetentionPolicy[] | null }>('GET', path, undefined, signal),
    [path],
  );
  const state = useRemote(load);
  const own = state.data?.items?.find((p) => p.scope_type === scope && p.scope_id === id);
  return (
    <>
      <State {...state} retry={state.refresh} />
      {state.data && (
        <RetentionForm
          key={`${scope}:${id}:${JSON.stringify(own)}`}
          scope={scope}
          id={id}
          own={own}
          writable={writable}
          refresh={state.refresh}
        />
      )}
    </>
  );
}
function RetentionForm({
  scope,
  id,
  own,
  writable,
  refresh,
}: {
  scope: 'instance' | 'workspace' | 'project';
  id: string;
  own?: RetentionPolicy;
  writable: boolean;
  refresh: () => void;
}) {
  const { t } = useTranslation();
  const [local, setLocal] = useState(own?.local_age_hours?.toString() ?? '');
  const [global, setGlobal] = useState(own?.global_age_hours?.toString() ?? '');
  const action = useAction(refresh);
  const body: RetentionInput = {
    local_age_hours: local === '' ? null : Number(local),
    global_age_hours: global === '' ? null : Number(global),
  };
  return (
    <ActionForm
      busy={action.busy || !writable}
      submit={() =>
        action.run(() => {
          if (scope !== 'instance' && local === '' && global === '')
            return own ? management.deleteRetention(scope, id) : Promise.resolve();
          return management.putRetention(
            scope,
            id,
            scope === 'instance'
              ? { ...body, cache_max_bytes: own?.cache_max_bytes, high_pct: own?.high_pct, low_pct: own?.low_pct }
              : body,
          );
        })
      }
    >
      <p className="text-ink2">{t('manage.retentionHint')}</p>
      <Field label={t('manage.localHours')}>
        <input
          className="manage-input"
          type="number"
          min={1}
          max={87840}
          step={1}
          value={local}
          disabled={!writable}
          onChange={(e) => setLocal(e.target.value)}
        />
      </Field>
      <Field label={t('manage.globalHours')}>
        <input
          className="manage-input"
          type="number"
          min={1}
          max={87840}
          step={1}
          value={global}
          disabled={!writable}
          onChange={(e) => setGlobal(e.target.value)}
        />
      </Field>
      {writable && own && scope !== 'instance' && (
        <button
          type="button"
          className="manage-button justify-self-start"
          onClick={() => void action.run(() => management.deleteRetention(scope, id))}
        >
          {t('manage.inherit')}
        </button>
      )}
      {action.message}
    </ActionForm>
  );
}
function ProjectDanger({ project }: { project: Project }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { refreshWorkspaces } = useTenancy();
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const load = useCallback((signal: AbortSignal) => management.purges(project.id, signal), [project.id]);
  const state = useRemote(load);
  return (
    <>
      <ConfirmDelete
        slug={project.slug}
        action={async () => {
          await management.deleteProject(project.id, project.slug);
          await refreshWorkspaces();
          navigate('/projects');
        }}
      />
      <section className="grid gap-3">
        <h2 className="font-bold">{t('manage.purge')}</h2>
        <Field label={t('manage.from')}>
          <input
            className="manage-input"
            type="datetime-local"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </Field>
        <Field label={t('manage.to')}>
          <input className="manage-input" type="datetime-local" value={to} onChange={(e) => setTo(e.target.value)} />
        </Field>
        <ConfirmDelete
          slug={project.slug}
          label={t('manage.purge')}
          action={async () => {
            await management.purge(project.id, {
              confirm: project.slug,
              ...(from ? { from: new Date(from).toISOString() } : {}),
              ...(to ? { to: new Date(to).toISOString() } : {}),
            });
            state.refresh();
          }}
        />
        <State {...state} retry={state.refresh} />
        {state.data?.map((job) => (
          <p key={job.id} className="text-ink2 text-sm">
            {job.id} · {job.status}
            {job.error ? ` · ${job.error}` : ''}
          </p>
        ))}
      </section>
    </>
  );
}
