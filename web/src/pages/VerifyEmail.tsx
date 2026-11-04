import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { account } from '../lib/account';
import { api } from '../lib/api';
import { useAuth } from '../lib/auth';
import EmailFlowFrame, { EMAIL_BUTTON, emailProblem, useEmailLinkToken, useEmailRequest } from '../components/EmailFlowFrame';

export default function VerifyEmail() {
  const { t } = useTranslation();
  const { state, establish, retry } = useAuth();
  const [token, clearToken] = useEmailLinkToken('verification');
  const controller = useEmailRequest();
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');
  const verify = async () => {
    if (busy || !token) return;
    const signal = controller.current.signal;
    setBusy(true); setError('');
    try {
      await account.verifyEmail(token, signal);
      if (signal.aborted) return;
      clearToken(); setDone(true);
      if (state.status === 'authenticated') {
        try { const session = await api.me(signal); if (!signal.aborted) establish(session); }
        catch { if (!signal.aborted) retry(); }
      }
    } catch (err) { if (!signal.aborted) setError(emailProblem(err, t)); }
    finally { if (!signal.aborted) setBusy(false); }
  };
  return <EmailFlowFrame title={t('emailFlow.verifyTitle')}>{done ? <p role="status">{t('emailFlow.verifyDone')}</p> : !token ? <p role="alert" className="text-ink2 text-sm">{t('emailFlow.missingLink')}</p> : <><p className="text-ink2 text-sm">{t('emailFlow.verifyHelp')}</p>{error && <p role="alert" className="text-err text-sm [overflow-wrap:anywhere]">{error}</p>}<button type="button" disabled={busy} onClick={() => void verify()} className={EMAIL_BUTTON}>{t(busy ? 'common.working' : 'emailFlow.verifySubmit')}</button></>}</EmailFlowFrame>;
}
