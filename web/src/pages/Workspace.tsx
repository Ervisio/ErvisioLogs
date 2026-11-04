import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { Link, Navigate, useLocation, useNavigate, useOutletContext, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Compass } from 'lucide-react';
import type { ShellContext } from '../components/Shell';
import { api, ApiError, type Project } from '../lib/api';
import { initials, useTenancy } from '../lib/tenancy';
import { projectPath, workspacePath } from '../lib/sections';
import { useAuth } from '../lib/auth';
import Alerts from './Alerts';
import Channels from './Channels';
import Logs from './Logs';
import StoragePage from './Storage';
import Tokens from './Tokens';
import Users from './Users';
import Settings from './Settings';

function Title({ children }: { children: ReactNode }) {
  return <h1 className="truncate text-base font-bold">{children}</h1>;
}

function Notice({ icon, title, text, children }: { icon: ReactNode; title: string; text: string; children?: ReactNode }) {
  return (
    <div className="flex flex-1 items-center justify-center overflow-auto p-6">
      <div className="max-w-[380px] text-center">
        <div className="text-ink3 mb-3 flex justify-center">{icon}</div>
        <h2 className="mb-1 text-[17px] font-bold">{title}</h2>
        <p className="text-ink2">{text}</p>
        {children}
      </div>
    </div>
  );
}

function NotFound() {
  const { t } = useTranslation();
  const { topbar } = useOutletContext<ShellContext>();
  return (
    <>
      {topbar(<Title>{t('notFound.title')}</Title>)}
      <Notice icon={<Compass className="size-7" aria-hidden />} title={t('notFound.title')} text={t('notFound.text')}>
        <Link to="/projects" className="text-hue mt-4 inline-flex min-h-11 items-center font-semibold underline underline-offset-2">
          {t('notFound.action')}
        </Link>
      </Notice>
    </>
  );
}
export { NotFound };

/** "/" opens the last project, or the page that creates the first one. */
export function Home() {
  const { t } = useTranslation();
  const { projects, workspace } = useTenancy();
  const { topbar } = useOutletContext<ShellContext>();
  const { workspaceId } = useParams();
  if (workspaceId && !workspace) return <NotFound />;
  if (!workspace) return <Navigate to="/projects" replace />;
  if (projects.status === 'failed') return <Navigate to={workspacePath(workspace.id)} replace />;
  if (projects.status === 'loading') {
    return (
      <>
        {topbar(<Title>{t('app.name')}</Title>)}
        <p role="status" className="text-ink2 flex items-center gap-2 p-6">
          <span className="spinner" aria-hidden />
          {t('projects.loading')}
        </p>
      </>
    );
  }
  const last = localStorage.getItem('project');
  const target = projects.projects.find((p) => p.id === last) ?? projects.projects[0];
  return <Navigate to={target ? projectPath(workspace.id, target.id) : workspacePath(workspace.id)} replace />;
}

type FoundProject = { state: 'loading' } | { state: 'failed' } | { state: 'missing' } | { state: 'ready'; project: Project };

function useProjectFromUrl(): FoundProject {
  const { projectId } = useParams();
  const { projects, workspace } = useTenancy();
  if (!workspace) return { state: 'missing' };
  if (projects.status !== 'ready') return { state: projects.status };
  const project = projects.projects.find((p) => p.id === projectId && p.workspace_id === workspace.id);
  if (project) return { state: 'ready', project };
  return { state: 'missing' };
}

export function LegacyProject() {
  const { projectId, '*': suffix = '' } = useParams();
  const location = useLocation();
  const { retry } = useAuth();
  const { projects, workspace, workspaces } = useTenancy();
  const [searched, setSearched] = useState<{ key: string; project?: Project; failed: boolean } | null>(null);
  const project = projects.status === 'ready' ? projects.projects.find((p) => p.id === projectId) : undefined;
  const key = `${workspace?.id}:${projectId}`;
  const others = workspaces.filter((w) => w.id !== workspace?.id);
  const needsLookup = projects.status === 'ready' && !project && others.length > 0;

  useEffect(() => {
    if (!needsLookup) return;
    const ctrl = new AbortController();
    void Promise.allSettled(workspaces.filter((w) => w.id !== workspace?.id).map(async (w) => {
      const list = await api.projects(w.id, ctrl.signal);
      return list.find((p) => p.id === projectId && p.workspace_id === w.id);
    })).then((results) => {
      if (ctrl.signal.aborted) return;
      if (results.some((r) => r.status === 'rejected' && r.reason instanceof ApiError && r.reason.status === 401)) {
        retry();
        return;
      }
      const hit = results.find((r) => r.status === 'fulfilled' && r.value);
      setSearched({ key, project: hit?.status === 'fulfilled' ? hit.value : undefined, failed: results.some((r) => r.status === 'rejected') });
    });
    return () => ctrl.abort();
  }, [needsLookup, workspaces, workspace?.id, projectId, key, retry]);

  if (projects.status !== 'ready') return <ProjectPending failed={projects.status === 'failed'} />;
  if (needsLookup && searched?.key !== key) return <ProjectPending failed={false} />;
  const resolved = project ?? (searched?.key === key ? searched.project : undefined);
  if (!resolved) return searched?.key === key && searched.failed ? <ProjectPending failed /> : <NotFound />;
  const path = suffix === 'users' ? workspacePath(resolved.workspace_id, 'members')
    : suffix === 'integrations' ? workspacePath(resolved.workspace_id, 'channels')
    : projectPath(resolved.workspace_id, resolved.id, suffix);
  return <Navigate to={`${path}${location.search}${location.hash}`} replace />;
}

function ProjectPending({ failed }: { failed: boolean }) {
  const { t } = useTranslation();
  const { topbar } = useOutletContext<ShellContext>();
  const { reload } = useTenancy();
  return (
    <>
      {topbar(<Title>{t('app.name')}</Title>)}
      {failed ? (
        <div role="alert" className="p-6">
          <p className="text-ink2 mb-2">{t('projects.loadFailed')}</p>
          <button type="button" onClick={reload} className="text-hue min-h-11 font-semibold underline underline-offset-2">
            {t('common.retry')}
          </button>
        </div>
      ) : (
        <p role="status" className="text-ink2 flex items-center gap-2 p-6">
          <span className="spinner" aria-hidden />
          {t('projects.loading')}
        </p>
      )}
    </>
  );
}

export function Explorer() {
  const found = useProjectFromUrl();
  if (found.state === 'loading' || found.state === 'failed') return <ProjectPending failed={found.state === 'failed'} />;
  if (found.state === 'missing') return <NotFound />;
  return <Logs key={found.project.id} project={found.project} live={false} />;
}

export function SectionPage() {
  const { section } = useParams();
  const { workspace } = useTenancy();
  const found = useProjectFromUrl();
  if (found.state === 'loading' || found.state === 'failed') return <ProjectPending failed={found.state === 'failed'} />;
  if (found.state === 'missing') return <NotFound />;
  if (section === 'live' || section === 'logs') return <Logs key={`${found.project.id}:${section}`} project={found.project} live={section === 'live'} />;
  if (section === 'storage') return <StoragePage key={found.project.id} project={found.project} />;
  if (section === 'tokens') return <Tokens key={found.project.id} project={found.project} />;
  if (section === 'settings') return <Settings key={found.project.id} scope="project" project={found.project} />;
  if (section === 'alerts' && workspace) return <Alerts key={found.project.id} project={found.project} workspace={workspace} />;
  return <NotFound />;
}

export function WorkspaceSectionPage() {
  const { workspaceSection } = useParams();
  const { workspace } = useTenancy();
  if (!workspace) return <NotFound />;
  if (workspaceSection === 'settings') return <Settings key={workspace.id} scope="workspace" />;
  if (workspaceSection === 'members') return <Users key={workspace.id} workspace={workspace} />;
  if (workspaceSection === 'channels') return <Channels key={workspace.id} workspace={workspace} />;
  return <NotFound />;
}

export function InstanceStorage() {
  return <StoragePage />;
}

function NameForm(props: {
  id: string;
  label: string;
  hint?: string;
  idle: string;
  busy: string;
  create: (name: string) => Promise<void>;
}) {
  const { t } = useTranslation();
  const { id, label, hint, idle, busy, create } = props;
  const [name, setName] = useState('');
  const [invalid, setInvalid] = useState<string | null>(null);
  const [working, setWorking] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (working) return;
    const value = name.trim();
    if (!value) return setInvalid(t('projects.nameRequired'));
    if (Array.from(value).length > 100) return setInvalid(t('projects.nameTooLong'));
    setInvalid(null);
    setWorking(true);
    try {
      await create(value);
      setName('');
    } catch (err) {
      const status = err instanceof ApiError ? err.status : 0;
      setInvalid(t(status === 403 ? 'projects.forbidden' : status === 409 ? 'projects.conflict' : 'projects.failed'));
    } finally {
      setWorking(false);
    }
  };

  return (
    <form noValidate onSubmit={(e) => void submit(e)} className="grid gap-3">
      <div className="grid gap-1.5">
        <label htmlFor={id} className="font-semibold">
          {label}
        </label>
        <input
          id={id}
          value={name}
          aria-invalid={!!invalid}
          aria-describedby={invalid || hint ? `${id}-m` : undefined}
          onChange={(e) => setName(e.target.value)}
          className={`bg-sunk h-12 w-full rounded-row border px-3 text-base md:h-11 md:text-sm ${invalid ? 'border-err' : 'border-field'}`}
        />
        {(invalid || hint) && (
          <p id={`${id}-m`} role={invalid ? 'alert' : undefined} className={`text-[13px] ${invalid ? 'text-err' : 'text-ink3'}`}>
            {invalid ?? hint}
          </p>
        )}
      </div>
      <button type="submit" aria-busy={working} className="bg-accent text-accent-ink flex h-12 items-center justify-center gap-2 rounded-row font-bold whitespace-nowrap hover:brightness-110 md:h-11">
        {working && <span className="spinner" aria-hidden />}
        {working ? busy : idle}
      </button>
    </form>
  );
}

export function Projects() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { topbar } = useOutletContext<ShellContext>();
  const { workspaces, workspace, selectWorkspace, addWorkspace, projects, addProject, reload } = useTenancy();
  const [newWorkspace, setNewWorkspace] = useState(false);
  const { workspaceId } = useParams();

  if (workspaceId && !workspace) return <NotFound />;

  if (!workspace) {
    return (
      <>
        {topbar(<Title>{t('projects.createWorkspaceTitle')}</Title>)}
        <div className="flex-1 overflow-auto p-6">
          <div className="mx-auto grid max-w-[360px] grid-cols-[minmax(0,1fr)] gap-4 pt-[8vh]">
            <p className="text-ink2">{t('projects.createWorkspaceText')}</p>
            <NameForm
              id="workspace-name"
              label={t('projects.workspaceName')}
              idle={t('projects.createWorkspace')}
              busy={t('projects.creatingWorkspace')}
              create={async (name) => addWorkspace(await api.createWorkspace(name))}
            />
          </div>
        </div>
      </>
    );
  }

  const list = projects.status === 'ready' ? projects.projects : [];
  return (
    <>
      {topbar(<Title>{t('projects.title')}</Title>)}
      <div className="flex-1 overflow-auto p-6">
        <div className="mx-auto grid max-w-[480px] grid-cols-[minmax(0,1fr)] gap-6">
          <section aria-labelledby="workspaces" className="grid gap-3">
            <h2 id="workspaces" className="text-[17px] font-bold">
              {t('projects.workspacesTitle')}
            </h2>
            <ul className="grid grid-cols-[minmax(0,1fr)] gap-1">
              {workspaces.map((w) => {
                const current = w.id === workspace.id;
                return (
                  <li key={w.id}>
                    <button
                      type="button"
                      aria-pressed={current}
                      onClick={() => selectWorkspace(w.id)}
                      className={`flex min-h-12 w-full items-center gap-3 rounded-row px-2 text-left ${current ? 'bg-sunk' : 'hover:bg-sunk'}`}
                    >
                      <span aria-hidden className="border-field grid size-9 flex-none place-items-center rounded-row border text-xs font-bold">
                        {initials(w.name)}
                      </span>
                      <span className="min-w-0 flex-1 truncate font-semibold">{w.name}</span>
                      <span className="text-ink3 text-[13px]">{current ? t('projects.open') : t(`users.role.${w.role}`, { defaultValue: w.role })}</span>
                    </button>
                  </li>
                );
              })}
            </ul>
            {newWorkspace ? (
              <NameForm
                id="workspace-name"
                label={t('projects.workspaceName')}
                hint={t('projects.workspaceHint')}
                idle={t('projects.createWorkspace')}
                busy={t('projects.creatingWorkspace')}
                create={async (name) => {
                  addWorkspace(await api.createWorkspace(name));
                  setNewWorkspace(false);
                }}
              />
            ) : (
              <button type="button" onClick={() => setNewWorkspace(true)} className="text-hue min-h-11 justify-self-start font-semibold underline underline-offset-2">
                {t('projects.newWorkspace')}
              </button>
            )}
          </section>
          <h2 className="text-[17px] font-bold">{t('projects.inWorkspace', { name: workspace.name })}</h2>
          {projects.status === 'loading' && (
            <p role="status" className="text-ink2 flex items-center gap-2">
              <span className="spinner" aria-hidden />
              {t('projects.loading')}
            </p>
          )}
          {projects.status === 'failed' && (
            <div role="alert">
              <p className="text-ink2 mb-2">{t('projects.loadFailed')}</p>
              <button type="button" onClick={reload} className="text-hue min-h-11 font-semibold underline underline-offset-2">
                {t('common.retry')}
              </button>
            </div>
          )}
          {projects.status === 'ready' && list.length === 0 && <p className="text-ink2">{t('projects.none')}</p>}
          {list.length > 0 && (
            <ul className="grid grid-cols-[minmax(0,1fr)] gap-1">
              {list.map((p) => (
                <li key={p.id}>
                  <Link to={projectPath(workspace.id, p.id)} className="hover:bg-sunk flex min-h-12 items-center gap-3 rounded-row px-2">
                    <span aria-hidden className="bg-sunk grid size-9 flex-none place-items-center rounded-full text-xs font-bold">
                      {initials(p.name)}
                    </span>
                    <span className="min-w-0 flex-1 truncate font-semibold">{p.name}</span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
          <section aria-labelledby="new-project" className="grid gap-3">
            <h3 id="new-project" className="font-bold">
              {t('projects.createProjectTitle', { name: workspace.name })}
            </h3>
            <NameForm
              id="project-name"
              label={t('projects.projectName')}
              hint={t('projects.projectHint')}
              idle={t('projects.createProject')}
              busy={t('projects.creatingProject')}
              create={async (name) => {
                const created = await api.createProject(workspace.id, name);
                addProject(created);
                navigate(projectPath(workspace.id, created.id));
              }}
            />
          </section>
        </div>
      </div>
    </>
  );
}
