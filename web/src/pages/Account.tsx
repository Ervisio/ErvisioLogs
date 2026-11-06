import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { request } from '../lib/api';
import { management } from '../lib/manage-api';
import { useSession } from '../lib/auth';
import { applyTheme, currentTheme, type Theme } from '../lib/theme';
import { registerPasskey, supportsPasskeys } from '../lib/passkeys';
import { ActionForm, Field, Page, State, useAction, useRemote } from '../components/Management';
import { Sessions } from './Administration';

type Security = { totp_enabled: boolean; passkeys: { id: string; name: string; created_at: string }[] };
export default function Account() {
  const { t, i18n } = useTranslation();
  const { user } = useSession();
  const [theme, setTheme] = useState(currentTheme);
  const [current, setCurrent] = useState('');
  const [password, setPassword] = useState('');
  const action = useAction();
  return (
    <Page title={t('manage.account')} subtitle={user.username}>
      <section className="manage-section">
        <h2 className="text-lg font-bold">{t('manage.profile')}</h2>
        <dl className="grid gap-3">
          <div>
            <dt className="text-ink3 text-xs">{t('login.field.username')}</dt>
            <dd>{user.username}</dd>
          </div>
          <div>
            <dt className="text-ink3 text-xs">{t('login.field.email')}</dt>
            <dd className="break-all">
              {user.email} · {user.email_verified ? t('manage.verified') : t('manage.unverified')}
            </dd>
          </div>
        </dl>
        {!user.email_verified && (
          <button
            type="button"
            className="manage-button justify-self-start"
            disabled={action.busy}
            onClick={() => void action.run(management.verification)}
          >
            {t('manage.resendVerification')}
          </button>
        )}
        {action.message}
      </section>
      <section className="manage-section">
        <h2 className="text-lg font-bold">{t('manage.preferences')}</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('language')}>
            <select
              className="manage-input"
              value={i18n.resolvedLanguage}
              onChange={(e) => void i18n.changeLanguage(e.target.value)}
            >
              <option value="it">Italiano</option>
              <option value="en">English</option>
            </select>
          </Field>
          <Field label={t('manage.theme')}>
            <select
              className="manage-input"
              value={theme}
              onChange={(e) => {
                const next = e.target.value as Theme;
                applyTheme(next);
                setTheme(next);
              }}
            >
              {['dark', 'light', 'amoled'].map((key) => (
                <option key={key} value={key}>
                  {t(`theme.${key}`)}
                </option>
              ))}
            </select>
          </Field>
        </div>
      </section>
      <section className="manage-section">
        <h2 className="text-lg font-bold">{t('manage.changePassword')}</h2>
        <ActionForm
          busy={action.busy}
          submit={() =>
            action.run(async () => {
              await management.changePassword(current, password);
              setCurrent('');
              setPassword('');
            })
          }
        >
          <Field label={t('manage.currentPassword')}>
            <input
              required
              autoComplete="current-password"
              type="password"
              className="manage-input"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </Field>
          <Field label={t('manage.newPassword')}>
            <input
              required
              minLength={10}
              autoComplete="new-password"
              type="password"
              className="manage-input"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
        </ActionForm>
      </section>
      <SecuritySettings />
      <Sessions />
    </Page>
  );
}
function SecuritySettings() {
  const { t } = useTranslation();
  const load = useCallback((signal: AbortSignal) => request<Security>('GET', '/auth/security', undefined, signal), []);
  const state = useRemote(load);
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [name, setName] = useState('');
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null);
  const [recovery, setRecovery] = useState<string[]>([]);
  const action = useAction(state.refresh);
  return (
    <section className="manage-section">
      <h2 className="text-lg font-bold">{t('manage.security')}</h2>
      <p className="text-ink2">{t('manage.optionalSecurity')}</p>
      <State {...state} retry={state.refresh} />
      {state.data && (
        <>
          <Field label={t('manage.currentPassword')} hint={t('manage.securityPasswordHint')}>
            <input
              type="password"
              autoComplete="current-password"
              className="manage-input"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
          <Field label={t('manage.authenticatorCode')}>
            <input
              className="manage-input"
              autoComplete="one-time-code"
              value={code}
              onChange={(e) => setCode(e.target.value)}
            />
          </Field>
          <div className="border-line grid gap-3 rounded-card border p-4">
            <h3 className="font-semibold">{t('manage.authenticator')}</h3>
            <p className="text-ink2">{state.data.totp_enabled ? t('manage.active') : t('manage.disabled')}</p>
            {state.data.totp_enabled ? (
              <button
                type="button"
                className="manage-button text-err justify-self-start"
                disabled={action.busy}
                onClick={() => void action.run(() => request('DELETE', '/auth/totp', { password, code }))}
              >
                {t('manage.disable')}
              </button>
            ) : (
              <>
                {!setup ? (
                  <button
                    type="button"
                    className="manage-button justify-self-start"
                    disabled={action.busy}
                    onClick={() =>
                      void action.run(async () => setSetup(await request('POST', '/auth/totp/setup', { password })))
                    }
                  >
                    {t('manage.setupAuthenticator')}
                  </button>
                ) : (
                  <>
                    <p>{t('manage.authenticatorHint')}</p>
                    <code className="bg-sunk rounded-row p-3 break-all select-all">{setup.secret}</code>
                    <a className="text-hue min-h-11 content-center underline" href={setup.uri}>
                      {t('manage.openAuthenticator')}
                    </a>
                    <button
                      type="button"
                      className="manage-button manage-primary justify-self-start"
                      disabled={action.busy || !code}
                      onClick={() =>
                        void action.run(async () => {
                          const result = await request<{ recovery_codes: string[] }>('POST', '/auth/totp/confirm', {
                            code,
                          });
                          setRecovery(result.recovery_codes);
                          setSetup(null);
                          setCode('');
                        })
                      }
                    >
                      {t('manage.confirm')}
                    </button>
                  </>
                )}
              </>
            )}
          </div>
          {recovery.length > 0 && (
            <div role="status" className="border-warn grid gap-3 rounded-card border p-4">
              <p>{t('manage.recoveryHint')}</p>
              <pre className="bg-sunk rounded-row p-3 select-all">{recovery.join('\n')}</pre>
              <button className="manage-button justify-self-start" type="button" onClick={() => setRecovery([])}>
                {t('manage.savedRecovery')}
              </button>
            </div>
          )}
          <div className="border-line grid gap-3 rounded-card border p-4">
            <h3 className="font-semibold">{t('manage.passkeys')}</h3>
            {state.data.passkeys.map((key) => (
              <div key={key.id} className="flex items-center justify-between gap-3">
                <span>
                  <span>{key.name}</span>
                  <span className="text-ink3 ml-2 text-xs">{new Date(key.created_at).toLocaleDateString()}</span>
                </span>
                <button
                  type="button"
                  disabled={action.busy}
                  className="manage-button text-err"
                  onClick={() =>
                    void action.run(() => request('DELETE', `/auth/passkeys/${key.id}`, { password, code }))
                  }
                >
                  {t('manage.delete')}
                </button>
              </div>
            ))}
            {supportsPasskeys() ? (
              <>
                <Field label={t('manage.passkeyName')}>
                  <input className="manage-input" value={name} onChange={(e) => setName(e.target.value)} />
                </Field>
                <button
                  type="button"
                  className="manage-button justify-self-start"
                  disabled={action.busy || !name.trim()}
                  onClick={() =>
                    void action.run(async () => {
                      await registerPasskey(password, code, name);
                      setName('');
                    })
                  }
                >
                  {t('manage.addPasskey')}
                </button>
              </>
            ) : (
              <p className="text-ink2">{t('manage.passkeyUnsupported')}</p>
            )}
          </div>
          {action.message}
        </>
      )}
    </section>
  );
}
