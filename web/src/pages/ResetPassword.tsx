import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { account } from '../lib/account';
import { validPassword } from '../lib/password';
import { useAuth } from '../lib/auth';
import EmailFlowFrame, { EMAIL_BUTTON, EMAIL_INPUT, emailProblem, useEmailLinkToken, useEmailRequest } from '../components/EmailFlowFrame';

export default function ResetPassword() {
  const { t } = useTranslation();
  const { state, retry } = useAuth();
  const [token, clearToken] = useEmailLinkToken('reset');
  const controller = useEmailRequest();
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy || !token) return;
    const form = event.currentTarget;
    const password = String(new FormData(form).get('password'));
    if (!validPassword(password)) { setError(t('emailFlow.passwordHelp')); return; }
    const signal = controller.current.signal;
    setBusy(true); setError('');
    try { await account.resetPassword(token, password, signal); if (!signal.aborted) { form.reset(); clearToken(); setDone(true); if (state.status === 'authenticated') retry(); } }
    catch (err) { if (!signal.aborted) setError(emailProblem(err, t)); }
    finally { if (!signal.aborted) setBusy(false); }
  };
  return <EmailFlowFrame title={t('emailFlow.resetTitle')}>{done ? <p role="status">{t('emailFlow.resetDone')}</p> : !token ? <p role="alert" className="text-ink2 text-sm">{t('emailFlow.missingLink')}</p> : <form onSubmit={event => void submit(event)} className="grid gap-4" aria-busy={busy}><p className="text-ink2 text-sm leading-relaxed">{t('emailFlow.resetHelp')}</p>{error && <p role="alert" className="text-err text-sm [overflow-wrap:anywhere]">{error}</p>}<label className="grid gap-2 text-sm font-semibold">{t('emailFlow.newPassword')}<input type="password" name="password" autoComplete="new-password" required disabled={busy} className={EMAIL_INPUT} /><span className="text-ink2 text-xs font-normal leading-relaxed">{t('emailFlow.passwordHelp')}</span></label><button type="submit" disabled={busy} className={EMAIL_BUTTON}>{t(busy ? 'common.working' : 'emailFlow.resetSubmit')}</button></form>}</EmailFlowFrame>;
}
