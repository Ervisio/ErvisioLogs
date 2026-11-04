import { useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { account, type InvitationPreview } from '../lib/account';
import { api } from '../lib/api';
import { useAuth } from '../lib/auth';
import { validPassword } from '../lib/password';
import { workspacePath } from '../lib/sections';
import EmailFlowFrame, { EMAIL_BUTTON, EMAIL_INPUT, emailProblem, useEmailLinkToken, useEmailRequest } from '../components/EmailFlowFrame';

export default function Invitation() {
  const { t, i18n } = useTranslation();
  const { state, establish, logout } = useAuth();
  const navigate = useNavigate();
  const [token, clearToken] = useEmailLinkToken('invitation');
  const controller = useEmailRequest();
  const [preview, setPreview] = useState<InvitationPreview | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const sessionReady = state.status === 'authenticated' || state.status === 'anonymous';
  const switchAccount = async () => {
    setBusy(true); setError('');
    try { await logout(); if (!controller.current.signal.aborted) navigate('/login?next=%2Faccept-invitation'); }
    catch (err) { if (!controller.current.signal.aborted) setError(emailProblem(err, t)); }
    finally { if (!controller.current.signal.aborted) setBusy(false); }
  };
  useEffect(() => {
    if (!token || !sessionReady) return;
    const ctrl = new AbortController();
    account.previewInvitation(token, ctrl.signal).then(value => { if (!ctrl.signal.aborted) setPreview(value); }).catch(err => { if (!ctrl.signal.aborted) setError(emailProblem(err, t)); });
    return () => ctrl.abort();
  }, [token, attempt, t, sessionReady]);

  const accept = async () => {
    if (busy || !token) return;
    const signal = controller.current.signal;
    setBusy(true); setError('');
    try {
      const workspace = await account.acceptInvitation(token, signal);
      if (signal.aborted) return;
      clearToken(); setDone(true);
      const session = await api.me(signal);
      if (!signal.aborted) { establish(session); navigate(workspacePath(workspace.id), { replace: true }); }
    } catch (err) { if (!signal.aborted) setError(emailProblem(err, t)); }
    finally { if (!signal.aborted) setBusy(false); }
  };
  const register = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!token || !preview || busy) return;
    const form = event.currentTarget;
    const fields = new FormData(form);
    const username = String(fields.get('username')).trim().toLowerCase();
    const password = String(fields.get('password'));
    if (!/^[a-z0-9][a-z0-9_.-]{2,31}$/.test(username)) { setError(t('emailFlow.usernameHelp')); return; }
    if (!validPassword(password, { email: preview.email, username })) { setError(t('emailFlow.passwordHelp')); return; }
    const signal = controller.current.signal;
    setBusy(true); setError('');
    try {
      const session = await account.registerInvitation(token, username, password, signal);
      if (signal.aborted) return;
      form.reset(); clearToken(); setDone(true); establish(session); navigate('/', { replace: true });
    } catch (err) { if (!signal.aborted) setError(emailProblem(err, t)); }
    finally { if (!signal.aborted) setBusy(false); }
  };
  return <EmailFlowFrame title={t('emailFlow.inviteTitle')} signInTo="/login?next=%2Faccept-invitation">
    {error && <div role="alert" className="grid gap-3"><p className="text-err text-sm [overflow-wrap:anywhere]">{error}</p>{!preview && token && <button type="button" onClick={() => { setError(''); setAttempt(value => value + 1); }} className="text-hue min-h-11 text-left underline">{t('common.retry')}</button>}</div>}
    {done ? <p role="status">{t('emailFlow.inviteDone')}</p> : !token ? <p role="alert" className="text-ink2 text-sm">{t('emailFlow.missingLink')}</p> : !preview ? !error && <p role="status">{t('emailFlow.loadingInvite')}</p> : <>
      <div className="grid gap-2 text-sm"><h2 className="text-lg font-bold [overflow-wrap:anywhere]">{preview.workspace_name}</h2><p className="text-ink2 [overflow-wrap:anywhere]">{preview.email}</p><p>{t('emailFlow.inviteRole', { role: t(`users.role.${preview.role}`, { defaultValue: preview.role }) })}</p><p className="text-ink2 text-xs">{t('emailFlow.inviteExpires', { date: new Date(preview.expires_at).toLocaleString(i18n.language) })}</p></div>
      {state.status === 'authenticated' ? <><p className="text-ink2 text-sm [overflow-wrap:anywhere]">{t('emailFlow.signedInAs', { email: state.session.user.email })}</p>{state.session.user.email.toLowerCase() !== preview.email.toLowerCase() ? <><p className="text-ink2 text-sm">{t('emailFlow.wrongAccount')}</p><button type="button" disabled={busy} onClick={() => void switchAccount()} className={EMAIL_BUTTON}>{t(busy ? 'common.working' : 'emailFlow.switchAccount')}</button></> : <button type="button" disabled={busy} onClick={() => void accept()} className={EMAIL_BUTTON}>{t(busy ? 'common.working' : 'emailFlow.acceptInvite')}</button>}</> : state.status === 'loading' ? <p role="status">{t('emailFlow.loadingAccount')}</p> : preview.has_account ? <><p className="text-ink2 text-sm leading-relaxed">{t('emailFlow.signInInvite')}</p><Link className={`${EMAIL_BUTTON} grid place-items-center`} to="/login?next=%2Faccept-invitation">{t('emailFlow.signIn')}</Link><p className="text-ink2 text-xs leading-relaxed">{t('emailFlow.inviteContinuity')}</p></> : <form onSubmit={event => void register(event)} className="grid gap-4" aria-busy={busy}><p className="text-ink2 text-sm leading-relaxed">{t('emailFlow.registerInviteHelp')}</p><label className="grid gap-2 text-sm font-semibold">{t('emailFlow.username')}<input name="username" autoComplete="username" required minLength={3} maxLength={32} disabled={busy} className={EMAIL_INPUT} /><span className="text-ink2 text-xs font-normal">{t('emailFlow.usernameHelp')}</span></label><label className="grid gap-2 text-sm font-semibold">{t('emailFlow.newPassword')}<input type="password" name="password" autoComplete="new-password" required disabled={busy} className={EMAIL_INPUT} /><span className="text-ink2 text-xs font-normal">{t('emailFlow.passwordHelp')}</span></label><button type="submit" disabled={busy} className={EMAIL_BUTTON}>{t(busy ? 'common.working' : 'emailFlow.registerInvite')}</button></form>}
    </>}
  </EmailFlowFrame>;
}
