import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { UserSession } from '../../../packages/admin-client/src';
import type { ShellContext } from '../components/Shell';
import { ApiError } from '../lib/api';
import { account } from '../lib/account';
import { useAuth, useSession } from '../lib/auth';
import { validPassword } from '../lib/password';
import { cn } from '../lib/cn';
import { EMAIL_INPUT, emailProblem, useEmailRequest } from '../components/EmailFlowFrame';

const BUTTON = 'border-line hover:bg-sunk min-h-11 rounded-row border px-3 py-2 text-sm disabled:opacity-50 md:min-h-9';
const PRIMARY = 'bg-accent text-accent-ink min-h-11 rounded-row px-4 py-2 text-sm font-semibold disabled:opacity-50 md:min-h-9';

export default function Account() {
  const { t } = useTranslation();
  const { topbar } = useOutletContext<ShellContext>();
  const [section, setSection] = useState<'profile' | 'password' | 'sessions'>('profile');
  return <>
    {topbar(<h1 className="text-base font-bold">{t('accountPage.title')}</h1>)}
    <div className="flex min-h-0 flex-1 flex-col overflow-auto md:flex-row">
      <nav aria-label={t('accountPage.sections')} className="border-line flex flex-none flex-wrap gap-1 border-b p-3 md:w-[184px] md:flex-col md:justify-start md:gap-1 md:border-r md:border-b-0">
        {(['profile', 'password', 'sessions'] as const).map(key => <button key={key} type="button" onClick={() => setSection(key)} aria-current={section === key ? 'page' : undefined} className={cn('min-h-11 rounded-row px-3 text-left text-sm font-semibold md:min-h-9', section === key ? 'bg-sunk text-hue' : 'text-ink2 hover:bg-sunk')}>{t(`accountPage.${key}`)}</button>)}
      </nav>
      <div className="min-w-0 flex-1 p-4 md:p-6"><div className="max-w-[720px]">{section === 'profile' ? <Profile /> : section === 'password' ? <Password /> : <Sessions />}</div></div>
    </div>
  </>;
}

function Profile() {
  const { t, i18n } = useTranslation();
  const { user } = useSession();
  const { retry } = useAuth();
  const controller = useEmailRequest();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const resend = async () => {
    if (busy) return;
    const signal = controller.current.signal;
    setBusy(true); setError(''); setNotice('');
    try { await account.resendVerification(signal); if (!signal.aborted) setNotice(t('accountPage.verificationSent')); }
    catch (err) { if (!signal.aborted) { if (err instanceof ApiError && err.status === 401) retry(); setError(emailProblem(err, t)); } }
    finally { if (!signal.aborted) setBusy(false); }
  };
  const rows = [[t('accountPage.username'), user.username], [t('accountPage.email'), user.email], [t('accountPage.created'), new Date(user.created_at).toLocaleDateString(i18n.language)], ['ID', user.id]];
  return <section className="space-y-6"><div className="space-y-2"><h2 className="text-xl font-bold">{t('accountPage.profile')}</h2><p className="text-ink2 text-sm">{t('accountPage.readOnly')}</p></div><dl className="grid grid-cols-[100px_minmax(0,1fr)] gap-4 text-sm">{rows.map(([label, value]) => <div key={label} className="contents"><dt className="text-ink2">{label}</dt><dd className="min-w-0 [overflow-wrap:anywhere]">{value}</dd></div>)}</dl><div className="border-line space-y-3 border-t pt-5"><h3 className="font-semibold">{t(user.email_verified ? 'accountPage.emailVerified' : 'accountPage.emailUnverified')}</h3>{!user.email_verified && <button type="button" disabled={busy} onClick={() => void resend()} className={BUTTON}>{t(busy ? 'common.working' : 'accountPage.resendVerification')}</button>}{notice && <p role="status" className="text-ink2 text-sm">{notice}</p>}{error && <p role="alert" className="text-err text-sm [overflow-wrap:anywhere]">{error}</p>}</div></section>;
}

function Password() {
  const { t } = useTranslation();
  const { user } = useSession();
  const { retry } = useAuth();
  const controller = useEmailRequest();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const values = new FormData(form);
    const current = String(values.get('current_password'));
    const next = String(values.get('new_password'));
    if (!validPassword(next, user)) { setError(t('emailFlow.passwordHelp')); return; }
    const signal = controller.current.signal;
    setBusy(true); setError(''); setNotice('');
    try { await account.changePassword(current, next, signal); if (!signal.aborted) { form.reset(); setNotice(t('accountPage.passwordChanged')); } }
    catch (err) { if (!signal.aborted) { if (err instanceof ApiError && err.status === 401) retry(); setError(emailProblem(err, t)); } }
    finally { if (!signal.aborted) setBusy(false); }
  };
  return <section className="space-y-5"><div className="space-y-2"><h2 className="text-xl font-bold">{t('accountPage.password')}</h2><p className="text-ink2 text-sm leading-relaxed">{t('accountPage.passwordIntro')}</p></div><form onSubmit={event => void submit(event)} className="grid max-w-[420px] gap-4" aria-busy={busy}>{notice && <p role="status" className="text-sm">{notice}</p>}{error && <p role="alert" className="text-err text-sm [overflow-wrap:anywhere]">{error}</p>}<label className="grid gap-2 text-sm font-semibold">{t('accountPage.currentPassword')}<input type="password" name="current_password" autoComplete="current-password" required disabled={busy} className={EMAIL_INPUT} /></label><label className="grid gap-2 text-sm font-semibold">{t('emailFlow.newPassword')}<input type="password" name="new_password" autoComplete="new-password" required disabled={busy} className={EMAIL_INPUT} /><span className="text-ink2 text-xs font-normal leading-relaxed">{t('emailFlow.passwordHelp')}</span></label><button type="submit" disabled={busy} className={`${PRIMARY} justify-self-start`}>{t(busy ? 'common.working' : 'accountPage.changePassword')}</button></form></section>;
}

function Sessions() {
  const { t, i18n } = useTranslation();
  const { retry } = useAuth();
  const controller = useEmailRequest();
  const [items, setItems] = useState<UserSession[] | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const fail = useCallback((err: unknown) => { if (err instanceof ApiError && err.status === 401) retry(); return emailProblem(err, t); }, [retry, t]);
  useEffect(() => {
    const ctrl = new AbortController();
    account.sessions(ctrl.signal).then(rows => { if (!ctrl.signal.aborted) setItems(rows); }).catch(err => { if (!ctrl.signal.aborted) setError(fail(err)); });
    return () => ctrl.abort();
  }, [attempt, fail]);
  const revoke = async (session?: UserSession) => {
    if (busy) return;
    const signal = controller.current.signal;
    setBusy(true); setError(''); setNotice('');
    try {
      if (session) await account.revokeSession(session.id, signal); else await account.revokeOthers(signal);
      if (signal.aborted) return;
      if (session?.current) { retry(); return; }
      setItems(previous => previous?.filter(item => session ? item.id !== session.id : item.current) ?? []);
      setNotice(t('accountPage.sessionsRevoked'));
    } catch (err) { if (!signal.aborted) setError(fail(err)); }
    finally { if (!signal.aborted) setBusy(false); }
  };
  const date = (value: string | null) => value ? new Date(value).toLocaleString(i18n.language) : t('accountPage.notReported');
  return <section className="space-y-5" aria-busy={busy}><div className="space-y-2"><h2 className="text-xl font-bold">{t('accountPage.sessions')}</h2><p className="text-ink2 text-sm leading-relaxed">{t('accountPage.sessionsIntro')}</p></div>{notice && <p role="status" className="text-sm">{notice}</p>}{error && <div role="alert" className="space-y-2"><p className="text-err text-sm [overflow-wrap:anywhere]">{error}</p><button type="button" onClick={() => { setError(''); setAttempt(value => value + 1); }} className={BUTTON}>{t('common.retry')}</button></div>}{!items && !error && <p role="status">{t('accountPage.loadingSessions')}</p>}{items?.length === 0 && <p>{t('accountPage.noSessions')}</p>}{items?.map(session => <article key={session.id} className="border-line grid gap-3 border-b pb-5"><div className="flex flex-wrap items-start justify-between gap-3"><h3 className="min-w-0 font-semibold [overflow-wrap:anywhere]">{session.user_agent || t('accountPage.notReported')}</h3><button type="button" disabled={busy} onClick={() => void revoke(session)} className={BUTTON}>{t(session.current ? 'accountPage.revokeCurrent' : 'accountPage.revokeSession')}</button></div><p className="text-ink2 font-mono text-xs [overflow-wrap:anywhere]">{session.ip}{session.current && ` · ${t('accountPage.currentSession')}`}</p><dl className="text-ink2 grid grid-cols-[100px_minmax(0,1fr)] gap-2 text-xs"><dt>{t('accountPage.lastSeen')}</dt><dd>{date(session.last_seen_at)}</dd><dt>{t('accountPage.created')}</dt><dd>{date(session.created_at)}</dd><dt>{t('accountPage.expires')}</dt><dd>{date(session.expires_at)}</dd></dl></article>)}{items?.some(item => !item.current) && <button type="button" disabled={busy} onClick={() => void revoke()} className={BUTTON}>{t('accountPage.revokeOthers')}</button>}</section>;
}
