import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Link, NavLink, Outlet, useLocation, useMatch, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Activity, Bell, BookOpen, ChevronDown, Database, Hash, KeyRound, LayoutGrid, LogOut, Menu, Plug, Plus, Search, Settings, SunMoon, Users, X } from 'lucide-react';
import { api, ApiError, type Environment, type Stream } from '../lib/api';
import { useAuth, useSession } from '../lib/auth';
import { cn } from '../lib/cn';
import { projectPath, workspacePath } from '../lib/sections';
import { initials, useTenancy } from '../lib/tenancy';
import { applyTheme, currentTheme } from '../lib/theme';
import AccountMenu from './AccountMenu';
import CommandPalette, { type Command } from './CommandPalette';

type ProjectData = { status: 'loading' | 'failed' } | { status: 'ready'; environments: Environment[]; streams: Stream[] };
export type ShellContext = {
  streams: Stream[];
  environment: Environment | null;
  environments: Environment[];
  setEnvironment: (id: string) => void;
  openMenu: () => void;
  topbar: (title: ReactNode) => ReactNode;
};

const LOADING: ProjectData = { status: 'loading' };
const NO_STREAMS: Stream[] = [];
const NO_ENVIRONMENTS: Environment[] = [];
const ROW = 'flex min-h-11 w-full items-center gap-2 rounded-row px-2 text-left text-[13px] font-medium md:min-h-9';
const linkState = ({ isActive }: { isActive: boolean }) => cn(ROW, isActive ? 'bg-sunk text-hue font-semibold' : 'text-ink2 hover:bg-sunk hover:text-ink');
const FOCUSABLE = 'a[href], button:not([disabled]), select:not([disabled]), input:not([disabled]), summary, [tabindex="0"]';

export default function Shell() {
  const { t } = useTranslation();
  const { user } = useSession();
  const { logout, retry } = useAuth();
  const { projects, workspace, workspaces, selectWorkspace } = useTenancy();
  const navigate = useNavigate();
  const location = useLocation();
  const [drawer, setDrawer] = useState(false);
  const [account, setAccount] = useState(false);
  const [palette, setPalette] = useState(false);
  const [loaded, setLoaded] = useState<{ key: string; result: ProjectData } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [envs, setEnvs] = useState<Record<string, string>>({});
  const navRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLButtonElement>(null);
  const projectId = useMatch('/w/:workspaceId/p/:projectId/*')?.params.projectId ?? null;
  const list = useMemo(() => projects.status === 'ready' ? projects.projects : [], [projects]);
  const project = list.find((p) => p.id === projectId && p.workspace_id === workspace?.id) ?? null;
  const knownProjectId = project?.id ?? null;
  const canManageTokens = !!workspace && ['owner', 'admin', 'developer'].includes(workspace.role);
  const dataKey = `${workspace?.id}:${knownProjectId}:${attempt}`;
  const data = loaded?.key === dataKey ? loaded.result : LOADING;

  useEffect(() => {
    if (knownProjectId) localStorage.setItem('project', knownProjectId);
  }, [knownProjectId]);

  useEffect(() => {
    if (!knownProjectId) return;
    const ctrl = new AbortController();
    Promise.all([api.environments(knownProjectId, ctrl.signal), api.streams(knownProjectId, ctrl.signal)])
      .then(([environments, streams]) => {
        if (!ctrl.signal.aborted) setLoaded({ key: dataKey, result: { status: 'ready', environments, streams } });
      })
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) retry();
        else setLoaded({ key: dataKey, result: { status: 'failed' } });
      });
    return () => ctrl.abort();
  }, [knownProjectId, dataKey, retry]);

  const [lastPath, setLastPath] = useState(location.pathname);
  if (location.pathname !== lastPath) {
    setLastPath(location.pathname);
    setDrawer(false);
    setAccount(false);
    setPalette(false);
  }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setDrawer(false);
        setPalette((open) => !open);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  useEffect(() => {
    if (!drawer) return;
    const previous = document.activeElement;
    const panel = navRef.current;
    const menuButton = menuRef.current;
    const stops = () => Array.from(panel?.querySelectorAll<HTMLElement>(FOCUSABLE) ?? [])
      .filter((el) => !el.closest('details:not([open])') || el.tagName === 'SUMMARY');
    stops()[0]?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setDrawer(false);
      }
      if (e.key !== 'Tab') return;
      const items = stops();
      const first = items[0];
      const last = items.at(-1);
      if (e.shiftKey && (document.activeElement === first || !panel?.contains(document.activeElement))) {
        e.preventDefault();
        last?.focus();
      } else if (!e.shiftKey && (document.activeElement === last || !panel?.contains(document.activeElement))) {
        e.preventDefault();
        first?.focus();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('keydown', onKey);
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
      else menuButton?.focus();
    };
  }, [drawer]);

  const closeAccount = useCallback(() => setAccount(false), []);
  const openMenu = useCallback(() => setDrawer(true), []);
  const streams = data.status === 'ready' && knownProjectId ? data.streams : NO_STREAMS;
  const environments = data.status === 'ready' && knownProjectId ? data.environments : NO_ENVIRONMENTS;
  const envId = (knownProjectId && envs[knownProjectId]) || '';
  const environment = environments.find((e) => e.id === envId) ?? null;
  const setEnvironment = useCallback((id: string) => {
    if (knownProjectId) setEnvs((cur) => ({ ...cur, [knownProjectId]: id }));
  }, [knownProjectId]);

  const commands = useMemo<Command[]>(() => {
    const go = (to: string) => () => navigate(to);
    const out: Command[] = list.map((p) => ({ id: `p-${p.id}`, group: t('palette.projects'), label: p.name, icon: LayoutGrid, run: go(projectPath(p.workspace_id, p.id)) }));
    out.push({ id: 'all', group: t('palette.projects'), label: t('nav.allProjects'), icon: Plus, run: go(workspace ? workspacePath(workspace.id) : '/projects') });
    if (project) {
      for (const s of streams) out.push({ id: `s-${s.id}`, group: t('palette.streams'), label: s.name, icon: Hash, run: go(projectPath(project.workspace_id, project.id, `streams/${s.id}`)) });
      out.push(
        { id: 'logs', group: t('palette.sections'), label: t('shell.logs'), icon: Search, run: go(projectPath(project.workspace_id, project.id)) },
        { id: 'alerts', group: t('palette.sections'), label: t('nav.alerts'), icon: Bell, run: go(projectPath(project.workspace_id, project.id, 'alerts')) },
        { id: 'project-settings', group: t('shell.projectSettings'), label: t('shell.configuration'), icon: Settings, run: go(projectPath(project.workspace_id, project.id, 'settings')) },
      );
      if (canManageTokens) out.push({ id: 'tokens', group: t('shell.projectSettings'), label: t('nav.tokens'), icon: KeyRound, run: go(projectPath(project.workspace_id, project.id, 'tokens')) });
    }
    if (workspace) out.push(
      { id: 'workspace-settings', group: t('shell.workspaceSettings'), label: t('shell.configuration'), icon: Settings, run: go(workspacePath(workspace.id, 'settings')) },
      { id: 'members', group: t('shell.workspaceSettings'), label: t('nav.users'), icon: Users, run: go(workspacePath(workspace.id, 'members')) },
      { id: 'channels', group: t('shell.workspaceSettings'), label: t('shell.channels'), icon: Plug, run: go(workspacePath(workspace.id, 'channels')) },
    );
    if (user.is_instance_admin) out.push(
      { id: 'instance-overview', group: t('shell.instance'), label: t('instanceAdmin.health.title'), icon: Activity, run: go('/admin') },
      { id: 'instance-settings', group: t('shell.instance'), label: t('nav.settings'), icon: Settings, run: go('/admin/settings') },
      { id: 'instance-users', group: t('shell.instance'), label: t('shell.instanceUsers'), icon: Users, run: go('/admin/users') },
      { id: 'instance-tokens', group: t('shell.instance'), label: t('instanceAdmin.tokens.title'), icon: KeyRound, run: go('/admin/tokens') },
      { id: 'storage', group: t('shell.instance'), label: t('nav.storage'), icon: Database, run: go('/admin/storage') },
    );
    out.push(
      { id: 'docs', group: t('palette.actions'), label: t('nav.documentation'), icon: BookOpen, run: go('/docs') },
      { id: 'status', group: t('palette.actions'), label: t('nav.status'), icon: Activity, run: go('/status') },
      { id: 'theme', group: t('palette.actions'), label: t('theme.toggle'), icon: SunMoon, run: () => applyTheme(currentTheme() === 'dark' ? 'light' : 'dark') },
      { id: 'out', group: t('palette.actions'), label: t('account.signOut'), icon: LogOut, run: () => void logout().catch(() => undefined) },
    );
    return out;
  }, [list, workspace, project, streams, canManageTokens, user.is_instance_admin, navigate, logout, t]);

  const topbar = useCallback((title: ReactNode) => (
    <header className="border-line flex min-h-16 flex-none items-center gap-2 border-b px-3 py-2 md:px-6">
      <button ref={menuRef} type="button" aria-label={t('nav.menu')} aria-expanded={drawer} aria-controls="shell-navigation" onClick={openMenu} className="text-ink2 -ml-1 grid size-11 flex-none place-items-center rounded-row md:hidden">
        <Menu className="size-5" aria-hidden />
      </button>
      <div className="min-w-0 flex-1">
        {workspace && <p className="text-ink2 flex min-w-0 gap-1 text-xs"><span className="truncate">{workspace.name}{project ? ` / ${project.name}` : ''}</span><span className="flex-none">· {t(`users.role.${workspace.role}`, { defaultValue: workspace.role })}</span></p>}
        {title}
      </div>
      <button type="button" onClick={() => setPalette(true)} className="bg-sunk border-line text-ink2 hover:text-ink flex h-11 min-w-11 flex-none items-center justify-center gap-2 rounded-row border px-3 whitespace-nowrap md:h-9">
        <Search className="size-4" aria-hidden />
        <span className="max-[560px]:sr-only">{t('shell.goTo')}</span>
        <kbd className="text-ink3 hidden text-xs md:inline" aria-hidden>{/mac/i.test(navigator.platform) ? '⌘K' : 'Ctrl K'}</kbd>
      </button>
    </header>
  ), [t, drawer, openMenu, workspace, project]);

  return (
    <div className="flex h-dvh overflow-hidden" style={{ ['--rail-now' as string]: '220px' }}>
      {drawer && <div className="fixed inset-0 z-40 bg-black/60 md:hidden" onClick={() => setDrawer(false)} aria-hidden />}
      <div id="shell-navigation" ref={navRef} role={drawer ? 'dialog' : undefined} aria-modal={drawer || undefined} aria-label={drawer ? t('nav.primary') : undefined} className={cn('bg-surface border-line z-50 flex h-full w-[220px] flex-none flex-col border-r max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:w-[min(300px,88vw)]', !drawer && 'max-md:invisible max-md:-translate-x-full')}>
        <nav aria-label={t('nav.primary')} className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-3">
          <div className="flex items-center gap-2">
            <Link to="/" className="flex min-h-11 min-w-0 flex-1 items-center gap-2 px-2 font-bold">
              <span className="bg-accent text-accent-ink grid size-8 flex-none place-items-center rounded-row text-xs" aria-hidden>EL</span>
              <span className="truncate">{t('app.name')}</span>
            </Link>
            <button type="button" aria-label={t('nav.closeMenu')} onClick={() => setDrawer(false)} className="text-ink2 grid size-11 flex-none place-items-center md:hidden"><X className="size-5" aria-hidden /></button>
          </div>
          <div className="border-line grid gap-1 border-y px-2 py-3">
            <label htmlFor="shell-workspace" className="text-ink2 text-xs font-semibold">{t('shell.workspace')}</label>
            <select id="shell-workspace" value={workspace?.id ?? ''} onChange={(e) => selectWorkspace(e.target.value)} className="text-ink bg-sunk border-field min-h-11 w-full min-w-0 rounded-row border px-2 font-semibold md:min-h-9">
              {!workspace && <option value="">{t('projects.workspacesTitle')}</option>}
              {workspaces.map((w) => <option key={w.id} value={w.id}>{w.name}</option>)}
            </select>
            {workspace && <p className="text-ink2 text-xs">{t(`users.role.${workspace.role}`, { defaultValue: workspace.role })}</p>}
          </div>
          <div className="grid gap-1">
            <p className="text-ink2 px-2 text-xs font-semibold">{t('shell.projects')}</p>
            {list.map((p) => <NavLink key={p.id} to={projectPath(p.workspace_id, p.id)} end aria-label={p.name} className={({ isActive }) => cn(ROW, p.id === knownProjectId || isActive ? 'text-ink font-semibold' : 'text-ink2 hover:bg-sunk')}>
              <span aria-hidden className={cn('grid size-7 flex-none place-items-center rounded-row text-[11px] font-bold', p.id === knownProjectId ? 'bg-accent text-accent-ink' : 'border-line border')}>{initials(p.name)}</span>
              <span className="min-w-0 flex-1 truncate" title={p.name}>{p.name}</span>
            </NavLink>)}
            <NavLink to={workspace ? workspacePath(workspace.id) : '/projects'} end className={linkState}><Plus className="size-4 flex-none" aria-hidden /><span className="truncate">{t('nav.allProjects')}</span></NavLink>
          </div>
          {project && <div className="grid gap-1">
            <p className="text-ink2 truncate px-2 text-xs font-semibold" title={project.name}>{t('shell.project')} · <span className="text-ink">{project.name}</span></p>
            <NavLink to={projectPath(project.workspace_id, project.id)} end className={linkState}><Search className="size-4" aria-hidden />{t('shell.logs')}</NavLink>
            <NavLink to={projectPath(project.workspace_id, project.id, 'alerts')} className={linkState}><Bell className="size-4" aria-hidden />{t('nav.alerts')}</NavLink>
            {environments.length > 0 && <label className="text-ink2 grid gap-1 px-2 py-2 text-xs">
              {t('nav.environment')}
              <select value={envId} onChange={(e) => setEnvironment(e.target.value)} className="bg-sunk text-ink border-field min-h-11 w-full min-w-0 rounded-row border px-2 text-[13px] md:min-h-9">
                <option value="">{t('nav.allEnvironments')}</option>
                {environments.map((env) => <option key={env.id} value={env.id}>{env.name}</option>)}
              </select>
            </label>}
            {data.status === 'failed' && <div role="alert" className="text-ink2 px-2 text-xs"><p>{t('nav.loadFailed')}</p><button type="button" onClick={() => setAttempt((n) => n + 1)} className="text-hue min-h-11 underline md:min-h-9">{t('common.retry')}</button></div>}
            <details className="group" open={/\/(tokens|settings)$/.test(location.pathname) || undefined}>
              <summary className={cn(ROW, 'text-ink2 cursor-pointer list-none hover:bg-sunk')}><Settings className="size-4" aria-hidden /><span className="min-w-0 flex-1">{t('shell.projectSettings')}</span><ChevronDown className="size-3 group-open:rotate-180" aria-hidden /></summary>
              <NavLink to={projectPath(project.workspace_id, project.id, 'settings')} className={(state) => cn(linkState(state), 'pl-8')}><Settings className="size-4" aria-hidden />{t('shell.configuration')}</NavLink>
              {canManageTokens && <NavLink to={projectPath(project.workspace_id, project.id, 'tokens')} className={(state) => cn(linkState(state), 'pl-8')}><KeyRound className="size-4" aria-hidden />{t('nav.tokens')}</NavLink>}
            </details>
          </div>}
          {workspace && <details className="group" open={!project && /\/(members|channels|settings)$/.test(location.pathname) || undefined}>
            <summary className={cn(ROW, 'text-ink2 cursor-pointer list-none hover:bg-sunk')}><Settings className="size-4" aria-hidden /><span className="min-w-0 flex-1">{t('shell.workspaceSettings')}</span><ChevronDown className="size-3 group-open:rotate-180" aria-hidden /></summary>
            <NavLink to={workspacePath(workspace.id, 'settings')} className={(state) => cn(linkState(state), 'pl-8')}><Settings className="size-4" aria-hidden />{t('shell.configuration')}</NavLink>
            <NavLink to={workspacePath(workspace.id, 'members')} className={(state) => cn(linkState(state), 'pl-8')}><Users className="size-4 flex-none" aria-hidden /><span>{t('nav.users')}</span></NavLink>
            <NavLink to={workspacePath(workspace.id, 'channels')} className={(state) => cn(linkState(state), 'pl-8')}><Plug className="size-4 flex-none" aria-hidden /><span>{t('shell.channels')}</span></NavLink>
          </details>}
          {user.is_instance_admin && <div className="grid gap-1"><p className="text-ink2 px-2 text-xs font-semibold">{t('shell.instance')}</p>
            <NavLink to="/admin" end className={linkState}><Activity className="size-4" aria-hidden />{t('instanceAdmin.health.title')}</NavLink>
            <NavLink to="/admin/settings" className={linkState}><Settings className="size-4" aria-hidden />{t('nav.settings')}</NavLink>
            <NavLink to="/admin/users" className={linkState}><Users className="size-4" aria-hidden />{t('shell.instanceUsers')}</NavLink>
            <NavLink to="/admin/tokens" className={linkState}><KeyRound className="size-4" aria-hidden />{t('instanceAdmin.tokens.title')}</NavLink>
            <NavLink to="/admin/storage" className={linkState}><Database className="size-4" aria-hidden />{t('nav.storage')}</NavLink>
          </div>}
          <div className="border-line mt-auto grid gap-1 border-t pt-3">
            <NavLink to="/docs" className={linkState}><BookOpen className="size-4" aria-hidden />{t('nav.documentation')}</NavLink>
            <button type="button" data-account-button aria-haspopup="true" aria-expanded={account} aria-label={t('account.open')} onClick={() => { setDrawer(false); setAccount((open) => !open); }} className={cn(ROW, 'text-ink2 hover:bg-sunk')}>
              <span aria-hidden className="bg-raised grid size-7 flex-none place-items-center rounded-row text-xs font-bold">{initials(user.username)}</span><span className="min-w-0 flex-1 truncate">{user.username}</span>
            </button>
          </div>
        </nav>
      </div>
      <main inert={drawer || undefined} className="bg-bg flex min-w-0 flex-1 flex-col overflow-hidden">
        <Outlet context={{ streams, environment, environments, setEnvironment, openMenu, topbar } satisfies ShellContext} />
      </main>
      {account && <AccountMenu onClose={closeAccount} onNavigate={closeAccount} />}
      {palette && <CommandPalette commands={commands} onClose={() => setPalette(false)} />}
    </div>
  );
}
