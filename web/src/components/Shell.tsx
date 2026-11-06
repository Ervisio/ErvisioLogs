import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { BookOpen, ChevronDown, Hash, LayoutGrid, Menu, Plus, Search, Settings, Shield, User, X } from 'lucide-react';
import { api, ApiError, type Environment, type Stream } from '../lib/api';
import { useAuth, useSession } from '../lib/auth';
import { cn } from '../lib/cn';
import { SECTIONS, sectionPath } from '../lib/sections';
import { projectPath, workspacePath } from '../lib/paths';
import { initials, useTenancy } from '../lib/tenancy';
import AccountMenu from './AccountMenu';
import CommandPalette, { type Command } from './CommandPalette';

type ProjectData =
  { status: 'loading' | 'failed' } | { status: 'ready'; environments: Environment[]; streams: Stream[] };
export type ShellContext = {
  streams: Stream[];
  environment: Environment | null;
  environments: Environment[];
  setEnvironment: (id: string) => void;
  openMenu: () => void;
  topbar: (title: ReactNode) => ReactNode;
};
const ROW = 'flex min-h-11 items-center gap-2 rounded-row px-3 text-sm md:min-h-9';
const EMPTY: Stream[] = [];
const EMPTY_ENV: Environment[] = [];

export default function Shell() {
  const { t } = useTranslation();
  const { user } = useSession();
  const { retry } = useAuth();
  const { projects, workspace } = useTenancy();
  const navigate = useNavigate();
  const location = useLocation();
  const [drawer, setDrawer] = useState(false);
  const [account, setAccount] = useState(false);
  const [palette, setPalette] = useState(false);
  const [loaded, setLoaded] = useState<{ key: string; result: ProjectData } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [envs, setEnvs] = useState<Record<string, string>>({});
  const projectId =
    location.pathname.match(/\/p\/([^/]+)/)?.[1] ?? location.pathname.match(/^\/projects\/([^/]+)/)?.[1];
  const list = useMemo(() => (projects.status === 'ready' ? projects.projects : []), [projects]);
  const project = list.find((p) => p.id === projectId);
  const knownId = project?.id;
  const dataKey = `${knownId}:${attempt}`;
  const data: ProjectData = loaded?.key === dataKey ? loaded.result : { status: 'loading' };
  useEffect(() => {
    if (!knownId) return;
    localStorage.setItem('project', knownId);
    const ctrl = new AbortController();
    Promise.all([api.environments(knownId, ctrl.signal), api.streams(knownId, ctrl.signal)])
      .then(([environments, streams]) =>
        setLoaded({ key: dataKey, result: { status: 'ready', environments, streams } }),
      )
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) retry();
        else setLoaded({ key: dataKey, result: { status: 'failed' } });
      });
    return () => ctrl.abort();
  }, [knownId, dataKey, retry]);
  const [lastPath, setLastPath] = useState(location.pathname);
  if (lastPath !== location.pathname) {
    setLastPath(location.pathname);
    setDrawer(false);
  }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPalette((v) => !v);
      }
      if (e.key === 'Escape') setDrawer(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
  const streams = knownId && data.status === 'ready' ? data.streams : EMPTY;
  const environments = knownId && data.status === 'ready' ? data.environments : EMPTY_ENV;
  const environment = environments.find((env) => env.id === (knownId ? envs[knownId] : '')) ?? null;
  const setEnvironment = useCallback(
    (id: string) => {
      if (knownId) setEnvs((v) => ({ ...v, [knownId]: id }));
    },
    [knownId],
  );
  const openMenu = useCallback(() => setDrawer(true), []);
  const closeAccount = useCallback(() => setAccount(false), []);
  const topbar = useCallback(
    (title: ReactNode) => (
      <header className="border-line flex min-h-14 flex-none items-center gap-2 border-b px-3 md:px-5">
        <button
          type="button"
          aria-label={t('nav.menu')}
          aria-expanded={drawer}
          onClick={openMenu}
          className="grid size-11 flex-none place-items-center rounded-row md:hidden"
        >
          <Menu className="size-5" />
        </button>
        <div className="min-w-0 flex-1">{title}</div>
        <button
          type="button"
          aria-label={t('palette.open')}
          onClick={() => setPalette(true)}
          className="text-ink3 grid size-11 flex-none place-items-center rounded-row hover:bg-sunk"
        >
          <Search className="size-4" />
        </button>
      </header>
    ),
    [t, drawer, openMenu],
  );
  const commands = useMemo<Command[]>(() => {
    const go = (path: string) => () => navigate(path);
    const out: Command[] = list.map((p) => ({
      id: p.id,
      group: t('palette.projects'),
      label: p.name,
      icon: LayoutGrid,
      run: go(projectPath(p.workspace_id, p.id)),
    }));
    if (project)
      for (const section of SECTIONS.filter(
        (section) => section.key !== 'tokens' || ['owner', 'admin', 'developer'].includes(workspace?.role ?? ''),
      ))
        out.push({
          id: `s-${section.key}`,
          group: project.name,
          label: t(`nav.${section.key}`),
          icon: section.icon,
          run: go(sectionPath(project.id, section.key, project.workspace_id)),
        });
    if (project)
      for (const tab of ['general', 'environments', 'streams', 'tokens', 'catalog', 'retention'].filter(
        (tab) => tab !== 'tokens' || ['owner', 'admin', 'developer'].includes(workspace?.role ?? ''),
      ))
        out.push({
          id: `settings-${tab}`,
          group: t('manage.projectSettings'),
          label: t(`manage.${tab}`),
          icon: Settings,
          run: go(projectPath(project.workspace_id, project.id, `settings/${tab}`)),
        });
    if (workspace)
      out.push({
        id: 'workspace',
        group: t('palette.sections'),
        label: t('manage.workspaceSettings'),
        icon: Settings,
        run: go(workspacePath(workspace.id)),
      });
    if (user.is_instance_admin)
      out.push({
        id: 'admin',
        group: t('palette.sections'),
        label: t('manage.administration'),
        icon: Shield,
        run: go('/admin'),
      });
    out.push({
      id: 'docs',
      group: t('palette.actions'),
      label: t('nav.documentation'),
      icon: BookOpen,
      run: go('/docs'),
    });
    out.push({
      id: 'account',
      group: t('palette.sections'),
      label: t('manage.account'),
      icon: User,
      run: go('/account'),
    });
    return out;
  }, [list, project, workspace, user.is_instance_admin, navigate, t]);
  const tile = 'grid size-11 flex-none place-items-center rounded-[15px] font-bold transition-colors';
  const activeRow = ({ isActive }: { isActive: boolean }) =>
    cn(ROW, isActive ? 'bg-accent-soft text-hue font-semibold' : 'text-ink2 hover:bg-sunk');
  return (
    <div
      className="app-shell relative flex h-dvh gap-2 overflow-hidden p-2"
      style={{ ['--rail-now' as string]: '64px' }}
    >
      <div className="ambient" aria-hidden>
        {Array.from({ length: 6 }, (_, i) => (
          <i key={i} />
        ))}
      </div>
      {drawer && (
        <div className="fixed inset-0 z-40 bg-black/60 md:hidden" onClick={() => setDrawer(false)} aria-hidden />
      )}
      <div
        className={cn(
          'z-50 flex h-full flex-none gap-2 max-md:fixed max-md:inset-y-2 max-md:left-2 max-md:max-w-[94vw]',
          !drawer && 'max-md:invisible max-md:-translate-x-full',
        )}
      >
        <nav
          aria-label={t('nav.primary')}
          className="shell-rail flex w-16 flex-none flex-col items-center gap-2 overflow-y-auto rounded-card py-2"
        >
          <Link
            to="/projects"
            aria-label={t('manage.switchWorkspace')}
            title={workspace?.name ?? t('manage.switchWorkspace')}
            className={cn(tile, 'bg-accent-soft text-hue border-line mb-1 border')}
          >
            {workspace ? initials(workspace.name) : 'E'}
          </Link>
          <div className="bg-line mb-1 h-px w-8" />
          {list.map((p) => (
            <Link
              key={p.id}
              to={projectPath(p.workspace_id, p.id)}
              aria-label={p.name}
              title={p.name}
              aria-current={p.id === projectId ? 'page' : undefined}
              className={cn(
                tile,
                p.id === projectId ? 'bg-accent text-accent-ink' : 'bg-sunk text-ink2 hover:bg-raised',
              )}
            >
              {initials(p.name)}
            </Link>
          ))}
          <Link
            to="/projects"
            aria-label={t('nav.allProjects')}
            title={t('nav.allProjects')}
            className={cn(tile, 'text-ink2 hover:bg-sunk')}
          >
            <Plus className="size-5" />
          </Link>
          <div className="min-h-2 flex-1" />
          {user.is_instance_admin && (
            <Link
              to="/admin"
              aria-label={t('manage.administration')}
              title={t('manage.administration')}
              className={cn(tile, 'text-ink2 hover:bg-sunk')}
            >
              <Shield className="size-5" />
            </Link>
          )}
          <Link
            to="/docs"
            aria-label={t('nav.documentation')}
            title={t('nav.documentation')}
            className={cn(tile, 'text-ink2 hover:bg-sunk')}
          >
            <BookOpen className="size-5" />
          </Link>
          <button
            type="button"
            data-account-button
            aria-label={t('account.open')}
            aria-expanded={account}
            onClick={() => setAccount((v) => !v)}
            className={cn(tile, 'bg-raised rounded-full text-xs')}
          >
            {initials(user.username)}
          </button>
        </nav>
        <aside
          aria-label={t('nav.sections')}
          className="shell-side flex w-[216px] min-w-0 flex-col rounded-card p-2 max-md:w-[min(216px,calc(94vw-80px))]"
        >
          <div className="mb-3 flex min-h-12 items-center gap-2 px-2">
            <div className="min-w-0 flex-1">
              <p className="truncate font-semibold">{project?.name ?? workspace?.name ?? t('app.name')}</p>
              <p className="text-ink3 truncate text-xs">
                {location.pathname.startsWith('/admin')
                  ? t('manage.instance')
                  : workspace
                    ? t(`users.role.${workspace.role}`, { defaultValue: workspace.role })
                    : user.username}
              </p>
            </div>
            {project && (
              <Link
                to={projectPath(project.workspace_id, project.id, 'settings')}
                aria-label={t('nav.settings')}
                className="text-ink2 grid size-11 flex-none place-items-center rounded-row hover:bg-sunk"
              >
                <Settings className="size-4" />
              </Link>
            )}
            <button
              type="button"
              aria-label={t('nav.closeMenu')}
              onClick={() => setDrawer(false)}
              className="grid size-11 place-items-center md:hidden"
            >
              <X className="size-4" />
            </button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            {project ? (
              <>
                {SECTIONS.filter((s) => s.group === 0).map((s) => (
                  <NavLink
                    key={s.key}
                    end
                    to={sectionPath(project.id, s.key, project.workspace_id)}
                    className={activeRow}
                  >
                    <s.icon className="size-4" />
                    {t(`nav.${s.key}`)}
                  </NavLink>
                ))}
                <p className="text-ink3 mt-5 mb-1 px-3 text-xs font-semibold">{t('nav.streams')}</p>
                {data.status === 'failed' && (
                  <button type="button" onClick={() => setAttempt((v) => v + 1)} className={ROW}>
                    {t('common.retry')}
                  </button>
                )}
                {streams.map((stream) => (
                  <NavLink
                    key={stream.id}
                    to={projectPath(project.workspace_id, project.id, `streams/${stream.id}`)}
                    className={activeRow}
                  >
                    <Hash className="size-4 flex-none" />
                    <span className="truncate">{stream.display_name || stream.name}</span>
                  </NavLink>
                ))}
              </>
            ) : (
              <>
                <NavLink to="/projects" className={activeRow}>
                  <LayoutGrid className="size-4" />
                  {t('nav.allProjects')}
                </NavLink>
                {location.pathname.match(/^\/w\/[^/]+\/settings/) &&
                  workspace &&
                  [
                    'general',
                    ...(['owner', 'admin'].includes(workspace.role) ? ['members'] : []),
                    ...(['owner', 'admin', 'developer'].includes(workspace.role) ? ['channels'] : []),
                    'retention',
                    ...(['owner', 'admin'].includes(workspace.role) ? ['audit'] : []),
                  ].map((key) => (
                    <NavLink key={key} to={workspacePath(workspace.id, key)} className={activeRow}>
                      {t(`manage.${key}`)}
                    </NavLink>
                  ))}
                {location.pathname.startsWith('/admin') &&
                  ['overview', 'users', 'workspaces', 'settings', 'archive', 'tokens', 'audit', 'system-alerts'].map(
                    (key) => (
                      <NavLink key={key} end to={key === 'overview' ? '/admin' : `/admin/${key}`} className={activeRow}>
                        {t(`manage.${key}`)}
                      </NavLink>
                    ),
                  )}
              </>
            )}
          </div>
          {workspace && (
            <div className="border-line mt-3 border-t pt-2">
              <Link to={workspacePath(workspace.id)} className={ROW}>
                <ChevronDown className="size-4" />
                {t('manage.workspaceSettings')}
              </Link>
            </div>
          )}
        </aside>
      </div>
      <main className="shell-main relative z-10 flex min-w-0 flex-1 flex-col overflow-hidden rounded-card">
        <Outlet
          context={{ streams, environments, environment, setEnvironment, openMenu, topbar } satisfies ShellContext}
        />
      </main>
      {account && <AccountMenu onClose={closeAccount} onNavigate={closeAccount} />}
      {palette && <CommandPalette commands={commands} onClose={() => setPalette(false)} />}
    </div>
  );
}
