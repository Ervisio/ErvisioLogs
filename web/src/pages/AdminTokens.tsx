import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import AdminUserDialogFrame from '../components/AdminUserDialogFrame';
import type { ShellContext } from '../components/Shell';
import { ApiError } from '../lib/api';
import { ADMIN_SCOPES, adminTokenIssue, adminTokens, type AdminScope, type AdminToken } from '../lib/adminTokens';
import { useAuth, useSession } from '../lib/auth';

const INPUT = 'bg-sunk border-field min-h-11 w-full min-w-0 rounded-row border px-3 text-[13px] disabled:opacity-60 md:min-h-9';
const BUTTON = 'border-line min-h-11 rounded-row border px-3 text-xs font-semibold disabled:opacity-60 md:min-h-9';

export default function AdminTokens() {
  const { t, i18n } = useTranslation();
  const { user } = useSession();
  const { retry } = useAuth();
  const { topbar } = useOutletContext<ShellContext>();
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; items?: AdminToken[]; failed?: boolean } | null>(null);
  const [creating, setCreating] = useState(false);
  const [secret, setSecret] = useState<{ name: string; token: string } | null>(null);
  const [revoking, setRevoking] = useState<AdminToken | null>(null);
  const [revoked, setRevoked] = useState<Set<string>>(() => new Set());
  const [clock, setClock] = useState(() => Date.now());
  const key = `${user.id}:${attempt}`;
  const result = loaded?.key === key ? loaded : null;
  const closeCreate = useCallback(() => setCreating(false), []);
  const closeReveal = useCallback(() => setSecret(null), []);
  const closeRevoke = useCallback(() => setRevoking(null), []);

  useEffect(() => {
    if (!user.is_instance_admin) return;
    const ctrl = new AbortController();
    adminTokens.list(ctrl.signal).then((items) => {
      if (!ctrl.signal.aborted) { setLoaded({ key, items }); setClock(Date.now()); }
    }).catch((err: unknown) => {
      if (ctrl.signal.aborted) return;
      if (err instanceof ApiError && err.status === 401) retry();
      else setLoaded({ key, failed: true });
    });
    return () => ctrl.abort();
  }, [user.is_instance_admin, key, retry]);

  const created = useCallback((value: { token: string; admin_token: AdminToken }) => {
    setCreating(false);
    setSecret({ name: value.admin_token.name, token: value.token });
    setAttempt((cur) => cur + 1);
  }, []);
  const didRevoke = useCallback((id: string) => {
    setRevoking(null);
    setRevoked((cur) => new Set([...cur, id]));
    setAttempt((cur) => cur + 1);
  }, []);
  const stamp = (value: string | null) => value ? new Date(value).toLocaleString(i18n.language) : t('instanceAdmin.tokens.never');

  return <>
    {topbar(<h1 className="truncate text-base font-bold">{t('instanceAdmin.tokens.title')}</h1>)}
    {!user.is_instance_admin ? <p role="alert" className="p-6">{t('instanceAdmin.forbidden')}</p> : <div className="flex-1 overflow-auto p-4 md:p-7"><div className="mx-auto grid max-w-[960px] gap-6">
      <header className="flex flex-wrap items-start justify-between gap-3"><div className="max-w-[650px]"><h2 className="text-xl font-bold">{t('instanceAdmin.tokens.title')}</h2><p className="text-ink2 mt-1 text-[13px]">{t('instanceAdmin.tokens.intro')}</p></div><button type="button" onClick={() => setCreating(true)} className="bg-accent text-accent-ink min-h-11 rounded-row px-3 text-xs font-bold md:min-h-9">{t('instanceAdmin.tokens.create')}</button></header>
      <p className="text-ink2 text-xs">{t('instanceAdmin.tokens.validity')}</p>
      {!result && <p role="status" className="text-ink2 flex items-center gap-2"><span className="spinner" aria-hidden />{t('instanceAdmin.loading')}</p>}
      {result?.failed && <div role="alert"><p>{t('instanceAdmin.tokens.loadFailed')}</p><button type="button" onClick={() => setAttempt((cur) => cur + 1)} className={`${BUTTON} mt-3`}>{t('instanceAdmin.refresh')}</button></div>}
      {result?.items?.length === 0 && <p className="text-ink2 text-[13px]">{t('instanceAdmin.tokens.empty')}</p>}
      <ul className="grid gap-3">{result?.items?.map((token) => {
        const wasRevoked = !!token.revoked_at || revoked.has(token.id);
        const expired = new Date(token.expires_at).getTime() <= clock;
        return <li key={token.id} className="bg-surface border-line min-w-0 rounded-row border p-4"><div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><h3 className="font-semibold [overflow-wrap:anywhere]">{token.name}</h3><code className="text-ink2 mt-1 block font-mono text-xs [overflow-wrap:anywhere]">{token.prefix}…</code></div><span className="text-ink2 text-xs">{t(wasRevoked ? 'instanceAdmin.tokens.revoked' : expired ? 'instanceAdmin.tokens.expired' : 'instanceAdmin.tokens.notRevoked')}</span></div>
          <ul aria-label={t('instanceAdmin.tokens.scopes')} className="my-3 flex flex-wrap gap-2">{token.scopes.map((scope) => <li key={scope} className="bg-sunk rounded-row px-2 py-1 font-mono text-[11px]" title={t(`instanceAdmin.scopes.${scope.split(':')[1]}.hint`)}>{scope}</li>)}</ul>
          <dl className="text-ink2 grid gap-2 text-xs md:grid-cols-2"><div><dt>{t('instanceAdmin.tokens.created')}</dt><dd>{stamp(token.created_at)}</dd></div><div><dt>{t('instanceAdmin.tokens.expires')}</dt><dd>{stamp(token.expires_at)}</dd></div><div><dt>{t('instanceAdmin.tokens.lastUsed')}</dt><dd>{stamp(token.last_used_at)}</dd></div>{token.revoked_at && <div><dt>{t('instanceAdmin.tokens.revokedAt')}</dt><dd>{stamp(token.revoked_at)}</dd></div>}{token.created_by && <div><dt>{t('instanceAdmin.tokens.creator')}</dt><dd className="break-all font-mono">{token.created_by}</dd></div>}</dl>
          {!wasRevoked && <button type="button" onClick={() => setRevoking(token)} className={`${BUTTON} text-err mt-4`} aria-label={t('instanceAdmin.tokens.revokeNamed', { name: token.name })}>{t('instanceAdmin.tokens.revoke')}</button>}
        </li>;
      })}</ul>
    </div></div>}
    {user.is_instance_admin && creating && <CreateTokenDialog onClose={closeCreate} onCreated={created} />}
    {user.is_instance_admin && secret && <TokenReveal token={secret.token} name={secret.name} onClose={closeReveal} />}
    {user.is_instance_admin && revoking && <RevokeTokenDialog token={revoking} onClose={closeRevoke} onRevoked={didRevoke} />}
  </>;
}

function localTime(value: Date) {
  return new Date(value.getTime() - value.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}

function CreateTokenDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (value: { token: string; admin_token: AdminToken }) => void }) {
  const { t } = useTranslation();
  const { retry } = useAuth();
  const [name, setName] = useState('');
  const [scopes, setScopes] = useState<AdminScope[]>([]);
  const [expires, setExpires] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [openedAt] = useState(() => Date.now());
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy) return;
    const issue = adminTokenIssue(name, scopes, expires);
    if (issue) return setError(t(issue));
    const ctrl = new AbortController();
    pending.current = ctrl;
    setBusy(true);
    setError('');
    try {
      const created = await adminTokens.create({ name: name.trim(), scopes, expires_at: new Date(expires).toISOString() }, ctrl.signal);
      if (!ctrl.signal.aborted) onCreated(created);
    } catch (err) {
      if (ctrl.signal.aborted) return;
      if (err instanceof ApiError && err.status === 401) retry();
      else setError(err instanceof ApiError ? err.message : t('instanceAdmin.tokens.createFailed'));
    } finally {
      if (!ctrl.signal.aborted) setBusy(false);
    }
  };

  return <AdminUserDialogFrame title={t('instanceAdmin.tokens.create')} onClose={onClose} busy={busy}><form noValidate onSubmit={(event) => void submit(event)} className="grid gap-4">
    <label className="grid gap-1 text-[13px] font-semibold">{t('instanceAdmin.tokens.name')}<input className={INPUT} value={name} disabled={busy} onChange={(event) => setName(event.target.value)} autoComplete="off" /></label>
    <fieldset disabled={busy} className="grid min-w-0 gap-2"><legend className="mb-2 text-[13px] font-semibold">{t('instanceAdmin.tokens.scopes')}</legend>{ADMIN_SCOPES.map((scope) => <label key={scope} className="border-line flex min-h-11 cursor-pointer items-start gap-3 rounded-row border p-3"><input type="checkbox" checked={scopes.includes(scope)} onChange={(event) => setScopes((cur) => event.target.checked ? [...cur, scope] : cur.filter((value) => value !== scope))} className="mt-0.5 size-5 flex-none accent-accent" /><span className="min-w-0"><span className="block text-[13px] font-semibold">{t(`instanceAdmin.scopes.${scope.split(':')[1]}.label`)}</span><span className="text-ink2 mt-1 block text-xs">{t(`instanceAdmin.scopes.${scope.split(':')[1]}.hint`)}</span><code className="text-ink2 mt-1 block font-mono text-[10px]">{scope}</code></span></label>)}</fieldset>
    <label className="grid gap-1 text-[13px] font-semibold">{t('instanceAdmin.tokens.expiry')}<input className={INPUT} type="datetime-local" value={expires} min={localTime(new Date(openedAt))} max={localTime(new Date(openedAt + 365 * 86400_000))} disabled={busy} onChange={(event) => setExpires(event.target.value)} /><span className="text-ink2 text-xs font-normal">{t('instanceAdmin.tokens.expiryHint')}</span></label>
    {error && <p role="alert" className="text-err text-[13px]">{error}</p>}
    <button type="submit" disabled={busy} className="bg-accent text-accent-ink min-h-11 rounded-row px-3 text-[13px] font-bold disabled:opacity-60 md:min-h-9">{t(busy ? 'instanceAdmin.saving' : 'instanceAdmin.tokens.create')}</button>
  </form></AdminUserDialogFrame>;
}

function TokenReveal({ token, name, onClose }: { token: string; name: string; onClose: () => void }) {
  const { t } = useTranslation();
  const [visible, setVisible] = useState(false);
  const [copied, setCopied] = useState<boolean | null>(null);
  const copy = async () => {
    try { await navigator.clipboard.writeText(token); setCopied(true); } catch { setCopied(false); }
  };
  return <AdminUserDialogFrame title={t('instanceAdmin.tokens.createdTitle', { name })} onClose={onClose}>
    <p className="text-ink2 text-[13px]">{t('instanceAdmin.tokens.once')}</p>
    <label className="grid gap-1 text-xs font-semibold">{t('instanceAdmin.tokens.secret')}<input type={visible ? 'text' : 'password'} className={`${INPUT} font-mono`} value={token} readOnly autoComplete="off" spellCheck={false} /></label>
    <div className="flex flex-wrap gap-2"><button type="button" className={BUTTON} onClick={() => setVisible((value) => !value)}>{t(visible ? 'instanceAdmin.tokens.hide' : 'instanceAdmin.tokens.show')}</button><button type="button" className={BUTTON} onClick={() => void copy()}>{t('instanceAdmin.tokens.copy')}</button><button type="button" className={`${BUTTON} bg-accent text-accent-ink`} onClick={onClose}>{t('instanceAdmin.tokens.done')}</button></div>
    {copied !== null && <p role={copied ? 'status' : 'alert'} className="text-ink2 text-xs">{t(copied ? 'instanceAdmin.tokens.copied' : 'instanceAdmin.tokens.copyFailed')}</p>}
  </AdminUserDialogFrame>;
}

function RevokeTokenDialog({ token, onClose, onRevoked }: { token: AdminToken; onClose: () => void; onRevoked: (id: string) => void }) {
  const { t } = useTranslation();
  const { retry } = useAuth();
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || confirm !== token.name) return;
    const ctrl = new AbortController();
    pending.current = ctrl;
    setBusy(true); setError('');
    try { await adminTokens.revoke(token.id, ctrl.signal); if (!ctrl.signal.aborted) onRevoked(token.id); }
    catch (err) { if (!ctrl.signal.aborted) { if (err instanceof ApiError && err.status === 401) retry(); else setError(err instanceof ApiError ? err.message : t('instanceAdmin.tokens.revokeFailed')); } }
    finally { if (!ctrl.signal.aborted) setBusy(false); }
  };
  return <AdminUserDialogFrame title={t('instanceAdmin.tokens.revokeNamed', { name: token.name })} onClose={onClose} busy={busy}><form onSubmit={(event) => void submit(event)} className="grid gap-4">
    <p className="text-ink2 text-[13px]">{t('instanceAdmin.tokens.revokeHint', { name: token.name })}</p>
    <label className="grid gap-1 text-[13px] font-semibold">{t('instanceAdmin.tokens.confirmName')}<input className={INPUT} value={confirm} disabled={busy} onChange={(event) => setConfirm(event.target.value)} autoComplete="off" /></label>
    {error && <p role="alert" className="text-err text-[13px]">{error}</p>}
    <button type="submit" className={`${BUTTON} text-err`} disabled={busy || confirm !== token.name}>{t(busy ? 'instanceAdmin.saving' : 'instanceAdmin.tokens.revoke')}</button>
  </form></AdminUserDialogFrame>;
}
