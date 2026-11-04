import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useMatch, useNavigate } from 'react-router-dom';
import { api, ApiError, type Project, type Workspace } from './api';
import { useAuth, useSession } from './auth';
import { workspacePath } from './sections';

type ProjectsState = { status: 'loading' } | { status: 'failed' } | { status: 'ready'; projects: Project[] };

type TenancyValue = {
  workspaces: Workspace[];
  workspace: Workspace | null;
  selectWorkspace: (id: string) => void;
  addWorkspace: (workspace: Workspace) => void;
  projects: ProjectsState;
  addProject: (project: Project) => void;
  reload: () => void;
};

const TenancyContext = createContext<TenancyValue | null>(null);

export function TenancyProvider({ children }: { children: ReactNode }) {
  const session = useSession();
  const { retry } = useAuth();
  const navigate = useNavigate();
  const scopedWorkspaceId = useMatch('/w/:workspaceId/*')?.params.workspaceId;
  const [workspaces, setWorkspaces] = useState(session.workspaces ?? []);
  const [loaded, setLoaded] = useState<{ key: string; result: ProjectsState } | null>(null);
  const [attempt, setAttempt] = useState(0);

  const workspace = scopedWorkspaceId
    ? workspaces.find((w) => w.id === scopedWorkspaceId) ?? null
    : workspaces.find((w) => w.id === localStorage.getItem('workspace')) ?? workspaces[0] ?? null;
  const workspaceId = workspace?.id ?? null;
  const loadKey = `${workspaceId}:${attempt}`;

  useEffect(() => {
    if (workspaceId) localStorage.setItem('workspace', workspaceId);
  }, [workspaceId]);

  // A result counts only for the request that produced it, so switching workspace shows "loading".
  const projects = useMemo<ProjectsState>(() => {
    if (!workspaceId) return { status: 'ready', projects: [] };
    return loaded?.key === loadKey ? loaded.result : { status: 'loading' };
  }, [workspaceId, loaded, loadKey]);

  useEffect(() => {
    if (!workspaceId) return;
    const ctrl = new AbortController();
    api
      .projects(workspaceId, ctrl.signal)
      .then((list) => {
        if (!ctrl.signal.aborted) setLoaded({ key: loadKey, result: { status: 'ready', projects: list.filter((p) => p.workspace_id === workspaceId) } });
      })
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        // An expired session asks the auth layer again, and the guard sends the user to sign in.
        if (err instanceof ApiError && err.status === 401) retry();
        else setLoaded({ key: loadKey, result: { status: 'failed' } });
      });
    return () => ctrl.abort();
  }, [workspaceId, loadKey, retry]);

  const selectWorkspace = useCallback((id: string) => {
    localStorage.setItem('workspace', id);
    navigate(workspacePath(id));
  }, [navigate]);
  const addWorkspace = useCallback((created: Workspace) => {
    setWorkspaces((cur) => [...cur, created]);
    localStorage.setItem('workspace', created.id);
    navigate(workspacePath(created.id));
  }, [navigate]);
  const addProject = useCallback((created: Project) => {
    if (created.workspace_id !== workspaceId) return;
    setLoaded((cur) => (cur?.key === loadKey && cur.result.status === 'ready' ? { key: cur.key, result: { status: 'ready', projects: [...cur.result.projects, created] } } : cur));
  }, [workspaceId, loadKey]);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  const value = useMemo(
    () => ({ workspaces, workspace, selectWorkspace, addWorkspace, projects, addProject, reload }),
    [workspaces, workspace, selectWorkspace, addWorkspace, projects, addProject, reload],
  );
  return <TenancyContext.Provider value={value}>{children}</TenancyContext.Provider>;
}

export function useTenancy() {
  const value = useContext(TenancyContext);
  if (!value) throw new Error('useTenancy needs TenancyProvider');
  return value;
}

/** Up to two letters for a project's circle in the rail. */
export function initials(name: string): string {
  const words = name.trim().split(/[\s._-]+/).filter(Boolean);
  const letters = words.length > 1 ? [words[0], words[1]].map((w) => Array.from(w)[0]) : Array.from(words[0] ?? '?').slice(0, 2);
  return letters.join('').toUpperCase();
}
