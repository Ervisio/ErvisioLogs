import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { Link, NavLink, Outlet, useLocation, useMatch, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Activity, BookOpen, ChevronDown, ChevronsLeft, ChevronsRight, Hash, LayoutGrid, LogOut, Menu, Plus, Search, SunMoon, X } from 'lucide-react';
import { api, ApiError, type Environment, type Stream } from '../lib/api';
import { useAuth, useSession } from '../lib/auth';
import { cn } from '../lib/cn';
import { SECTIONS, sectionPath } from '../lib/sections';
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

type Tip = { text: string; top: number; left: number } | null;

const LOADING: ProjectData = { status: 'loading' };
const NO_STREAMS: Stream[] = [];
const NO_ENVIRONMENTS: Environment[] = [];

const SECTION_ROW = 'flex h-11 w-full items-center gap-2 rounded-row px-2 text-left font-medium whitespace-nowrap md:h-[34px]';
const sectionState = (active: boolean) => (active ? 'bg-raised text-ink font-semibold' : 'text-ink2 hover:bg-sunk hover:text-ink');

export default function Shell() {
  const { t } = useTranslation();
  const { user } = useSession();
  const { logout, retry } = useAuth();
  const { projects } = useTenancy();
  const navigate = useNavigate();
  const location = useLocation();

  const [wide, setWide] = useState(() => localStorage.getItem('rail') === 'wide');
  const [drawer, setDrawer] = useState(false);
  const [account, setAccount] = useState(false);
  const [palette, setPalette] = useState(false);
  const [streamsOpen, setStreamsOpen] = useState(true);
  const [tip, setTip] = useState<Tip>(null);
  const [loaded, setLoaded] = useState<{ key: string; result: ProjectData } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [envs, setEnvs] = useState<Record<string, string>>({});

  const projectId = useMatch('/projects/:projectId/*')?.params.projectId ?? null;
  const list = useMemo(() => (projects.status === 'ready' ? projects.projects : []), [projects]);
  const project = list.find((p) => p.id === projectId) ?? null;
  const knownProjectId = project?.id ?? null;

  useEffect(() => {
    if (knownProjectId) localStorage.setItem('project', knownProjectId);
  }, [knownProjectId]);

  // A result counts only for the project and attempt that requested it.
  const dataKey = `${knownProjectId}:${attempt}`;
  const data: ProjectData = loaded?.key === dataKey ? loaded.result : LOADING;

  useEffect(() => {
    if (!knownProjectId) return;
    const ctrl = new AbortController();
    Promise.all([api.environments(knownProjectId, ctrl.signal), api.streams(knownProjectId, ctrl.signal)])
      .then(([environments, streams]) => setLoaded({ key: dataKey, result: { status: 'ready', environments, streams } }))
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) retry();
        else setLoaded({ key: dataKey, result: { status: 'failed' } });
      });
    return () => ctrl.abort();
  }, [knownProjectId, dataKey, retry]);

  // The drawer and its sheets close when the page changes.
  const [lastPath, setLastPath] = useState(location.pathname);
  if (location.pathname !== lastPath) {
    setLastPath(location.pathname);
    setDrawer(false);
    setAccount(false);
    setTip(null);
  }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPalette((open) => !open);
      } else if (e.key === 'Escape') {
        setDrawer(false);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const toggleRail = () => {
    setTip(null);
    setWide((w) => {
      localStorage.setItem('rail', w ? 'narrow' : 'wide');
      return !w;
    });
  };
  const closeAccount = useCallback(() => setAccount(false), []);
  const openMenu = useCallback(() => setDrawer(true), []);

  const showTip = (text: string) => (e: { currentTarget: HTMLElement }) => {
    if (wide) return;
    const box = e.currentTarget.getBoundingClientRect();
    setTip({ text, top: box.top + box.height / 2, left: box.right + 6 });
  };
  const tipProps = (text: string) => ({
    onMouseEnter: showTip(text),
    onFocus: showTip(text),
    onMouseLeave: () => setTip(null),
    onBlur: () => setTip(null),
  });

  const streams = data.status === 'ready' && knownProjectId ? data.streams : NO_STREAMS;
  const environments = data.status === 'ready' && knownProjectId ? data.environments : NO_ENVIRONMENTS;
  // Empty means every environment: events may arrive without one.
  const envId = (knownProjectId && envs[knownProjectId]) || '';
  const environment = environments.find((e) => e.id === envId) ?? null;
  const setEnvironment = useCallback(
    (id: string) => {
      if (knownProjectId) setEnvs((cur) => ({ ...cur, [knownProjectId]: id }));
    },
    [knownProjectId],
  );
  const sections = useMemo(() => SECTIONS.filter((s) => !s.adminOnly || user.is_instance_admin), [user.is_instance_admin]);

  const commands = useMemo<Command[]>(() => {
    const go = (to: string) => () => navigate(to);
    const out: Command[] = list.map((p) => ({ id: `p-${p.id}`, group: t('palette.projects'), label: p.name, icon: LayoutGrid, run: go(`/projects/${p.id}`) }));
    out.push({ id: 'all', group: t('palette.projects'), label: t('nav.allProjects'), icon: Plus, run: go('/projects') });
    if (knownProjectId) {
      for (const s of streams) out.push({ id: `s-${s.id}`, group: t('palette.streams'), label: s.name, icon: Hash, run: go(`/projects/${knownProjectId}/streams/${s.id}`) });
      for (const s of sections) out.push({ id: `n-${s.key}`, group: t('palette.sections'), label: t(`nav.${s.key}`), icon: s.icon, run: go(sectionPath(knownProjectId, s.key)) });
    }
    out.push(
      { id: 'docs', group: t('palette.actions'), label: t('nav.documentation'), icon: BookOpen, run: go('/docs') },
      { id: 'status', group: t('palette.actions'), label: t('nav.status'), icon: Activity, run: go('/status') },
      { id: 'theme', group: t('palette.actions'), label: t('theme.toggle'), icon: SunMoon, run: () => applyTheme(currentTheme() === 'dark' ? 'light' : 'dark') },
      { id: 'out', group: t('palette.actions'), label: t('account.signOut'), icon: LogOut, run: () => void logout().catch(() => undefined) },
    );
    return out;
  }, [list, streams, sections, knownProjectId, navigate, logout, t]);

  const topbar = useCallback(
    (title: ReactNode) => (
      <header className="border-line flex min-h-[52px] flex-none items-center gap-2 border-b px-3 md:px-4">
        <button type="button" aria-label={t('nav.menu')} aria-expanded={drawer} onClick={openMenu} className="text-ink2 -ml-1 grid size-11 flex-none place-items-center rounded-row md:hidden">
          <Menu className="size-5" aria-hidden />
        </button>
        <div className="min-w-0 flex-1">{title}</div>
        <button type="button" onClick={() => setPalette(true)} className="bg-sunk border-raised text-ink2 hover:text-ink flex h-11 min-w-11 flex-none items-center justify-center gap-2 rounded-row border px-3 whitespace-nowrap md:h-9">
          <Search className="size-4" aria-hidden />
          <span className="max-[560px]:sr-only">{t('palette.open')}</span>
          <kbd className="text-ink3 hidden font-sans text-xs md:inline" aria-hidden>
            {/mac/i.test(navigator.platform) ? '⌘K' : 'Ctrl K'}
          </kbd>
        </button>
      </header>
    ),
    [t, drawer, openMenu],
  );

  const railRow = 'relative flex w-full flex-none items-center px-4 text-left';
  const railWidth = wide ? 220 : 76;

  return (
    <div className="flex h-dvh overflow-hidden" style={{ ['--rail-now' as string]: `${railWidth}px` }}>
      {drawer && <div className="fixed inset-0 z-40 bg-black/60 md:hidden" onClick={() => setDrawer(false)} aria-hidden />}

      <div className={cn('z-50 flex h-full flex-none', 'max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:max-w-[92vw] max-md:transition-transform', drawer ? 'max-md:translate-x-0' : 'max-md:invisible max-md:-translate-x-full')}>
        <nav aria-label={t('nav.primary')} className={cn('bg-bg flex w-[72px] flex-col gap-1 overflow-x-hidden overflow-y-auto pt-3 md:transition-[width] md:duration-150', wide ? 'md:w-[220px]' : 'md:w-[76px]')}>
          <Link to="/" aria-label={t('app.name')} {...tipProps(t('app.name'))} className={cn(railRow, 'h-11')}>
            <span aria-hidden className="bg-sunk text-hue grid size-11 flex-none place-items-center rounded-[14px] text-base font-extrabold">
              E
            </span>
            {wide && <span className="ml-3 font-bold whitespace-nowrap max-md:hidden">{t('app.name')}</span>}
          </Link>
          <div className="bg-raised mx-5 my-2 h-px flex-none" />

          {list.map((p) => {
            const current = p.id === projectId;
            return (
              <Link key={p.id} to={`/projects/${p.id}`} aria-current={current ? 'true' : undefined} aria-label={p.name} {...tipProps(p.name)} className={cn(railRow, 'group h-[52px]')}>
                <span aria-hidden className={cn('bg-ink absolute top-1/2 left-0 w-1 -translate-y-1/2 rounded-r transition-[height]', current ? 'bg-accent h-8' : 'h-0 group-hover:h-3.5')} />
                <span aria-hidden className={cn('grid size-11 flex-none place-items-center text-sm font-bold transition-[border-radius]', current ? 'bg-accent text-accent-ink rounded-[14px]' : 'bg-sunk group-hover:bg-raised rounded-full group-hover:rounded-[14px]')}>
                  {initials(p.name)}
                </span>
                {wide && <span className="ml-3 min-w-0 flex-1 truncate font-semibold max-md:hidden">{p.name}</span>}
              </Link>
            );
          })}

          <NavLink to="/projects" end aria-label={t('nav.allProjects')} {...tipProps(t('nav.allProjects'))} className={cn(railRow, 'group h-[52px]')}>
            {({ isActive }) => (
              <>
                <span aria-hidden className={cn('border-raised text-ink2 grid size-11 flex-none place-items-center rounded-full border', isActive && 'border-accent text-hue rounded-[14px]')}>
                  <Plus className="size-5" />
                </span>
                {wide && <span className="text-ink2 ml-3 font-medium whitespace-nowrap max-md:hidden">{t('nav.allProjects')}</span>}
              </>
            )}
          </NavLink>

          <div className="min-h-2 flex-1" />

          <NavLink to="/docs" aria-label={t('nav.documentation')} {...tipProps(t('nav.documentation'))} className={({ isActive }) => cn(railRow, 'min-h-11', isActive ? 'text-hue' : 'text-ink2 hover:text-ink')}>
            <span className="grid w-11 flex-none place-items-center"><BookOpen className="size-5" aria-hidden /></span>
            {wide && <span className="ml-3 whitespace-nowrap max-md:hidden">{t('nav.documentation')}</span>}
          </NavLink>

          <button type="button" onClick={toggleRail} aria-expanded={wide} aria-label={wide ? t('nav.collapse') : t('nav.expand')} {...tipProps(t('nav.expand'))} className={cn(railRow, 'text-ink2 hover:text-ink h-11 max-md:hidden')}>
            <span className="grid w-11 flex-none place-items-center">{wide ? <ChevronsLeft className="size-5" aria-hidden /> : <ChevronsRight className="size-5" aria-hidden />}</span>
            {wide && <span className="ml-3 whitespace-nowrap">{t('nav.collapse')}</span>}
          </button>
          <button type="button" data-account-button aria-haspopup="true" aria-expanded={account} aria-label={t('account.open')} {...tipProps(user.username)} onClick={() => setAccount((a) => !a)} className={cn(railRow, 'mb-2 h-14 md:h-12')}>
            <span aria-hidden className="bg-raised grid size-11 flex-none place-items-center rounded-full text-[13px] font-bold">
              {initials(user.username)}
            </span>
            {wide && <span className="ml-3 min-w-0 flex-1 truncate font-semibold max-md:hidden">{user.username}</span>}
          </button>
        </nav>

        {knownProjectId && project && (
          <aside aria-label={t('nav.sections')} className="bg-surface flex w-[min(268px,calc(92vw-72px))] flex-col md:w-[232px]">
            <div className="flex-none p-2">
              <div className="flex items-center gap-1">
                <p className="h-11 min-w-0 flex-1 truncate px-2 text-base leading-11 font-bold md:h-8 md:leading-8">{project.name}</p>
                <button type="button" aria-label={t('nav.closeMenu')} onClick={() => setDrawer(false)} className="text-ink2 grid size-11 flex-none place-items-center md:hidden">
                  <X className="size-5" aria-hidden />
                </button>
              </div>
              {environments.length > 0 && (
                <label className="text-ink2 mt-1 flex items-center justify-between gap-2 px-2 text-[13px]">
                  {t('nav.environment')}
                  <select value={envId} onChange={(e) => setEnvironment(e.target.value)} className="bg-sunk text-ink border-field h-11 max-w-[130px] truncate rounded-row border px-1.5 md:h-8">
                    <option value="">{t('nav.allEnvironments')}</option>
                    {environments.map((env) => (
                      <option key={env.id} value={env.id}>
                        {env.name}
                      </option>
                    ))}
                  </select>
                </label>
              )}
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
              {[0, 1].map((group) => (
                <ul key={group} className="border-line mb-2 grid grid-cols-[minmax(0,1fr)] gap-0.5 pb-2 not-last:border-b">
                  {sections.filter((s) => s.group === group).map((s) => (
                    <li key={s.key}>
                      <NavLink to={sectionPath(knownProjectId, s.key)} end className={({ isActive }) => cn(SECTION_ROW, sectionState(isActive), s.parent && 'pl-8')}>
                        {({ isActive }) => (
                          <>
                            <s.icon className={cn('size-4 flex-none', isActive && 'text-hue')} aria-hidden />
                            {t(`nav.${s.key}`)}
                          </>
                        )}
                      </NavLink>
                    </li>
                  ))}
                </ul>
              ))}

              <button type="button" aria-expanded={streamsOpen} aria-label={t('nav.toggleStreams')} onClick={() => setStreamsOpen((o) => !o)} className="text-ink2 hover:text-ink flex h-11 w-full items-center gap-1 px-2 text-[13px] font-semibold md:h-7">
                <ChevronDown className={cn('size-3.5 transition-transform', !streamsOpen && '-rotate-90')} aria-hidden />
                {t('nav.streams')}
              </button>
              {data.status === 'failed' && (
                <div role="alert" className="text-ink2 px-2 py-1 text-[13px]">
                  <p>{t('nav.loadFailed')}</p>
                  <button type="button" onClick={() => setAttempt((n) => n + 1)} className="text-hue min-h-11 font-semibold underline underline-offset-2 md:min-h-0">
                    {t('common.retry')}
                  </button>
                </div>
              )}
              {data.status === 'loading' && <div aria-hidden className="bg-sunk mx-2 mt-1 h-5 animate-pulse rounded" />}
              {streamsOpen && (
                <ul className="grid grid-cols-[minmax(0,1fr)] gap-0.5">
                  {streams.map((s) => (
                    <li key={s.id}>
                      <NavLink to={`/projects/${knownProjectId}/streams/${s.id}`} className={({ isActive }) => cn(SECTION_ROW, sectionState(isActive))}>
                        {({ isActive }) => (
                          <>
                            <Hash className={cn('size-4 flex-none', isActive && 'text-hue')} aria-hidden />
                            <span className="min-w-0 flex-1 truncate" title={s.name}>{s.name}</span>
                          </>
                        )}
                      </NavLink>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </aside>
        )}
      </div>

      <main className="bg-bg md:border-line flex min-w-0 flex-1 flex-col overflow-hidden md:border-l">
        <Outlet context={{ streams, environment, environments, setEnvironment, openMenu, topbar } satisfies ShellContext} />
      </main>

      {tip && !drawer && (
        <div role="tooltip" style={{ top: tip.top, left: tip.left }} className="bg-raised text-ink pointer-events-none fixed z-70 -translate-y-1/2 rounded-row px-2 py-1 text-[13px] whitespace-nowrap max-md:hidden">
          {tip.text}
        </div>
      )}
      {account && <AccountMenu onClose={closeAccount} onNavigate={closeAccount} />}
      {palette && <CommandPalette commands={commands} onClose={() => setPalette(false)} />}
    </div>
  );
}
