import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Copy, KeyRound } from 'lucide-react';
import type { ShellContext } from '../components/Shell';
import { api, ApiError, type ApiToken, type Project } from '../lib/api';
import { useAuth } from '../lib/auth';
import { cn } from '../lib/cn';
import { notifications, futureExpiry } from '../lib/notifications-api';

const SCOPES = ['logs:ingest', 'logs:read', 'logs:export'] as const;

type Loaded = { status: 'ready'; tokens: ApiToken[] } | { status: 'failed'; forbidden: boolean };

export default function Tokens({ project, embedded = false }: { embedded?: boolean; project: Project }) {
  const { t, i18n } = useTranslation();
  const { retry } = useAuth();
  const { topbar } = useOutletContext<ShellContext>();
  const [attempt, setAttempt] = useState(0);
  const [now] = useState(() => Date.now());
  const [loaded, setLoaded] = useState<{ key: string; result: Loaded } | null>(null);
  const [created, setCreated] = useState<{ token: string; name: string } | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const key = `${project.id}:${attempt}`;

  useEffect(() => {
    const ctrl = new AbortController();
    api
      .tokens(project.id, ctrl.signal)
      .then((tokens) => setLoaded({ key, result: { status: 'ready', tokens } }))
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) retry();
        setLoaded({ key, result: { status: 'failed', forbidden: err instanceof ApiError && err.status === 403 } });
      });
    return () => ctrl.abort();
  }, [key, project.id, retry]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const result = loaded?.key === key ? loaded.result : null;
  const date = (iso: string | null) =>
    iso ? new Date(iso).toLocaleDateString(i18n.language, { day: 'numeric', month: 'short', year: 'numeric' }) : '';

  const revoke = async (tok: ApiToken) => {
    if (!window.confirm(t('tokens.revokeConfirm', { name: tok.name }))) return;
    setRevoking(tok.id);
    setActionError(null);
    try {
      await api.revokeToken(tok.id);
      reload();
    } catch {
      setActionError(t('tokens.revokeFailed'));
    } finally {
      setRevoking(null);
    }
  };

  const rotate = async (tok: ApiToken) => {
    if (!window.confirm(t('flows.tokens.rotateConfirm', { name: tok.name }))) return;
    setRevoking(tok.id);
    setActionError(null);
    try {
      const value = await notifications.rotateToken(tok.id);
      setCreated({ token: value.token, name: tok.name });
      reload();
    } catch (err) {
      setActionError(err instanceof ApiError && err.status < 500 ? err.message : t('flows.tokens.rotateFailed'));
    } finally {
      setRevoking(null);
    }
  };

  const active = result?.status === 'ready' ? result.tokens.filter((x) => !x.revoked_at) : [];
  const revoked = result?.status === 'ready' ? result.tokens.filter((x) => x.revoked_at) : [];

  return (
    <>
      {!embedded && topbar(<h1 className="truncate text-base font-bold">{t('nav.tokens')}</h1>)}
      <div className={embedded ? 'min-w-0' : 'flex-1 overflow-auto p-4 md:p-6'}>
        <div className="mx-auto grid max-w-[760px] grid-cols-[minmax(0,1fr)] gap-8">
          <p className="text-ink2 max-w-[60ch]">{t('tokens.intro')}</p>

          {created && (
            <div role="status" className="border-accent grid gap-2 rounded-row border p-4">
              <p className="font-semibold">{t('tokens.createdTitle', { name: created.name })}</p>
              <p className="text-ink2 text-[13px]">{t('tokens.createdText')}</p>
              <div className="bg-sunk flex items-center gap-2 rounded-row p-2">
                <code className="min-w-0 flex-1 truncate font-mono text-[13px]">{created.token}</code>
                <CopyButton text={created.token} />
              </div>
              <button
                type="button"
                onClick={() => setCreated(null)}
                className="text-hue h-11 w-fit font-semibold underline underline-offset-2"
              >
                {t('tokens.done')}
              </button>
            </div>
          )}

          <section aria-labelledby="tok-list" className="grid gap-3">
            <h2 id="tok-list" className="text-[17px] font-bold">
              {t('tokens.active')}
            </h2>
            {!result && (
              <p role="status" className="text-ink2 flex items-center gap-2">
                <span className="spinner" aria-hidden />
                {t('tokens.loading')}
              </p>
            )}
            {result?.status === 'failed' && (
              <div role="alert">
                <p className="text-ink2 mb-2">{result.forbidden ? t('tokens.forbidden') : t('tokens.loadFailed')}</p>
                {!result.forbidden && (
                  <button
                    type="button"
                    onClick={reload}
                    className="text-hue min-h-11 font-semibold underline underline-offset-2"
                  >
                    {t('common.retry')}
                  </button>
                )}
              </div>
            )}
            {result?.status === 'ready' && active.length === 0 && <p className="text-ink2">{t('tokens.none')}</p>}
            {active.length > 0 && (
              <ul className="border-line divide-line grid divide-y rounded-row border">
                {active.map((tok) => (
                  <li key={tok.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 p-3">
                    <KeyRound className="text-ink3 size-4 flex-none" aria-hidden />
                    <div className="min-w-0 flex-1 basis-[calc(100%-4rem)] md:basis-auto">
                      <p className="truncate font-semibold">{tok.name}</p>
                      <p className="text-ink2 text-[13px]">
                        <code className="font-mono">{tok.prefix}…</code>
                        <span className="mx-2">{tok.scopes.join(', ')}</span>
                      </p>
                      <p className="text-ink3 text-[12px]">
                        {t('tokens.created', { date: date(tok.created_at) })}
                        {' · '}
                        {tok.last_used_at ? t('tokens.used', { date: date(tok.last_used_at) }) : t('tokens.neverUsed')}
                        {tok.expires_at && ` · ${t('tokens.expires', { date: date(tok.expires_at) })}`}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() => void rotate(tok)}
                      disabled={revoking === tok.id || (!!tok.expires_at && new Date(tok.expires_at).getTime() <= now)}
                      className="manage-button"
                    >
                      {t('flows.tokens.rotate')}
                    </button>
                    <button
                      type="button"
                      onClick={() => void revoke(tok)}
                      disabled={revoking === tok.id}
                      aria-label={`${t('tokens.revoke')} ${tok.name}`}
                      className="text-err hover:bg-sunk h-11 rounded-row px-3 font-semibold whitespace-nowrap disabled:opacity-60 md:h-9"
                    >
                      {t('tokens.revoke')}
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {actionError && (
              <p role="alert" className="text-err">
                {actionError}
              </p>
            )}
            {revoked.length > 0 && (
              <details className="text-ink2 text-[13px]">
                <summary className="min-h-11 cursor-pointer content-center">
                  {t('tokens.revokedCount', { count: revoked.length })}
                </summary>
                <ul className="mt-1 grid gap-1 pl-4">
                  {revoked.map((tok) => (
                    <li key={tok.id}>
                      {tok.name} <code className="font-mono">{tok.prefix}…</code>{' '}
                      {t('tokens.revokedOn', { date: date(tok.revoked_at) })}
                    </li>
                  ))}
                </ul>
              </details>
            )}
          </section>

          {!(result?.status === 'failed' && result.forbidden) && (
            <CreateToken
              project={project}
              onCreated={(token, name) => {
                setCreated({ token, name });
                reload();
              }}
            />
          )}
        </div>
      </div>
    </>
  );
}

export function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState('');
  const [failed, setFailed] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(text);
      setFailed(false);
    } catch {
      setFailed(true);
    }
  };
  return (
    <button
      type="button"
      onClick={() => void copy()}
      className="bg-raised flex h-9 flex-none items-center gap-1.5 rounded-row px-2.5 text-[13px] whitespace-nowrap"
    >
      <Copy className="size-3.5" aria-hidden />
      {failed ? t('flows.copyFailed') : copied === text ? t('detail.copied') : t('first.copy')}
    </button>
  );
}

function CreateToken({ project, onCreated }: { project: Project; onCreated: (token: string, name: string) => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [scopes, setScopes] = useState<string[]>(['logs:ingest']);
  const [expiry, setExpiry] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const value = name.trim();
    if (!value) return setError(t('tokens.nameRequired'));
    if (scopes.length === 0) return setError(t('tokens.scopeRequired'));
    const expiresAt = futureExpiry(expiry);
    if (expiry && !expiresAt) return setError(t('flows.tokens.expiryInvalid'));
    setBusy(true);
    setError(null);
    try {
      const res = await api.createToken({
        project_id: project.id,
        name: value,
        scopes,
        ...(expiresAt ? { expires_at: expiresAt } : {}),
      });
      setName('');
      setExpiry('');
      onCreated(res.token, value);
    } catch (err) {
      setError(err instanceof ApiError && err.status < 500 ? err.message : t('tokens.createFailed'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section aria-labelledby="tok-new" className="grid gap-3">
      <h2 id="tok-new" className="text-[17px] font-bold">
        {t('tokens.new')}
      </h2>
      <form noValidate onSubmit={(e) => void submit(e)} className="grid max-w-[420px] gap-4">
        <div className="grid gap-1.5">
          <label htmlFor="tok-name" className="font-semibold">
            {t('tokens.name')}
          </label>
          <input
            id="tok-name"
            value={name}
            maxLength={100}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('tokens.namePlaceholder')}
            className="bg-sunk border-field placeholder:text-ink3 h-12 rounded-row border px-3 text-base md:h-11 md:text-sm"
          />
        </div>
        <label className="grid gap-1.5">
          <span className="font-semibold">{t('flows.tokens.expiry')}</span>
          <input
            type="datetime-local"
            value={expiry}
            onChange={(e) => setExpiry(e.target.value)}
            className="manage-input"
          />
          <span className="text-ink3 text-xs">{t('flows.tokens.expiryHint')}</span>
        </label>
        <fieldset className="grid gap-1">
          <legend className="mb-1.5 font-semibold">{t('tokens.scopes')}</legend>
          {SCOPES.map((sc) => (
            <label key={sc} className="flex min-h-11 items-center gap-3 md:min-h-9">
              <input
                type="checkbox"
                checked={scopes.includes(sc)}
                onChange={() => setScopes((cur) => (cur.includes(sc) ? cur.filter((x) => x !== sc) : [...cur, sc]))}
                className="accent-accent size-4"
              />
              <span>
                <code className="font-mono text-[13px]">{sc}</code>
                <span className="text-ink2 ml-2 text-[13px]">{t(`tokens.scope.${sc.replace('logs:', '')}`)}</span>
              </span>
            </label>
          ))}
        </fieldset>
        {error && (
          <p role="alert" className="text-err">
            {error}
          </p>
        )}
        <button
          type="submit"
          disabled={busy}
          className={cn(
            'bg-accent text-accent-ink flex h-12 items-center justify-center gap-2 rounded-row font-bold hover:brightness-110 disabled:opacity-60 md:h-11',
          )}
        >
          {busy && <span className="spinner" aria-hidden />}
          {t('tokens.create')}
        </button>
      </form>
    </section>
  );
}
