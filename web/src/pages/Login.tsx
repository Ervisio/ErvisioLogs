import { useCallback, useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { Link, Navigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Eye, EyeOff } from 'lucide-react';
import { api, ApiError, oauthStartUrl, type Providers } from '../lib/api';
import { safeNext, useAuth } from '../lib/auth';
import { cn } from '../lib/cn';

type Mode = 'login' | 'register' | 'setup';
type FieldName = 'login' | 'email' | 'username' | 'password' | 'setup_code';
type Values = Partial<Record<FieldName, string>>;
type Invalid = { key: string; count?: number } | null;

const FIELDS: Record<Mode, FieldName[]> = {
  login: ['login', 'password'],
  register: ['email', 'username', 'password'],
  setup: ['setup_code', 'email', 'username', 'password'],
};
const ERROR_CODES = [
  'provider_denied',
  'invalid_request',
  'invalid_state',
  'exchange_failed',
  'email_unverified',
  'email_in_use',
  'identity_conflict',
  'registration_closed',
  'setup_required',
  'account_disabled',
  'server_error',
];
const ERROR_ACTIONS: Record<string, 'login' | 'setup'> = {
  provider_denied: 'login',
  exchange_failed: 'login',
  email_in_use: 'login',
  identity_conflict: 'login',
  setup_required: 'setup',
};
const PROVIDER_NAMES: Record<string, string> = { google: 'Google', discord: 'Discord', cognito: 'AWS Cognito' };
const EMAIL_RE =
  /^[a-z0-9.!#$%&'*+/=?^_{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$/;
const USERNAME_RE = /^[a-z0-9][a-z0-9_.-]{2,31}$/;

const norm = (v = '') => v.trim().toLowerCase();

/** Same rules as the backend: auth_handlers.go and auth.ValidatePassword. */
function check(mode: Mode, name: FieldName, values: Values): Invalid {
  const raw = values[name] ?? '';
  if (name === 'login') {
    const v = norm(raw);
    if (!v) return { key: 'loginRequired' };
    return v.length > 320 ? { key: 'loginLong' } : null;
  }
  if (name === 'setup_code') return raw.trim() ? null : { key: 'setupRequired' };
  if (name === 'email') {
    const v = norm(raw);
    if (!v) return { key: 'emailRequired' };
    return v.length > 254 || !EMAIL_RE.test(v) ? { key: 'emailInvalid' } : null;
  }
  if (name === 'username') {
    const v = norm(raw);
    if (!v) return { key: 'usernameRequired' };
    if (v.length < 3 || v.length > 32) return { key: 'usernameLength' };
    if (!/^[a-z0-9]/.test(v)) return { key: 'usernameStart' };
    return USERNAME_RE.test(v) ? null : { key: 'usernameChars' };
  }
  if (mode === 'login') return raw ? null : { key: 'passwordLoginRequired' };
  if (!raw) return { key: 'passwordRequired' };
  const runes = Array.from(raw).length;
  if (runes < 10) return { key: 'passwordShort', count: 10 - runes };
  if (new TextEncoder().encode(raw).length > 128) return { key: 'passwordLong' };
  const low = raw.toLowerCase();
  return low === norm(values.email) || low === norm(values.username) ? { key: 'passwordSame' } : null;
}

function Brand() {
  const { t } = useTranslation();
  return (
    <p className="flex items-center gap-2 text-sm font-bold whitespace-nowrap">
      <span aria-hidden className="bg-accent text-accent-ink grid size-6 place-items-center rounded-[7px] text-[13px] font-extrabold">
        E
      </span>
      {t('app.name')}
    </p>
  );
}

function Field(props: {
  mode: Mode;
  name: FieldName;
  value: string;
  error: Invalid;
  onChange: (value: string) => void;
  onBlur: () => void;
}) {
  const { t } = useTranslation();
  const { mode, name, value, error, onChange, onBlur } = props;
  const id = useId();
  const [shown, setShown] = useState(false);
  const isPassword = name === 'password';
  const hasHint = mode !== 'login' && (name === 'username' || name === 'password');
  const message = error ? t(`login.invalid.${error.key}`, { count: error.count }) : hasHint ? t(`login.hint.${name}`) : '';
  const autoComplete = {
    login: 'username',
    email: 'email',
    username: 'username',
    password: mode === 'login' ? 'current-password' : 'new-password',
    setup_code: 'off',
  }[name];
  return (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="font-semibold">
        {t(`login.field.${name}`)}
      </label>
      <div className="relative">
        <input
          id={id}
          name={name}
          value={value}
          type={isPassword && !shown ? 'password' : name === 'email' ? 'email' : 'text'}
          autoComplete={autoComplete}
          autoCapitalize="none"
          spellCheck={false}
          required
          aria-invalid={!!error}
          aria-describedby={message ? `${id}-m` : undefined}
          onChange={(e) => onChange(e.target.value)}
          onBlur={onBlur}
          className={cn(
            'bg-sunk h-12 w-full rounded-row border px-3 text-base md:h-11 md:text-sm',
            error ? 'border-err' : 'border-field',
            isPassword && 'pr-12',
            name === 'setup_code' && 'font-mono',
          )}
        />
        {isPassword && (
          <button
            type="button"
            aria-label={shown ? t('login.hidePassword') : t('login.showPassword')}
            aria-pressed={shown}
            aria-controls={id}
            onClick={() => setShown((s) => !s)}
            className="text-ink2 hover:text-ink absolute top-0 right-0 grid h-full w-11 place-items-center rounded-row"
          >
            {shown ? <EyeOff className="size-5" aria-hidden /> : <Eye className="size-5" aria-hidden />}
          </button>
        )}
      </div>
      {message && (
        <p id={`${id}-m`} className={cn('text-[13px]', error ? 'text-err' : 'text-ink3')}>
          {message}
        </p>
      )}
    </div>
  );
}

function CredentialsForm({ mode }: { mode: Mode }) {
  const { t } = useTranslation();
  const { establish } = useAuth();
  const [values, setValues] = useState<Values>({});
  const [touched, setTouched] = useState<Partial<Record<FieldName, boolean>>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const formRef = useRef<HTMLFormElement>(null);
  const names = FIELDS[mode];

  const waitLabel = (seconds: number | null) => {
    if (seconds === null) return t('login.wait.later');
    return seconds < 60
      ? t('login.wait.seconds', { count: seconds })
      : t('login.wait.minutes', { count: Math.ceil(seconds / 60) });
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setTouched(Object.fromEntries(names.map((n) => [n, true])));
    const firstInvalid = names.find((n) => check(mode, n, values));
    if (firstInvalid) {
      formRef.current?.querySelector<HTMLInputElement>(`input[name="${firstInvalid}"]`)?.focus();
      return;
    }
    setBusy(true);
    setProblem(null);
    try {
      const session =
        mode === 'login'
          ? await api.login({ login: norm(values.login), password: values.password ?? '' })
          : await api.register({
              email: norm(values.email),
              username: norm(values.username),
              password: values.password ?? '',
              ...(mode === 'setup' ? { setup_code: (values.setup_code ?? '').trim() } : {}),
            });
      establish(session);
    } catch (err) {
      if (err instanceof ApiError) {
        const key = `login.problem.${mode}.${err.status}`;
        setProblem(t(key, { wait: waitLabel(err.retryAfter), defaultValue: t('login.fallbackText') }));
      } else {
        setProblem(t('login.unreachableText'));
      }
      setBusy(false);
    }
  };

  return (
    <form ref={formRef} noValidate onSubmit={(e) => void submit(e)} className="grid gap-4">
      {names.map((name) => (
        <Field
          key={name}
          mode={mode}
          name={name}
          value={values[name] ?? ''}
          error={touched[name] ? check(mode, name, values) : null}
          onChange={(v) => setValues((cur) => ({ ...cur, [name]: v }))}
          onBlur={() => setTouched((cur) => ({ ...cur, [name]: true }))}
        />
      ))}
      {problem && (
        <p role="alert" className="err-tint border-err rounded-row border px-3 py-2 text-[13px]">
          {problem}
        </p>
      )}
      <button
        type="submit"
        aria-busy={busy}
        className="bg-accent text-accent-ink mt-1 flex h-12 w-full items-center justify-center gap-2 rounded-row font-bold whitespace-nowrap hover:brightness-110 md:h-11"
      >
        {busy && <span className="spinner" aria-hidden />}
        {t(busy ? `login.busy.${mode}` : `login.idle.${mode}`)}
      </button>
    </form>
  );
}

function ProviderMark({ provider }: { provider: string }) {
  if (provider === 'google') {
    return (
      <svg width="18" height="18" viewBox="0 0 48 48" aria-hidden className="flex-none">
        <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z" />
        <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z" />
        <path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z" />
        <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z" />
      </svg>
    );
  }
  if (provider === 'discord') {
    return (
      <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden className="text-discord flex-none">
        <path
          fill="currentColor"
          d="M20.317 4.3698a19.7913 19.7913 0 00-4.8851-1.5152.0741.0741 0 00-.0785.0371c-.211.3753-.4447.8648-.6083 1.2495-1.8447-.2762-3.68-.2762-5.4868 0-.1636-.3933-.4058-.8742-.6177-1.2495a.077.077 0 00-.0785-.037 19.7363 19.7363 0 00-4.8852 1.515.0699.0699 0 00-.0321.0277C.5334 9.0458-.319 13.5799.0992 18.0578a.0824.0824 0 00.0312.0561c2.0528 1.5076 4.0413 2.4228 5.9929 3.0294a.0777.0777 0 00.0842-.0276c.4616-.6304.8731-1.2952 1.226-1.9942a.076.076 0 00-.0416-.1057c-.6528-.2476-1.2743-.5495-1.8722-.8923a.077.077 0 01-.0076-.1277c.1258-.0943.2517-.1923.3718-.2914a.0743.0743 0 01.0776-.0105c3.9278 1.7933 8.18 1.7933 12.0614 0a.0739.0739 0 01.0785.0095c.1202.099.246.1981.3728.2924a.077.077 0 01-.0066.1276 12.2986 12.2986 0 01-1.873.8914.0766.0766 0 00-.0407.1067c.3604.698.7719 1.3628 1.225 1.9932a.076.076 0 00.0842.0286c1.961-.6067 3.9495-1.5219 6.0023-3.0294a.077.077 0 00.0313-.0552c.5004-5.177-.8382-9.6739-3.5485-13.6604a.061.061 0 00-.0312-.0286zM8.02 15.3312c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9555-2.4189 2.157-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.9555 2.4189-2.1569 2.4189zm7.9748 0c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9554-2.4189 2.1569-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.946 2.4189-2.1568 2.4189Z"
        />
      </svg>
    );
  }
  // AWS Cognito has no mark here: the prototype's logo had no verified source. The name is enough.
  return null;
}

function ProviderButtons({ providers }: { providers: string[] }) {
  const { t } = useTranslation();
  const [opening, setOpening] = useState<string | null>(null);
  return (
    <div className="flex flex-wrap gap-2">
      {providers.map((p) => {
        const name = PROVIDER_NAMES[p] ?? p;
        return (
          <button
            key={p}
            type="button"
            aria-label={t('login.continueWith', { provider: name })}
            aria-busy={opening === p}
            onClick={() => {
              setOpening(p);
              window.location.assign(oauthStartUrl(p));
            }}
            className="bg-sunk border-raised hover:bg-raised hover:border-field flex h-12 min-w-0 flex-1 items-center justify-center gap-2 rounded-row border px-2 font-semibold whitespace-nowrap md:h-11"
          >
            {opening === p ? <span className="spinner" aria-hidden /> : <ProviderMark provider={p} />}
            <span>{name}</span>
          </button>
        );
      })}
    </div>
  );
}

function OAuthError({ code, onSetup }: { code: string; onSetup: string }) {
  const { t } = useTranslation();
  const known = ERROR_CODES.includes(code);
  const action = known ? ERROR_ACTIONS[code] : undefined;
  const base = `login.error.${known ? code : 'fallback'}`;
  return (
    <div role="alert" className="err-tint border-err rounded-row border p-3 text-[13px]">
      <strong className="mb-0.5 block text-sm font-semibold">{t(`${base}.title`)}</strong>
      <p>{t(`${base}.text`)}</p>
      <div className="mt-2 flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        {action && (
          <Link to={action === 'setup' ? onSetup : '/login'} className="text-hue px-0.5 py-3.5 font-semibold underline underline-offset-2">
            {t(`login.action.${action}`)}
          </Link>
        )}
        {known && <code className="text-ink2 font-mono text-xs">{t('login.errorCode', { code })}</code>}
      </div>
    </div>
  );
}

function SetupHelp() {
  const { t } = useTranslation();
  return (
    <div className="bg-surface border-line text-ink2 grid gap-2 rounded-row border p-3 text-[13px]">
      <p className="text-ink font-semibold">{t('login.setup.where')}</p>
      <p>{t('login.setup.find')}</p>
      <pre className="bg-sunk overflow-x-auto rounded-row p-2 font-mono text-xs">{'{"level":"WARN",…,"setup_code":"…"}'}</pre>
      <p>
        {t('login.setup.compose')} <code className="text-ink font-mono text-xs break-all">docker compose logs app | grep setup_code</code>
      </p>
    </div>
  );
}

export default function Login() {
  const { t } = useTranslation();
  const { state } = useAuth();
  const [params] = useSearchParams();
  const [providers, setProviders] = useState<Providers | null>(null);
  const [failed, setFailed] = useState(false);
  const titleRef = useRef<HTMLHeadingElement>(null);

  const [attempt, setAttempt] = useState(0);
  const load = useCallback(() => {
    setFailed(false);
    setAttempt((n) => n + 1);
  }, []);
  useEffect(() => {
    const ctrl = new AbortController();
    api
      .providers(ctrl.signal)
      .then(setProviders)
      .catch(() => {
        if (!ctrl.signal.aborted) setFailed(true);
      });
    return () => ctrl.abort();
  }, [attempt]);

  const next = params.get('next');
  const view = params.get('view');
  const code = params.get('error');
  const keep = next ? `&next=${encodeURIComponent(next)}` : '';
  const toLogin = next ? `/login?next=${encodeURIComponent(next)}` : '/login';
  const toRegister = `/login?view=register${keep}`;
  const toSetup = `/login?view=setup${keep}`;

  let mode: Mode = 'login';
  if (providers?.setup_required || view === 'setup') mode = 'setup';
  else if (view === 'register') mode = 'register';
  const local = providers?.local ?? true;
  const canRegister = !!providers?.open_registration && local;
  const closed = mode === 'register' && !!providers && !canRegister;
  const showProviders = mode !== 'setup' && !closed && (providers?.providers.length ?? 0) > 0;

  // Moving between views keeps keyboard and screen reader users on the new title.
  const firstView = useRef(true);
  useEffect(() => {
    if (firstView.current) {
      firstView.current = false;
      return;
    }
    titleRef.current?.focus();
  }, [mode]);

  if (state.status === 'authenticated') return <Navigate to={safeNext(next)} replace />;

  return (
    <main className="flex min-h-dvh justify-center px-4 py-10 md:items-center">
      <div className="grid w-full max-w-[360px] grid-cols-[minmax(0,1fr)] content-start gap-6">
        <Brand />
        <h1 ref={titleRef} tabIndex={-1} className="text-[22px] font-bold outline-none">
          {t(closed ? 'login.title.register' : `login.title.${mode}`)}
        </h1>

        {code && <OAuthError code={code} onSetup={toSetup} />}

        {failed && (
          <div role="alert" className="err-tint border-err rounded-row border p-3 text-[13px]">
            <strong className="mb-0.5 block text-sm font-semibold">{t('login.unreachableTitle')}</strong>
            <p>{t('login.unreachableText')}</p>
            <button type="button" onClick={() => load()} className="text-hue mt-2 font-semibold underline underline-offset-2">
              {t('common.retry')}
            </button>
          </div>
        )}

        {!providers && !failed && (
          <p role="status" className="text-ink2 flex items-center gap-2">
            <span className="spinner" aria-hidden />
            {t('login.loading')}
          </p>
        )}

        {providers && closed && (
          <div className="grid justify-items-start gap-2">
            <p className="text-ink2">{t('login.closedText')}</p>
            <Link to={toLogin} className="text-hue px-0.5 py-3.5 font-semibold underline underline-offset-2">
              {t('login.backToSignIn')}
            </Link>
          </div>
        )}

        {providers && !closed && (
          <>
            {showProviders && <ProviderButtons providers={providers.providers} />}
            {showProviders && local && (
              <p className="text-ink3 before:bg-raised after:bg-raised flex items-center gap-3 text-[13px] before:h-px before:flex-1 after:h-px after:flex-1">
                {t('login.or')}
              </p>
            )}
            {mode === 'setup' && <SetupHelp />}
            {(local || mode === 'setup') && <CredentialsForm key={mode} mode={mode} />}
            {mode === 'login' && canRegister && (
              <p className="text-ink2">
                {t('login.noAccount')}{' '}
                <Link to={toRegister} className="text-hue px-0.5 py-3.5 font-semibold whitespace-nowrap underline underline-offset-2">
                  {t('login.createAccount')}
                </Link>
              </p>
            )}
            {mode === 'register' && (
              <p className="text-ink2">
                {t('login.haveAccount')}{' '}
                <Link to={toLogin} className="text-hue px-0.5 py-3.5 font-semibold whitespace-nowrap underline underline-offset-2">
                  {t('login.signIn')}
                </Link>
              </p>
            )}
          </>
        )}
      </div>
    </main>
  );
}
