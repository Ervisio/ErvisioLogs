import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { NavLink, useLocation, useNavigate, useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { ShellContext } from './Shell';
import { ApiError } from '../lib/api';

export function useRemote<T>(load: (signal: AbortSignal) => Promise<T>) {
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ load: typeof load; attempt: number; value?: T; error?: string } | null>(null);
  useEffect(() => {
    const ctrl = new AbortController();
    load(ctrl.signal)
      .then((value) => {
        if (!ctrl.signal.aborted) setResult({ load, attempt, value });
      })
      .catch((err: unknown) => {
        if (!ctrl.signal.aborted) setResult({ load, attempt, error: err instanceof Error ? err.message : String(err) });
      });
    return () => ctrl.abort();
  }, [load, attempt]);
  const current = result?.load === load && result.attempt === attempt ? result : null;
  const refresh = useCallback(() => setAttempt((n) => n + 1), []);
  return { data: current?.value, error: current?.error, loading: !current, refresh };
}
export function useAction(onSuccess?: () => void) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  const running = useRef(false);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const [message, setMessage] = useState<{ error: boolean; text: string } | null>(null);
  const run = async (action: () => Promise<unknown>) => {
    if (running.current) return false;
    running.current = true;
    setBusy(true);
    setMessage(null);
    try {
      await action();
      if (mounted.current) {
        setMessage({ error: false, text: t('manage.saved') });
        onSuccess?.();
      }
      return true;
    } catch (err) {
      if (mounted.current)
        setMessage({
          error: true,
          text: err instanceof ApiError || err instanceof Error ? err.message : t('manage.failed'),
        });
      return false;
    } finally {
      running.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  return {
    busy,
    run,
    message: message && (
      <p role={message.error ? 'alert' : 'status'} className={message.error ? 'text-err break-words' : 'text-ok'}>
        {message.text}
      </p>
    ),
  };
}
export function State({ loading, error, retry }: { loading?: boolean; error?: string; retry?: () => void }) {
  const { t } = useTranslation();
  if (error)
    return (
      <div role="alert" className="grid gap-2 py-4">
        <p className="text-err break-words">{error}</p>
        {retry && (
          <button type="button" className="manage-button justify-self-start" onClick={retry}>
            {t('common.retry')}
          </button>
        )}
      </div>
    );
  return loading ? (
    <p role="status" className="text-ink2 flex items-center gap-2 py-4">
      <span className="spinner" />
      {t('login.loading')}
    </p>
  ) : null;
}
export function Page({
  title,
  subtitle,
  tabs = [],
  children,
}: {
  title: string;
  subtitle?: string;
  tabs?: { path: string; label: string }[];
  children: ReactNode;
}) {
  const { topbar } = useOutletContext<ShellContext>();
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <>
      {topbar(
        <div className="min-w-0">
          <h1 className="truncate font-bold">{title}</h1>
          {subtitle && <p className="text-ink3 truncate text-xs">{subtitle}</p>}
        </div>,
      )}
      {tabs.length > 0 && (
        <>
          <div className="hidden md:contents">
            <nav className="manage-tabs">
              {tabs.map((tab) => (
                <NavLink
                  key={tab.path}
                  to={tab.path}
                  end
                  className={({ isActive }) =>
                    `flex min-h-11 shrink-0 items-center rounded-row px-3 ${isActive ? 'bg-accent-soft text-hue font-semibold' : 'text-ink2 hover:bg-sunk'}`
                  }
                >
                  {tab.label}
                </NavLink>
              ))}
            </nav>
          </div>
          <div className="m-3 md:hidden">
            <select
              aria-label={title}
              value={tabs.find((tab) => tab.path === location.pathname)?.path ?? tabs[0].path}
              onChange={(e) => navigate(e.target.value)}
              className="manage-input"
            >
              {tabs.map((tab) => (
                <option key={tab.path} value={tab.path}>
                  {tab.label}
                </option>
              ))}
            </select>
          </div>
        </>
      )}
      <div className="min-h-0 flex-1 overflow-auto px-5 pb-8 md:px-8">
        <div className="mx-auto grid max-w-[960px] gap-5 py-5">{children}</div>
      </div>
    </>
  );
}
export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <label className="manage-field">
      <span className="font-medium">{label}</span>
      {children}
      {hint && <span className="text-ink3 text-xs">{hint}</span>}
    </label>
  );
}
export function ActionForm({
  children,
  submit,
  busy,
  label,
  className = '',
  submitDisabled = false,
}: {
  children: ReactNode;
  submit: () => Promise<unknown>;
  busy: boolean;
  label?: string;
  className?: string;
  submitDisabled?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <form
      className={`grid gap-4 ${className}`}
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        if (!busy && !submitDisabled) void submit();
      }}
    >
      <fieldset disabled={busy} className="contents">
        {children}
      </fieldset>
      <button
        disabled={busy || submitDisabled}
        aria-busy={busy}
        className="manage-button manage-primary justify-self-start"
        type="submit"
      >
        {busy && <span className="spinner" />}
        {label ?? t('manage.save')}
      </button>
    </form>
  );
}
export function ConfirmDelete({
  slug,
  action,
  label,
}: {
  slug: string;
  action: () => Promise<unknown>;
  label?: string;
}) {
  const { t } = useTranslation();
  const [confirm, setConfirm] = useState('');
  const state = useAction();
  return (
    <section className="border-err grid gap-3 rounded-card border p-5">
      <h2 className="text-err font-bold">{label ?? t('manage.danger')}</h2>
      <p className="text-ink2">{t('manage.confirmSlug', { slug })}</p>
      <Field label={t('manage.confirmation')}>
        <input
          className="manage-input"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          autoComplete="off"
        />
      </Field>
      <button
        type="button"
        className="manage-button text-err justify-self-start"
        disabled={state.busy || confirm !== slug}
        onClick={() => void state.run(action)}
      >
        {t('manage.delete')}
      </button>
      {state.message}
    </section>
  );
}
