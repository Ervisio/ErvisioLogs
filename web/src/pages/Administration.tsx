import { useCallback, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api, request, INSTANCE_SCOPE, type User, type Workspace } from '../lib/api';
import { management, type AdminToken, type SessionInfo } from '../lib/manage-api';
import { useSession } from '../lib/auth';
import { ActionForm, ConfirmDelete, Field, Page, State, useAction, useRemote } from '../components/Management';
import { RuntimeSettings } from './Settings';
import { Destinations, InstanceRetention, Usage } from './Storage';
import { formatBytes } from './Storage';
import { SystemAlerts } from './Insights';

export default function Administration() {
  const { t } = useTranslation();
  const { user } = useSession();
  const { section = 'overview' } = useParams();
  if (!user.is_instance_admin)
    return (
      <Page title={t('manage.administration')}>
        <p role="alert">{t('storage.adminOnly')}</p>
      </Page>
    );
  if (section === 'audit') return <AuditPage path="/admin" />;
  return (
    <Page title={t('manage.administration')} subtitle={t('manage.instance')}>
      {section === 'overview' && <Health />}
      {section === 'users' && <AdminUsers />}
      {section === 'workspaces' && <AdminWorkspaces />}
      {section === 'settings' && (
        <>
          <p className="text-ink2">{t('manage.runtimeHint')}</p>
          <RuntimeSettings path="/admin" />
        </>
      )}
      {section === 'archive' && <Archive />}
      {section === 'tokens' && <AdminTokens />}
      {section === 'system-alerts' && <SystemAlerts />}
    </Page>
  );
}
function Health() {
  const { t, i18n } = useTranslation();
  const load = useCallback((signal: AbortSignal) => management.health(signal), []);
  const state = useRemote(load);
  const h = state.data;
  return (
    <>
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-xl font-bold">{t('manage.health')}</h2>
        <button type="button" className="manage-button" onClick={state.refresh}>
          {t('manage.refresh')}
        </button>
      </div>
      <State {...state} retry={state.refresh} />
      {h && (
        <>
          <p role="status" className={h.status === 'ok' ? 'text-ok' : 'text-warn'}>
            {h.status}
          </p>
          <div className="grid gap-4 sm:grid-cols-2">
            {[
              ['PostgreSQL', h.postgres],
              ['ClickHouse', h.clickhouse],
            ].map(
              ([name, check]) =>
                typeof check !== 'string' && (
                  <section key={String(name)} className="border-line rounded-card border p-5">
                    <h3 className="font-semibold">{String(name)}</h3>
                    <p className={check.ok ? 'text-ok' : 'text-err'}>
                      {check.ok ? t('manage.available') : check.error}
                    </p>
                  </section>
                ),
            )}
          </div>
          <dl className="grid gap-3 sm:grid-cols-2">
            {[
              [
                t('manage.wal'),
                h.wal
                  ? `${formatBytes(h.wal.bytes, i18n.language)} / ${formatBytes(h.wal.max_bytes, i18n.language)}`
                  : t('manage.unavailable'),
              ],
              [t('manage.queue'), h.wal?.pending_records ?? 0],
              [t('manage.uploads'), h.upload.waiting.count],
              [t('manage.failedUploads'), h.upload.failed.count],
              [
                t('manage.lastUpload'),
                h.upload.last_verified_at ? new Date(h.upload.last_verified_at).toLocaleString() : '—',
              ],
              [t('manage.destination'), h.archive.provider?.name ?? t('manage.none')],
            ].map(([key, val]) => (
              <div key={key} className="border-line rounded-row border p-4">
                <dt className="text-ink3 text-xs">{key}</dt>
                <dd className="mt-1 break-words font-mono">{val}</dd>
              </div>
            ))}
          </dl>
        </>
      )}
    </>
  );
}
export function AdminUsers() {
  const { t } = useTranslation();
  const [query, setQuery] = useState('');
  const [search, setSearch] = useState('');
  const [cursor, setCursor] = useState('');
  const [selected, setSelected] = useState<string | null>(null);
  const load = useCallback((signal: AbortSignal) => management.users(search, cursor, signal), [search, cursor]);
  const state = useRemote(load);
  return (
    <>
      <h2 className="text-xl font-bold">{t('manage.users')}</h2>
      <form
        role="search"
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          setSearch(query);
          setCursor('');
        }}
      >
        <Field label={t('manage.searchUsers')}>
          <input className="manage-input" value={query} onChange={(e) => setQuery(e.target.value)} />
        </Field>
        <button className="manage-button self-end">{t('explorer.search')}</button>
      </form>
      <details className="border-line rounded-row border p-4">
        <summary className="cursor-pointer font-semibold">{t('manage.createUser')}</summary>
        <CreateUser refresh={state.refresh} />
      </details>
      <State {...state} retry={state.refresh} />
      <ul className="divide-line divide-y">
        {state.data?.items?.map((user) => (
          <li key={user.id}>
            <button
              type="button"
              className="hover:bg-sunk flex min-h-16 w-full items-center gap-3 rounded-row px-3 text-left"
              aria-expanded={selected === user.id}
              onClick={() => setSelected(selected === user.id ? null : user.id)}
            >
              <span className="bg-raised grid size-10 shrink-0 place-items-center rounded-full font-semibold">
                {user.username.slice(0, 2).toUpperCase()}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate font-semibold">{user.username}</span>
                <span className="text-ink3 block truncate text-xs">{user.email}</span>
              </span>
              <span className="text-ink2 text-xs">
                {user.is_instance_admin ? t('manage.instanceAdmin') : t(`manage.${user.status}`)}
              </span>
            </button>
            {selected === user.id && <UserProfile key={JSON.stringify(user)} user={user} refresh={state.refresh} />}
          </li>
        ))}
      </ul>
      {state.data && !state.data.items?.length && <p className="text-ink2">{t('manage.empty')}</p>}
      <div className="flex gap-2">
        {cursor && (
          <button type="button" className="manage-button" onClick={() => setCursor('')}>
            {t('manage.firstPage')}
          </button>
        )}
        {state.data?.next_cursor && (
          <button type="button" className="manage-button" onClick={() => setCursor(state.data?.next_cursor ?? '')}>
            {t('manage.next')}
          </button>
        )}
      </div>
    </>
  );
}
function CreateUser({ refresh }: { refresh: () => void }) {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const action = useAction(refresh);
  return (
    <ActionForm
      className="mt-4"
      busy={action.busy}
      label={t('manage.createUser')}
      submit={() =>
        action.run(async () => {
          await management.createUser({ email, username, password });
          setEmail('');
          setUsername('');
          setPassword('');
        })
      }
    >
      <Field label={t('login.field.email')}>
        <input
          required
          type="email"
          className="manage-input"
          value={email}
          autoComplete="off"
          onChange={(e) => setEmail(e.target.value)}
        />
      </Field>
      <Field label={t('login.field.username')}>
        <input
          required
          pattern={String.raw`[a-z0-9][a-z0-9_.\-]{2,31}`}
          className="manage-input"
          value={username}
          autoComplete="off"
          onChange={(e) => setUsername(e.target.value)}
        />
      </Field>
      <Field label={t('login.field.password')} hint={t('manage.optionalPassword')}>
        <input
          type="password"
          minLength={10}
          className="manage-input"
          value={password}
          autoComplete="new-password"
          onChange={(e) => setPassword(e.target.value)}
        />
      </Field>
      {action.message}
    </ActionForm>
  );
}
function UserProfile({ user, refresh }: { user: User; refresh: () => void }) {
  const { t } = useTranslation();
  const { user: current } = useSession();
  const self = user.id === current.id;
  const [status, setStatus] = useState(user.status);
  const [admin, setAdmin] = useState(user.is_instance_admin);
  const [password, setPassword] = useState('');
  const action = useAction(refresh);
  return (
    <div className="bg-sunk/50 grid gap-5 rounded-card p-5">
      <p className="text-ink3 text-sm">{t('manage.adminScopeHint')}</p>
      {self ? (
        <Link to="/account" className="text-hue min-h-11 underline">
          {t('manage.ownAccount')}
        </Link>
      ) : (
        <>
          <ActionForm
            busy={action.busy}
            submit={() => action.run(() => management.updateUser(user.id, { status, is_instance_admin: admin }))}
          >
            <Field label={t('manage.status')}>
              <select className="manage-input" value={status} onChange={(e) => setStatus(e.target.value)}>
                <option value="active">{t('manage.active')}</option>
                <option value="disabled">{t('manage.disabled')}</option>
              </select>
            </Field>
            <label className="flex min-h-11 items-center gap-3">
              <input type="checkbox" checked={admin} onChange={(e) => setAdmin(e.target.checked)} />
              {t('manage.instanceAdmin')}
            </label>
            {action.message}
          </ActionForm>
          <details>
            <summary className="min-h-11 cursor-pointer font-semibold">{t('manage.resetPassword')}</summary>
            <ActionForm
              busy={action.busy}
              label={t('manage.resetPassword')}
              submit={() =>
                action.run(async () => {
                  await management.resetPassword(user.id, password);
                  setPassword('');
                })
              }
            >
              <p className="text-ink2">{t('manage.resetPasswordHint')}</p>
              <Field label={t('manage.newPassword')}>
                <input
                  required
                  minLength={10}
                  type="password"
                  className="manage-input"
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </Field>
            </ActionForm>
          </details>
          <Sessions path={`/admin/users/${user.id}`} />
          <ConfirmDelete
            slug={user.username}
            label={t('manage.deleteUser')}
            action={async () => {
              await management.deleteUser(user.id);
              refresh();
            }}
          />
        </>
      )}
    </div>
  );
}
export function Sessions({ path = '/auth' }: { path?: string }) {
  const { t } = useTranslation();
  const load = useCallback((signal: AbortSignal) => management.sessions(path, signal), [path]);
  const state = useRemote<SessionInfo[]>(load);
  const action = useAction(state.refresh);
  return (
    <section className="grid gap-3">
      <h2 className="font-bold">{t('manage.sessions')}</h2>
      <State {...state} retry={state.refresh} />
      {state.data?.map((session) => (
        <div key={session.id} className="border-line flex flex-wrap items-center gap-3 border-b py-3">
          <div className="min-w-0 flex-1">
            <p className="truncate font-medium" title={session.user_agent}>
              {session.user_agent || t('manage.unknownDevice')}
            </p>
            <p className="text-ink3 text-xs">
              {session.ip} · {new Date(session.last_seen_at ?? session.created_at).toLocaleString()}
              {session.current ? ` · ${t('manage.currentSession')}` : ''}
            </p>
          </div>
          {!session.current && (
            <button
              type="button"
              className="manage-button"
              disabled={action.busy}
              onClick={() => void action.run(() => management.revokeSession(path, session.id))}
            >
              {t('manage.revoke')}
            </button>
          )}
        </div>
      ))}
      <button
        type="button"
        className="manage-button justify-self-start"
        disabled={action.busy || !state.data?.some((session) => !session.current)}
        onClick={() => void action.run(() => management.revokeSession(path))}
      >
        {t('manage.revokeOthers')}
      </button>
      {action.message}
    </section>
  );
}
export function AuditPage({ path }: { path: string }) {
  const { t } = useTranslation();
  const [page, setPage] = useState({ path, cursor: '' });
  const cursor = page.path === path ? page.cursor : '';
  const load = useCallback((signal: AbortSignal) => management.audit(path, cursor, signal), [path, cursor]);
  const state = useRemote(load);
  return (
    <Page title={t('manage.audit')}>
      <State {...state} retry={state.refresh} />
      {state.data && !state.data.items?.length && <p>{t('manage.empty')}</p>}
      {state.data?.items?.map((item) => (
        <details key={item.id} className="border-line border-b py-3">
          <summary className="flex cursor-pointer flex-wrap gap-x-4 gap-y-1">
            <time className="text-ink3 text-xs">{new Date(item.created_at).toLocaleString()}</time>
            <span className="font-semibold">{item.action}</span>
            <span className="text-ink2">{item.actor_name || item.actor_user_id}</span>
          </summary>
          <pre className="bg-sunk mt-3 overflow-auto rounded-row p-3 text-xs">
            {JSON.stringify(item.detail, null, 2)}
          </pre>
        </details>
      ))}
      {state.data?.next_cursor && (
        <button
          type="button"
          className="manage-button justify-self-start"
          onClick={() => setPage({ path, cursor: state.data?.next_cursor ?? '' })}
        >
          {t('manage.next')}
        </button>
      )}
    </Page>
  );
}
function Archive() {
  const load = useCallback(
    async (signal: AbortSignal) => ({ overview: await api.storage(signal), policies: await api.retention(signal) }),
    [],
  );
  const state = useRemote(load);
  return (
    <>
      <State {...state} retry={state.refresh} />
      {state.data && (
        <>
          <Usage overview={state.data.overview} />
          <Destinations providers={state.data.overview.providers ?? []} onChanged={state.refresh} />
          <InstanceRetention
            instance={state.data.policies.find((p) => p.scope_type === 'instance' && p.scope_id === INSTANCE_SCOPE)}
            onSaved={state.refresh}
          />
        </>
      )}
    </>
  );
}
function AdminWorkspaces() {
  const { t } = useTranslation();
  const load = useCallback(
    (signal: AbortSignal) => request<{ items: Workspace[] | null }>('GET', '/admin/workspaces', undefined, signal),
    [],
  );
  const state = useRemote(load);
  return (
    <>
      <h2 className="text-xl font-bold">{t('manage.workspaces')}</h2>
      <p className="text-ink2">{t('manage.adminScopeHint')}</p>
      <State {...state} retry={state.refresh} />
      {state.data?.items?.map((w) => (
        <div key={w.id} className="border-line flex items-center justify-between border-b py-4">
          <div>
            <h3 className="font-semibold">{w.name}</h3>
            <p className="text-ink3 font-mono text-xs">{w.slug}</p>
          </div>
        </div>
      ))}
    </>
  );
}
function AdminTokens() {
  const { t } = useTranslation();
  const load = useCallback((signal: AbortSignal) => management.adminTokens(signal), []);
  const state = useRemote<AdminToken[]>(load);
  const [name, setName] = useState('');
  const [expires, setExpires] = useState('');
  const [scopes, setScopes] = useState(['admin:read']);
  const [token, setToken] = useState('');
  const action = useAction(state.refresh);
  return (
    <>
      <h2 className="text-xl font-bold">{t('manage.operatorTokens')}</h2>
      <p className="text-ink2">{t('manage.operatorHint')}</p>
      <ActionForm
        busy={action.busy}
        label={t('manage.create')}
        submit={() =>
          action.run(async () => {
            const result = await management.createAdminToken({
              name,
              scopes,
              expires_at: new Date(expires).toISOString(),
            });
            setToken(result.token);
            setName('');
          })
        }
      >
        <Field label={t('manage.name')}>
          <input required className="manage-input" value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t('manage.expires')}>
          <input
            type="datetime-local"
            required
            className="manage-input"
            value={expires}
            onChange={(e) => setExpires(e.target.value)}
          />
        </Field>
        <fieldset className="flex flex-wrap gap-4">
          <legend className="mb-2 font-medium">{t('manage.scopes')}</legend>
          {['admin:read', 'admin:users', 'admin:settings', 'admin:tenancy', 'admin:storage'].map((scope) => (
            <label key={scope} className="flex min-h-11 items-center gap-2">
              <input
                type="checkbox"
                checked={scopes.includes(scope)}
                onChange={(e) => setScopes(e.target.checked ? [...scopes, scope] : scopes.filter((s) => s !== scope))}
              />
              {scope}
            </label>
          ))}
        </fieldset>
        {action.message}
      </ActionForm>
      {token && (
        <div role="status" className="bg-sunk grid gap-2 rounded-card p-4">
          <p>{t('tokens.copyNow')}</p>
          <code className="break-all select-all">{token}</code>
          <button type="button" className="manage-button justify-self-start" onClick={() => setToken('')}>
            {t('detail.close')}
          </button>
        </div>
      )}
      <State {...state} retry={state.refresh} />
      {state.data?.map((item) => (
        <div key={item.id} className="border-line flex flex-wrap items-center gap-3 border-b py-3">
          <div className="min-w-0 flex-1">
            <p>
              {item.name} <code className="text-ink3 text-xs">{item.prefix}</code>
            </p>
            <p className="text-ink3 text-xs">
              {new Date(item.expires_at).toLocaleString()} · {item.scopes.join(', ')}
            </p>
          </div>
          {!item.revoked_at && (
            <button
              type="button"
              className="manage-button text-err"
              disabled={action.busy}
              onClick={() => void action.run(() => management.revokeAdminToken(item.id))}
            >
              {t('manage.revoke')}
            </button>
          )}
        </div>
      ))}
    </>
  );
}
