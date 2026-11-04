import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { ApiError } from '../lib/api';
import { captureEmailToken, clearEmailToken } from '../lib/account';
import { useAuth } from '../lib/auth';

export const EMAIL_INPUT = 'bg-sunk border-field min-h-12 min-w-0 w-full rounded-row border px-3 text-base md:min-h-11 md:text-sm';
export const EMAIL_BUTTON = 'bg-accent text-accent-ink min-h-12 rounded-row px-4 font-bold disabled:opacity-50 md:min-h-11';

export function emailProblem(err: unknown, t: TFunction): string {
  if (err instanceof ApiError && err.status === 410) return t('emailFlow.expired');
  if (err instanceof ApiError && err.status === 429) return t('emailFlow.rateLimited', { seconds: err.retryAfter ?? 60 });
  return err instanceof ApiError && err.status < 500 ? err.message : t('emailFlow.failed');
}

export function useEmailRequest() {
  const controller = useRef(new AbortController());
  useEffect(() => { const active = new AbortController(); controller.current = active; return () => active.abort(); }, []);
  return controller;
}

export function useEmailLinkToken(kind: 'reset' | 'verification' | 'invitation') {
  const location = useLocation();
  const [token, setToken] = useState(() => captureEmailToken(kind, location.hash));
  const clear = () => { clearEmailToken(kind); setToken(''); };
  return [token, clear] as const;
}

export default function EmailFlowFrame({ title, children, signInTo = '/login' }: { title: string; children: ReactNode; signInTo?: string }) {
  const { t } = useTranslation();
  const { state, retry } = useAuth();
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus(); }, []);
  return <main className="flex min-h-dvh justify-center px-4 py-10 md:items-center"><div className="grid w-full max-w-[360px] min-w-0 content-start gap-6"><p className="flex items-center gap-2 text-sm font-bold"><span aria-hidden className="bg-accent text-accent-ink grid size-6 place-items-center rounded-row text-[13px] font-extrabold">E</span>{t('app.name')}</p><h1 ref={heading} tabIndex={-1} className="text-2xl font-bold outline-none">{title}</h1>{state.status === 'loading' ? <p role="status">{t('login.loading')}</p> : state.status === 'unreachable' ? <div role="alert" className="grid gap-3 text-sm"><p>{t('login.unreachableText')}</p><button type="button" onClick={retry} className="text-hue min-h-11 text-left underline">{t('common.retry')}</button></div> : children}<Link className="text-ink2 hover:text-ink flex min-h-11 items-center text-sm underline underline-offset-2" to={signInTo}>{t('emailFlow.signIn')}</Link></div></main>;
}
