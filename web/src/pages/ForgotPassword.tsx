import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { account } from '../lib/account';
import EmailFlowFrame, { EMAIL_BUTTON, EMAIL_INPUT, emailProblem, useEmailRequest } from '../components/EmailFlowFrame';

export default function ForgotPassword() {
  const { t } = useTranslation();
  const controller = useEmailRequest();
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState('');
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy) return;
    const form = event.currentTarget;
    const email = String(new FormData(form).get('email')).trim().toLowerCase();
    const signal = controller.current.signal;
    setBusy(true); setError('');
    try { await account.forgotPassword(email, signal); if (!signal.aborted) { form.reset(); setSent(true); } }
    catch (err) { if (!signal.aborted) setError(emailProblem(err, t)); }
    finally { if (!signal.aborted) setBusy(false); }
  };
  return <EmailFlowFrame title={t('emailFlow.forgotTitle')}><p className="text-ink2 text-sm leading-relaxed">{t('emailFlow.forgotHelp')}</p>{sent ? <p role="status" className="text-sm leading-relaxed">{t('emailFlow.forgotNeutral')}</p> : <form onSubmit={event => void submit(event)} className="grid gap-4" aria-busy={busy}>{error && <p role="alert" className="text-err text-sm [overflow-wrap:anywhere]">{error}</p>}<label className="grid gap-2 text-sm font-semibold">{t('emailFlow.email')}<input type="email" name="email" autoComplete="email" required maxLength={254} disabled={busy} className={EMAIL_INPUT} /></label><button type="submit" disabled={busy} className={EMAIL_BUTTON}>{t(busy ? 'common.working' : 'emailFlow.sendReset')}</button></form>}</EmailFlowFrame>;
}
