import { useCallback, useEffect, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { request, type Session } from '../lib/api';
import { useAuth } from '../lib/auth';
import { ActionForm, Field, State, useAction, useRemote } from '../components/Management';

export default function AccountLink() {
  const { t } = useTranslation();
  const location = useLocation();
  const { state, establish } = useAuth();
  const [token] = useState(() => new URLSearchParams(location.hash.slice(1)).get('token') ?? '');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [username, setUsername] = useState('');
  const [done, setDone] = useState(false);
  const action = useAction();
  useEffect(() => {
    if (location.hash) history.replaceState(null, '', location.pathname);
  }, [location.hash, location.pathname]);
  const mode = location.pathname.slice(1);
  const load = useCallback(
    (signal: AbortSignal) =>
      state.status !== 'loading' && state.status !== 'unreachable' && mode === 'invite' && token
        ? request<{ workspace_name: string; email: string; role: string; has_account: boolean }>(
            'POST',
            '/invitations/preview',
            { token },
            signal,
          )
        : Promise.resolve(null),
    [mode, token, state.status],
  );
  const preview = useRemote(load);
  const submit = () =>
    action.run(async () => {
      if (mode === 'forgot-password') await request('POST', '/auth/password/forgot', { email });
      else if (mode === 'reset-password') await request('POST', '/auth/password/reset', { token, password });
      else if (mode === 'verify-email') await request('POST', '/auth/email/verify', { token });
      else if (state.status === 'authenticated') {
        await request('POST', '/invitations/accept', { token });
        establish(await request('GET', '/auth/me'));
      } else establish(await request<Session>('POST', '/invitations/register', { token, username, password }));
      setDone(true);
    });
  return (
    <main className="grid min-h-dvh place-items-center p-5">
      <div className="bg-surface border-line grid w-full max-w-md gap-5 rounded-card border p-6">
        <h1 className="text-xl font-bold">{t(`manage.link_${mode}`)}</h1>
        {done ? (
          <p role="status">{t(mode === 'forgot-password' ? 'manage.forgotSent' : 'manage.completed')}</p>
        ) : (
          <>
            <State {...preview} />
            {mode === 'invite' && preview.data && (
              <p className="text-ink2">
                {preview.data.workspace_name} · {preview.data.email} · {t(`users.role.${preview.data.role}`)}
              </p>
            )}
            {mode !== 'forgot-password' && !token ? (
              <p role="alert" className="text-err">
                {t('manage.invalidLink')}
              </p>
            ) : (
              !preview.error && (
                <ActionForm
                  busy={
                    action.busy ||
                    state.status === 'loading' ||
                    (mode === 'invite' &&
                      (!preview.data || (preview.data.has_account && state.status !== 'authenticated')))
                  }
                  label={t('manage.continue')}
                  submit={submit}
                >
                  {mode === 'forgot-password' && (
                    <Field label={t('login.field.email')}>
                      <input
                        type="email"
                        required
                        className="manage-input"
                        value={email}
                        onChange={(e) => setEmail(e.target.value)}
                      />
                    </Field>
                  )}
                  {mode === 'invite' && state.status !== 'authenticated' && preview.data?.has_account ? (
                    <Link
                      to={`/login?next=/invite#invite=${encodeURIComponent(token)}`}
                      className="text-hue min-h-11 content-center underline"
                    >
                      {t('manage.signInToAccept')}
                    </Link>
                  ) : (
                    <>
                      {mode === 'invite' && state.status !== 'authenticated' && (
                        <Field label={t('login.field.username')}>
                          <input
                            required
                            className="manage-input"
                            value={username}
                            onChange={(e) => setUsername(e.target.value)}
                          />
                        </Field>
                      )}
                      {(mode === 'reset-password' || (mode === 'invite' && state.status !== 'authenticated')) && (
                        <Field label={t('manage.newPassword')}>
                          <input
                            type="password"
                            required
                            minLength={10}
                            autoComplete="new-password"
                            className="manage-input"
                            value={password}
                            onChange={(e) => setPassword(e.target.value)}
                          />
                        </Field>
                      )}
                    </>
                  )}
                  {action.message}
                </ActionForm>
              )
            )}
          </>
        )}
        <Link to="/" className="text-hue min-h-11 content-center underline">
          {t('manage.backToApp')}
        </Link>
      </div>
    </main>
  );
}
