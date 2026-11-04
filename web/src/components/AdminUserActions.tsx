import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, type User } from '../lib/api';
import { adminUsers, validAdminPassword, type AdminUserSession } from '../lib/adminUsers';
import { useAuth } from '../lib/auth';
import AdminUserDialogFrame from './AdminUserDialogFrame';
import type { UserAction } from './AdminUserInspector';

export type AdminUserModal = { action: 'create' } | { action: UserAction; user: User };
const INPUT = 'bg-sunk border-field min-h-11 min-w-0 w-full rounded-row border px-3 py-2 text-sm md:min-h-9';
const BUTTON = 'border-line hover:bg-sunk min-h-11 rounded-row border px-3 py-2 text-sm disabled:opacity-50 md:min-h-9';

export default function AdminUserActions({ modal, currentUserId, onClose, onChanged }: {
  modal: AdminUserModal;
  currentUserId: string;
  onClose: () => void;
  onChanged: (user: User | null, message: string) => void;
}) {
  const { t } = useTranslation();
  const { retry } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const controller = useRef(new AbortController());
  useEffect(() => {
    const active = new AbortController();
    controller.current = active;
    return () => active.abort();
  }, []);
  const user = 'user' in modal ? modal.user : null;
  const self = user?.id === currentUserId;
  const problem = useCallback((err: unknown) => {
    if (err instanceof ApiError && err.status === 401) retry();
    return err instanceof ApiError && err.status < 500 ? err.message : t('adminUsers.actionFailed');
  }, [retry, t]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy) return;
    if (self) { setError(t('adminUsers.selfHelp')); return; }
    const form = event.currentTarget;
    const fields = new FormData(form);
    const password = String(fields.get('password') ?? '');
    const email = String(fields.get('email') ?? '').trim().toLowerCase();
    const username = String(fields.get('username') ?? '').trim().toLowerCase();
    const identity = user ?? { email, username };
    if ((modal.action === 'password' || password) && !validAdminPassword(password, identity)) { setError(t('adminUsers.passwordHelp')); return; }
    if (modal.action === 'create' && !/^[a-z0-9][a-z0-9_.-]{2,31}$/.test(username)) { setError(t('adminUsers.usernameHelp')); return; }
    if (modal.action === 'delete' && fields.get('confirmation') !== user?.username) { setError(t('adminUsers.confirmMismatch')); return; }
    setBusy(true);
    setError('');
    const signal = controller.current.signal;
    try {
      let updated: User | null = user;
      if (modal.action === 'create') updated = await adminUsers.create({ email, username, ...(password ? { password } : {}) }, signal);
      else if (modal.action === 'delete') { await adminUsers.remove(user!.id, signal); updated = null; }
      else if (modal.action === 'password') await adminUsers.resetPassword(user!.id, password, signal);
      else if (modal.action === 'admin' || modal.action === 'status') {
        await adminUsers.update(user!.id, modal.action === 'admin' ? { is_instance_admin: !user!.is_instance_admin } : { status: user!.status === 'active' ? 'disabled' : 'active' }, signal);
        updated = await adminUsers.get(user!.id, signal);
      }
      if (signal.aborted) return;
      form.reset();
      onChanged(updated, t(`adminUsers.success.${modal.action}`));
    } catch (err) { if (!signal.aborted) setError(problem(err)); }
    finally { if (!signal.aborted) setBusy(false); }
  };

  const effect = modal.action === 'admin' ? user?.is_instance_admin ? 'demote' : 'promote' : modal.action === 'status' ? user?.status === 'active' ? 'disable' : 'activate' : modal.action;
  const title = modal.action === 'create' ? t('adminUsers.create') : t(`adminUsers.actionTitle.${effect}`, { name: user?.username });
  return (
    <AdminUserDialogFrame title={title} onClose={onClose} busy={busy}>
      {modal.action === 'sessions' && user ? <UserSessions user={user} onCurrentRevoked={retry} /> : <>
        <p className="text-ink2 text-[13px] leading-relaxed">{t(`adminUsers.actionHelp.${effect}`, { name: user?.username })}</p>
        {error && <p role="alert" className="border-err border-l-2 pl-3 text-[13px] [overflow-wrap:anywhere]">{error}</p>}
        <form onSubmit={event => void submit(event)} className="grid gap-4">
          {modal.action === 'create' && <><label className="grid gap-2 text-sm">{t('adminUsers.email')}<input type="email" name="email" required maxLength={254} autoComplete="off" disabled={busy} className={INPUT} /></label><label className="grid gap-2 text-sm">{t('adminUsers.username')}<input name="username" required minLength={3} maxLength={32} autoComplete="off" disabled={busy} className={INPUT} /><span className="text-ink2 text-xs">{t('adminUsers.usernameHelp')}</span></label></>}
          {(modal.action === 'password' || modal.action === 'create') && <label className="grid gap-2 text-sm">{t(modal.action === 'create' ? 'adminUsers.optionalPassword' : 'adminUsers.newPassword')}<input type="password" name="password" required={modal.action === 'password'} autoComplete="new-password" disabled={busy} className={INPUT} /><span className="text-ink2 text-xs">{t('adminUsers.passwordHelp')}</span></label>}
          {modal.action === 'delete' && <label className="grid gap-2 text-sm">{t('adminUsers.confirmUsername', { name: user?.username })}<input name="confirmation" autoComplete="off" required disabled={busy} className={INPUT} /></label>}
          <div className="flex flex-wrap justify-end gap-2"><button type="button" disabled={busy} onClick={onClose} className={BUTTON}>{t('adminUsers.cancel')}</button><button type="submit" disabled={busy || self} className="bg-accent text-accent-ink min-h-11 rounded-row px-4 py-2 text-sm font-semibold disabled:opacity-50 md:min-h-9">{busy ? t('common.working') : t(`adminUsers.submit.${modal.action}`)}</button></div>
        </form>
      </>}
    </AdminUserDialogFrame>
  );
}

function UserSessions({ user, onCurrentRevoked }: { user: User; onCurrentRevoked: () => void }) {
  const { t, i18n } = useTranslation();
  const [items, setItems] = useState<AdminUserSession[] | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [attempt, setAttempt] = useState(0);
  const { retry } = useAuth();
  const controller = useRef(new AbortController());
  const fail = useCallback((err: unknown) => {
    if (err instanceof ApiError && err.status === 401) retry();
    return err instanceof ApiError && err.status < 500 ? err.message : t('adminUsers.sessionsFailed');
  }, [retry, t]);
  useEffect(() => {
    const ctrl = new AbortController();
    adminUsers.sessions(user.id, ctrl.signal).then(rows => { if (!ctrl.signal.aborted) setItems(rows); }).catch(err => { if (!ctrl.signal.aborted) setError(fail(err)); });
    return () => ctrl.abort();
  }, [user.id, attempt, fail]);
  useEffect(() => { const active = new AbortController(); controller.current = active; return () => active.abort(); }, []);
  const revoke = async (session?: AdminUserSession) => {
    if (busy) return;
    setBusy(true); setError(''); setNotice('');
    const signal = controller.current.signal;
    try {
      if (session) await adminUsers.revokeSession(user.id, session.id, signal);
      else await adminUsers.revokeSessions(user.id, signal);
      if (signal.aborted) return;
      if (session?.current) return onCurrentRevoked();
      setItems(current => current?.filter(item => session ? item.id !== session.id : item.current) ?? []);
      setNotice(t('adminUsers.sessionsRevoked'));
    } catch (err) { if (!signal.aborted) setError(fail(err)); }
    finally { if (!signal.aborted) setBusy(false); }
  };
  const date = (value: string | null) => value ? new Date(value).toLocaleString(i18n.language) : t('adminUsers.notAvailable');
  return <div className="grid gap-4" aria-busy={busy}>
    <p className="text-ink2 text-[13px]">{t('adminUsers.sessionHelp')}</p>
    {error && <div role="alert"><p className="text-err text-sm [overflow-wrap:anywhere]">{error}</p><button type="button" onClick={() => { setError(''); setAttempt(value => value + 1); }} className={BUTTON}>{t('common.retry')}</button></div>}
    {notice && <p role="status" className="text-ink2 text-sm">{notice}</p>}
    {!items && !error && <p role="status">{t('adminUsers.loadingSessions')}</p>}
    {items?.length === 0 && <p>{t('adminUsers.noSessions')}</p>}
    {items?.map(session => <article key={session.id} className="border-line grid gap-2 border-b pb-4"><div className="flex items-start justify-between gap-3"><h3 className="min-w-0 text-sm font-semibold [overflow-wrap:anywhere]">{session.user_agent || t('adminUsers.unknownBrowser')}</h3><button type="button" disabled={busy} onClick={() => void revoke(session)} className={`${BUTTON} flex-none`}>{t('adminUsers.revokeSession')}</button></div><p className="text-ink2 font-mono text-xs [overflow-wrap:anywhere]">{session.ip}{session.current && ` · ${t('adminUsers.currentSession')}`}</p><dl className="text-ink2 grid grid-cols-[96px_minmax(0,1fr)] gap-1 text-xs"><dt>{t('adminUsers.lastSeen')}</dt><dd>{date(session.last_seen_at)}</dd><dt>{t('adminUsers.created')}</dt><dd>{date(session.created_at)}</dd><dt>{t('adminUsers.expires')}</dt><dd>{date(session.expires_at)}</dd></dl></article>)}
    {!!items?.some(item => !item.current) && <button type="button" disabled={busy} onClick={() => void revoke()} className={BUTTON}>{t(items.some(item => item.current) ? 'adminUsers.revokeOthers' : 'adminUsers.revokeAll')}</button>}
  </div>;
}
