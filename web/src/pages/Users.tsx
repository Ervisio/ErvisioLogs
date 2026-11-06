import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { ShellContext } from '../components/Shell';
import { api, ApiError, type Member, type Workspace } from '../lib/api';
import { useAuth, useSession } from '../lib/auth';
import { initials } from '../lib/tenancy';
import { management } from '../lib/manage-api';
import { Field, State, useAction, useRemote } from '../components/Management';
import { CopyButton } from './Tokens';

const ROLES = ['owner', 'admin', 'developer', 'analyst', 'viewer'] as const;
const MANAGERS = new Set(['owner', 'admin']);

type Loaded = { status: 'ready'; members: Member[] } | { status: 'failed'; forbidden: boolean };

/** Members belong to the workspace, so this page shows the same people for every project in it. */
export default function Users({ workspace, embedded = false }: { embedded?: boolean; workspace: Workspace }) {
  const { t, i18n } = useTranslation();
  const { retry } = useAuth();
  const { user } = useSession();
  const { topbar } = useOutletContext<ShellContext>();
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; result: Loaded } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const key = `${workspace.id}:${attempt}`;
  const canManage = MANAGERS.has(workspace.role);
  const isOwner = workspace.role === 'owner';

  useEffect(() => {
    const ctrl = new AbortController();
    api
      .members(workspace.id, ctrl.signal)
      .then((members) => setLoaded({ key, result: { status: 'ready', members } }))
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) retry();
        setLoaded({ key, result: { status: 'failed', forbidden: err instanceof ApiError && err.status === 403 } });
      });
    return () => ctrl.abort();
  }, [key, workspace.id, retry]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const result = loaded?.key === key ? loaded.result : null;
  const date = (iso: string) =>
    new Date(iso).toLocaleDateString(i18n.language, { day: 'numeric', month: 'short', year: 'numeric' });
  const problem = (err: unknown, fallback: string) =>
    err instanceof ApiError && err.status === 409
      ? t('users.lastOwner')
      : err instanceof ApiError && err.status < 500 && err.status !== 401
        ? err.message
        : t(fallback);

  const changeRole = async (m: Member, role: string) => {
    setBusy(m.user_id);
    setActionError(null);
    try {
      await api.setMemberRole(workspace.id, m.user_id, role);
      reload();
    } catch (err) {
      setActionError(problem(err, 'users.roleFailed'));
    } finally {
      setBusy(null);
    }
  };

  const remove = async (m: Member) => {
    if (!window.confirm(t('users.removeConfirm', { name: m.username, workspace: workspace.name }))) return;
    setBusy(m.user_id);
    setActionError(null);
    try {
      await api.removeMember(workspace.id, m.user_id);
      // Leaving drops this workspace from the session, so start again from the top.
      if (m.user_id === user.id) window.location.assign('/');
      else reload();
    } catch (err) {
      setActionError(problem(err, 'users.removeFailed'));
    } finally {
      setBusy(null);
    }
  };

  // Only an owner may change or remove another owner; the server enforces the same rule.
  const editable = (m: Member) => canManage && m.user_id !== user.id && (isOwner || m.role !== 'owner');
  const grantable = ROLES.filter((r) => isOwner || r !== 'owner');

  return (
    <>
      {!embedded && topbar(<h1 className="truncate text-base font-bold">{t('nav.users')}</h1>)}
      <div className={embedded ? 'min-w-0' : 'flex-1 overflow-auto p-4 md:p-6'}>
        <div className="mx-auto grid max-w-[760px] grid-cols-[minmax(0,1fr)] gap-8">
          <p className="text-ink2 max-w-[60ch]">{t('users.intro', { workspace: workspace.name })}</p>

          <section aria-labelledby="mem-list" className="grid gap-3">
            <h2 id="mem-list" className="text-[17px] font-bold">
              {t('users.members')}
            </h2>
            {!result && (
              <p role="status" className="text-ink2 flex items-center gap-2">
                <span className="spinner" aria-hidden />
                {t('users.loading')}
              </p>
            )}
            {result?.status === 'failed' && (
              <div role="alert">
                <p className="text-ink2 mb-2">{result.forbidden ? t('users.forbidden') : t('users.loadFailed')}</p>
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
            {result?.status === 'ready' && (
              <ul className="border-line divide-line grid divide-y rounded-row border">
                {result.members.map((m) => {
                  const self = m.user_id === user.id;
                  return (
                    <li key={m.user_id} className="flex flex-wrap items-center gap-x-4 gap-y-2 p-3">
                      <span
                        aria-hidden
                        className="bg-sunk grid size-9 flex-none place-items-center rounded-full text-xs font-bold"
                      >
                        {initials(m.username)}
                      </span>
                      <div className="min-w-0 flex-1 basis-[calc(100%-4rem)] md:basis-auto">
                        <p className="truncate font-semibold">
                          {m.username}
                          {self && <span className="text-ink3 ml-2 text-[13px] font-normal">{t('users.you')}</span>}
                        </p>
                        <p className="text-ink2 truncate text-[13px]">{m.email}</p>
                        <p className="text-ink3 text-[12px]">{t('users.joined', { date: date(m.joined_at) })}</p>
                      </div>
                      {editable(m) ? (
                        <select
                          aria-label={t('users.roleOf', { name: m.username })}
                          value={m.role}
                          disabled={busy === m.user_id}
                          onChange={(e) => void changeRole(m, e.target.value)}
                          className="bg-sunk border-field h-11 rounded-row border px-2 md:h-9"
                        >
                          {grantable.map((r) => (
                            <option key={r} value={r}>
                              {t(`users.role.${r}`)}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <span className="bg-sunk rounded-row px-2.5 py-1 text-[13px] font-semibold">
                          {t(`users.role.${m.role}`, { defaultValue: m.role })}
                        </span>
                      )}
                      {(editable(m) || self) && (
                        <button
                          type="button"
                          onClick={() => void remove(m)}
                          disabled={busy === m.user_id}
                          aria-label={self ? t('users.leave') : t('users.removeOne', { name: m.username })}
                          className="text-err hover:bg-sunk h-11 rounded-row px-3 font-semibold whitespace-nowrap disabled:opacity-60 md:h-9"
                        >
                          {self ? t('users.leave') : t('users.remove')}
                        </button>
                      )}
                    </li>
                  );
                })}
              </ul>
            )}
            {actionError && (
              <p role="alert" className="text-err">
                {actionError}
              </p>
            )}
          </section>

          {canManage && <Invitations workspace={workspace} roles={grantable} />}

          {canManage && <AddMember workspace={workspace} roles={grantable} onAdded={reload} />}

          <section aria-labelledby="roles-help" className="grid gap-2">
            <h2 id="roles-help" className="text-[17px] font-bold">
              {t('users.rolesTitle')}
            </h2>
            <dl className="grid grid-cols-[minmax(0,1fr)] gap-2 md:grid-cols-[8rem_minmax(0,1fr)]">
              {ROLES.map((r) => (
                <div key={r} className="contents">
                  <dt className="font-semibold">{t(`users.role.${r}`)}</dt>
                  <dd className="text-ink2 mb-2 md:mb-0">{t(`users.roleText.${r}`)}</dd>
                </div>
              ))}
            </dl>
          </section>
        </div>
      </div>
    </>
  );
}

function AddMember({
  workspace,
  roles,
  onAdded,
}: {
  workspace: Workspace;
  roles: readonly string[];
  onAdded: () => void;
}) {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const [role, setRole] = useState('viewer');
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const value = email.trim();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) return setError(t('users.emailInvalid'));
    setBusy(true);
    setError(null);
    setDone(null);
    try {
      const m = await api.addMember(workspace.id, { email: value, role });
      setEmail('');
      setDone(t('users.added', { name: m.username, role: t(`users.role.${m.role}`) }));
      onAdded();
    } catch (err) {
      const status = err instanceof ApiError ? err.status : 0;
      setError(
        status === 404
          ? t('users.noAccount')
          : status === 409
            ? t('users.already')
            : status === 403
              ? t('users.forbidden')
              : t('users.addFailed'),
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <section aria-labelledby="mem-new" className="grid gap-3">
      <h2 id="mem-new" className="text-[17px] font-bold">
        {t('users.add')}
      </h2>
      <form noValidate onSubmit={(e) => void submit(e)} className="grid max-w-[420px] gap-4">
        <div className="grid gap-1.5">
          <label htmlFor="mem-email" className="font-semibold">
            {t('users.email')}
          </label>
          <input
            id="mem-email"
            type="email"
            autoComplete="off"
            value={email}
            aria-invalid={!!error}
            aria-describedby="mem-email-m"
            onChange={(e) => setEmail(e.target.value)}
            className={`bg-sunk h-12 rounded-row border px-3 text-base md:h-11 md:text-sm ${error ? 'border-err' : 'border-field'}`}
          />
          <p id="mem-email-m" className="text-ink3 text-[13px]">
            {t('users.emailHint')}
          </p>
        </div>
        <div className="grid gap-1.5">
          <label htmlFor="mem-role" className="font-semibold">
            {t('users.roleLabel')}
          </label>
          <select
            id="mem-role"
            value={role}
            onChange={(e) => setRole(e.target.value)}
            className="bg-sunk border-field h-12 rounded-row border px-2 md:h-11"
          >
            {roles.map((r) => (
              <option key={r} value={r}>
                {t(`users.role.${r}`)}
              </option>
            ))}
          </select>
        </div>
        {error && (
          <p role="alert" className="text-err">
            {error}
          </p>
        )}
        {done && <p role="status">{done}</p>}
        <button
          type="submit"
          disabled={busy}
          className="bg-accent text-accent-ink flex h-12 items-center justify-center gap-2 rounded-row font-bold hover:brightness-110 disabled:opacity-60 md:h-11"
        >
          {busy && <span className="spinner" aria-hidden />}
          {t('users.addButton')}
        </button>
      </form>
    </section>
  );
}

function Invitations({ workspace, roles }: { workspace: Workspace; roles: readonly string[] }) {
  const { t, i18n } = useTranslation();
  const load = useCallback((signal: AbortSignal) => management.invitations(workspace.id, signal), [workspace.id]);
  const remote = useRemote(load);
  const action = useAction(remote.refresh);
  const [email, setEmail] = useState('');
  const [role, setRole] = useState('viewer');
  const [link, setLink] = useState('');
  const [mailError, setMailError] = useState('');
  const invite = async (event: FormEvent) => {
    event.preventDefault();
    await action.run(async () => {
      const result = await management.invite(workspace.id, email.trim(), role);
      setLink(result.url ?? '');
      setMailError(result.mail_error ?? '');
      setEmail('');
    });
  };
  return (
    <section className="grid gap-4" aria-labelledby="invitations-title">
      <h2 id="invitations-title" className="text-[17px] font-bold">
        {t('flows.invites.title')}
      </h2>
      <p className="text-ink2">{t('flows.invites.hint')}</p>
      <form onSubmit={(event) => void invite(event)} className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto_auto]">
        <Field label={t('users.email')}>
          <input
            required
            type="email"
            className="manage-input"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </Field>
        <Field label={t('users.roleLabel')}>
          <select className="manage-input" value={role} onChange={(event) => setRole(event.target.value)}>
            {roles.map((value) => (
              <option key={value} value={value}>
                {t(`users.role.${value}`)}
              </option>
            ))}
          </select>
        </Field>
        <button className="manage-button manage-primary self-end" disabled={action.busy} type="submit">
          {t('flows.invites.send')}
        </button>
      </form>
      {action.message}
      {mailError && (
        <p role="alert" className="text-warn">
          {mailError}
        </p>
      )}
      {link && (
        <div role="status" className="border-line flex min-w-0 items-center gap-3 rounded-row border p-3">
          <span className="min-w-0 flex-1 break-all font-mono text-xs">{link}</span>
          <CopyButton text={link} />
        </div>
      )}
      <State loading={remote.loading} error={remote.error} retry={remote.refresh} />
      {remote.data?.length === 0 && <p className="text-ink3">{t('flows.invites.none')}</p>}
      <ul className="divide-line divide-y">
        {remote.data?.map((invitation) => (
          <li key={invitation.id} className="flex flex-wrap items-center gap-3 py-3">
            <div className="min-w-0 flex-1 basis-[calc(100%-4rem)] md:basis-auto">
              <p className="break-all font-semibold">{invitation.email}</p>
              <p className="text-ink3 text-xs">
                {t(`users.role.${invitation.role}`)} ·{' '}
                {t('tokens.expires', { date: new Date(invitation.expires_at).toLocaleString(i18n.language) })}
              </p>
            </div>
            <button
              type="button"
              className="manage-button text-err"
              disabled={action.busy}
              onClick={() => {
                if (window.confirm(t('flows.invites.cancelConfirm', { email: invitation.email })))
                  void action.run(() => management.cancelInvitation(workspace.id, invitation.id));
              }}
            >
              {t('flows.invites.cancel')}
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}
