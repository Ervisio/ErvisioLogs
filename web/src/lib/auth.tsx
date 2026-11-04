import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { api, ApiError, setCsrfToken, type Session } from './api';

type AuthState =
  | { status: 'loading' }
  | { status: 'anonymous' }
  | { status: 'unreachable' }
  | { status: 'authenticated'; session: Session };

type AuthValue = {
  state: AuthState;
  establish: (session: Session) => void;
  logout: () => Promise<void>;
  retry: () => void;
};

const AuthContext = createContext<AuthValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const bootstrap = useRef<AbortController | null>(null);

  useEffect(() => {
    const ctrl = new AbortController();
    bootstrap.current = ctrl;
    api
      .me(ctrl.signal)
      .then((session) => {
        if (ctrl.signal.aborted) return;
        setCsrfToken(session.csrf_token);
        setState({ status: 'authenticated', session });
      })
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        setCsrfToken(null);
        // Only a 401 means "signed out". A 5xx or a network failure must not bounce
        // someone with a valid session to the sign-in page.
        setState({ status: err instanceof ApiError && err.status === 401 ? 'anonymous' : 'unreachable' });
      });
    return () => ctrl.abort();
  }, [attempt]);

  const establish = useCallback((session: Session) => {
    bootstrap.current?.abort();
    setCsrfToken(session.csrf_token);
    setState({ status: 'authenticated', session });
  }, []);

  const logout = useCallback(async () => {
    await api.logout();
    bootstrap.current?.abort();
    setCsrfToken(null);
    setState({ status: 'anonymous' });
  }, []);

  const retry = useCallback(() => {
    bootstrap.current?.abort();
    setState({ status: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  const value = useMemo(() => ({ state, establish, logout, retry }), [state, establish, logout, retry]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error('useAuth needs AuthProvider');
  return value;
}

/** The signed-in session. Only call below RequireAuth. */
export function useSession(): Session {
  const { state } = useAuth();
  if (state.status !== 'authenticated') throw new Error('useSession needs an authenticated user');
  return state.session;
}

/** Keep only same-app paths, so ?next= cannot send someone to another site. */
export function safeNext(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') && !next.startsWith('/\\') ? next : '/';
}

export function RequireAuth({ loading, unreachable }: { loading: ReactNode; unreachable: ReactNode }) {
  const { state } = useAuth();
  const location = useLocation();
  if (state.status === 'loading') return loading;
  if (state.status === 'unreachable') return unreachable;
  if (state.status === 'anonymous') {
    const here = location.pathname + location.search;
    return <Navigate to={here === '/' ? '/login' : `/login?next=${encodeURIComponent(here)}`} replace />;
  }
  return <Outlet />;
}
