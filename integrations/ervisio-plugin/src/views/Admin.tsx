import { useState, type ReactNode } from 'react';
import type {
  AdminClient,
  Setting,
  User,
  Workspace,
  Project,
  Environment,
  Provider,
  ProviderInput,
  RetentionInput,
} from '../../../../packages/admin-client/src/index';
import { INSTANCE_SCOPE } from '../../../../packages/admin-client/src/index';
import { Badge, Button, ConfirmDialog, Dialog, EmptyState, Input, Select, Skeleton, Switch, Textarea } from '../kit';
import { getSdk } from '../sdk';
import { adminClient, type Connection } from '../api/admin';
import type { Ctx } from './LogsPage';
import { errText, useLoad } from './util';

export const tr = (en: string, it: string) => (getSdk().lang().startsWith('it') ? it : en);
type Field = {
  key: string;
  label: string;
  value?: string;
  type?: string;
  required?: boolean;
  options?: string[];
  multiline?: boolean;
  provider?: string;
};
type Form = {
  title: string;
  fields: Field[];
  save(values: Record<string, string>): Promise<unknown>;
  result?: boolean;
};

export function useAdminActions(reload: () => void) {
  const [form, setForm] = useState<Form | null>(null);
  const [confirmation, setConfirmation] = useState<{ title: string; text: string; run(): Promise<unknown> } | null>(
    null,
  );
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState('');
  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    setError('');
    try {
      const value = await action();
      reload();
      return value;
    } catch (e) {
      setError(errText(e));
      throw e;
    } finally {
      setBusy(false);
    }
  };
  const execute = (action: () => Promise<unknown>) => {
    void run(action).catch(() => undefined);
  };
  const dialogs = (
    <>
      {error && (
        <div className="el-note is-err" role="alert">
          {error}
        </div>
      )}
      {form && (
        <Editor
          key={form.title}
          form={form}
          busy={busy}
          error={error}
          close={() => setForm(null)}
          save={async (v) => {
            try {
              const value = await run(() => form.save(v));
              if (form.result) setResult(typeof value === 'string' ? value : JSON.stringify(value, null, 2));
              setForm(null);
            } catch {
              /* The shared action error stays visible. */
            }
          }}
        />
      )}
      <ConfirmDialog
        open={!!confirmation}
        title={confirmation?.title ?? ''}
        confirmText={confirmation?.text}
        confirmLabel={tr('Confirm', 'Conferma')}
        danger
        onClose={() => setConfirmation(null)}
        onConfirm={async () => {
          if (confirmation) {
            try {
              await run(confirmation.run);
              setConfirmation(null);
            } catch {
              /* Keep confirmation open on failure. */
            }
          }
        }}
      />
      <Dialog
        open={!!result}
        title={tr('Copy this value now', 'Copia questo valore ora')}
        description={tr('It will not be shown again.', 'Non verrà mostrato di nuovo.')}
        onClose={() => setResult('')}
      >
        <pre className="el-result">{result}</pre>
        <Button
          onClick={() => {
            void navigator.clipboard.writeText(result).catch((e) => setError(errText(e)));
          }}
        >
          {tr('Copy', 'Copia')}
        </Button>
      </Dialog>
    </>
  );
  return {
    edit: setForm,
    confirm: (title: string, text: string, action: () => Promise<unknown>) =>
      setConfirmation({ title, text, run: action }),
    execute,
    dialogs,
    busy,
  };
}
function Editor({
  form,
  busy,
  error,
  close,
  save,
}: {
  form: Form;
  busy: boolean;
  error: string;
  close(): void;
  save(v: Record<string, string>): Promise<void>;
}) {
  const [values, setValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(form.fields.map((f) => [f.key, f.value ?? ''])),
  );
  return (
    <Dialog
      open
      title={form.title}
      onClose={close}
      dismissable={!busy}
      onSubmit={(e) => {
        e.preventDefault();
        void save(values);
      }}
      footer={
        <Button type="submit" variant="primary" loading={busy} disabled={busy}>
          {tr('Save', 'Salva')}
        </Button>
      }
    >
      {error && (
        <div className="el-note is-err" role="alert">
          {error}
        </div>
      )}
      {form.fields
        .filter((f) => !f.provider || f.provider === values.type)
        .map((f) =>
          f.options ? (
            <Select
              key={f.key}
              label={f.label}
              value={values[f.key]}
              options={f.options.map((value) => ({ value, label: value }))}
              onChange={(value) => setValues({ ...values, [f.key]: value })}
            />
          ) : f.multiline ? (
            <Textarea
              key={f.key}
              label={f.label}
              value={values[f.key]}
              rows={6}
              required={f.required}
              onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
            />
          ) : (
            <Input
              key={f.key}
              label={f.label}
              type={f.type ?? 'text'}
              required={f.required}
              value={values[f.key]}
              autoComplete={f.type === 'password' ? 'new-password' : 'off'}
              onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
            />
          ),
        )}
    </Dialog>
  );
}
export function LoadState({
  loading,
  error,
  retry,
  empty,
}: {
  loading: boolean;
  error: unknown;
  retry(): void;
  empty?: boolean;
}) {
  if (loading) return <Skeleton height={120} />;
  if (error)
    return (
      <EmptyState
        title={tr('Could not load', 'Caricamento non riuscito')}
        text={errText(error)}
        action={<Button onClick={retry}>{tr('Retry', 'Riprova')}</Button>}
      />
    );
  return empty ? <p className="el-muted">{tr('No items.', 'Nessun elemento.')}</p> : null;
}
function Row({ title, detail, children }: { title: string; detail?: string; children?: ReactNode }) {
  return (
    <div className="el-admin-row">
      <div className="el-li-t">
        <b>{title}</b>
        {detail && <span>{detail}</span>}
      </div>
      <div className="el-acts">{children}</div>
    </div>
  );
}
const nameField: Field = { key: 'name', label: 'Nome / Name', required: true };
const slugField: Field = { key: 'slug', label: 'Slug', required: true };
const roles = ['viewer', 'analyst', 'developer', 'admin', 'owner'];

export function ConnectionPanel({
  connection,
  setConnection,
  mode,
}: {
  connection: Connection;
  setConnection(c: Connection): void;
  mode: Ctx['mode'];
}) {
  const [remote, setRemote] = useState(connection.kind === 'remote');
  const [url, setUrl] = useState(connection.kind === 'remote' ? connection.url : '');
  const [token, setToken] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <section className="el-card">
      <h3>{tr('Instance connection', 'Connessione istanza')}</h3>
      <Switch
        label={tr('Remote instance with administrator token', 'Istanza remota con token amministratore')}
        checked={remote}
        onChange={setRemote}
      />
      {remote ? (
        <>
          <Input label="URL" type="url" value={url} onChange={(e) => setUrl(e.target.value)} />
          <Input
            label={tr('Administrator token', 'Token amministratore')}
            type="password"
            value={token}
            autoComplete="off"
            onChange={(e) => setToken(e.target.value)}
          />
          <p className="el-muted">
            {tr(
              'Create a token in the web app administration. It stays in memory for this page only.',
              'Crea un token nell’amministrazione web. Resta solo nella memoria di questa pagina.',
            )}
          </p>
        </>
      ) : (
        <p className="el-muted">
          {mode === 'docker'
            ? '/var/lib/docker/volumes/ervisiologs_admin/_data/admin.sock'
            : '/run/ervisio-logs/admin.sock'}
        </p>
      )}
      {error && (
        <div className="el-note is-err" role="alert">
          {error}
        </div>
      )}
      <Button
        loading={busy}
        onClick={() => {
          setBusy(true);
          setError('');
          const next: Connection = remote ? { kind: 'remote', url, token } : { kind: 'local' };
          void Promise.resolve()
            .then(async () => {
              if (next.kind === 'remote') {
                const remoteUrl = new URL(next.url);
                if (remoteUrl.protocol !== 'https:')
                  throw new Error(tr('Use an HTTPS instance URL', 'Usa un URL HTTPS per l’istanza'));
                const approval = await getSdk().network.request(remoteUrl.host);
                if (approval.reloading) return false;
              }
              await adminClient(mode, next).health();
              return true;
            })
            .then((connected) => {
              if (connected) {
                setConnection(next);
                setToken('');
              }
            })
            .catch((e) => setError(errText(e)))
            .finally(() => setBusy(false));
        }}
      >
        {tr('Connect and verify', 'Collega e verifica')}
      </Button>
    </section>
  );
}
export function HealthPanel({ client }: { client: AdminClient }) {
  const state = useLoad(client.health, [client]);
  const h = state.data;
  return (
    <section className="el-card">
      <h3>
        {tr('Ingest and archive', 'Ingresso e archivio')}
        <span className="el-sp" />
        <Button size="sm" onClick={state.reload}>
          {tr('Refresh', 'Aggiorna')}
        </Button>
      </h3>
      <LoadState {...state} retry={state.reload} />
      {h && (
        <>
          <Row title="PostgreSQL" detail={h.postgres.error ?? ''}>
            <Badge tone={h.postgres.ok ? 'ok' : 'err'}>
              {h.postgres.ok ? 'OK' : tr('Unavailable', 'Non disponibile')}
            </Badge>
          </Row>
          <Row title="ClickHouse" detail={h.clickhouse.error ?? ''}>
            <Badge tone={h.clickhouse.ok ? 'ok' : 'err'}>
              {h.clickhouse.ok ? 'OK' : tr('Unavailable', 'Non disponibile')}
            </Badge>
          </Row>
          <Row
            title={tr('Durable ingest queue', 'Coda ingresso persistente')}
            detail={
              h.wal
                ? `${h.wal.bytes.toLocaleString()} / ${h.wal.max_bytes.toLocaleString()} B · ${h.wal.pending_records} ${tr('pending events', 'eventi in attesa')} · ${h.wal.lag_seconds}s`
                : tr('Not configured', 'Non configurata')
            }
          />
          <Row
            title={tr('Archive uploads', 'Invii archivio')}
            detail={`${h.upload.waiting.count} ${tr('waiting', 'in attesa')} · ${h.upload.failed.count} ${tr('failed', 'falliti')}`}
          />
          <Row
            title={tr('Destination', 'Destinazione')}
            detail={
              h.archive.provider
                ? `${h.archive.provider.name} · ${h.archive.provider.status} · ${h.archive.provider.last_error ?? ''}`
                : tr('None active', 'Nessuna attiva')
            }
          />
          <Row
            title={tr('Last verified upload', 'Ultimo invio verificato')}
            detail={h.upload.last_verified_at ? new Date(h.upload.last_verified_at).toLocaleString() : '—'}
          />
          <Row
            title={tr('Oldest event awaiting archive', 'Evento più vecchio da archiviare')}
            detail={
              h.upload.oldest_unarchived_event_at ? new Date(h.upload.oldest_unarchived_event_at).toLocaleString() : '—'
            }
          />
        </>
      )}
    </section>
  );
}
export function RuntimeSettings({ client, scope = '/admin' }: { client: AdminClient; scope?: string }) {
  const state = useLoad(() => client.settings(scope), [client, scope]);
  const [filter, setFilter] = useState('');
  const a = useAdminActions(state.reload);
  return (
    <section className="el-card">
      <h3>{tr('Runtime settings', 'Impostazioni runtime')}</h3>
      <Input
        label={tr('Find setting', 'Cerca impostazione')}
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
      />
      <LoadState {...state} retry={state.reload} />
      {(state.data ?? [])
        .filter((s) => s.key.includes(filter))
        .map((s: Setting) => (
          <Row key={s.key} title={s.key} detail={`${String(s.value)} · ${s.source}${s.locked ? ` · ${s.env}` : ''}`}>
            <Button
              size="sm"
              disabled={s.locked || a.busy}
              onClick={() =>
                a.edit({
                  title: s.key,
                  fields: [
                    {
                      key: 'value',
                      label: s.type === 'duration' ? `${s.key} (${tr('seconds', 'secondi')})` : s.key,
                      value: String(s.value),
                      type: s.type === 'int' || s.type === 'duration' ? 'number' : 'text',
                      options: s.options?.length ? s.options : s.type === 'bool' ? ['true', 'false'] : undefined,
                      required: s.type !== 'string',
                    },
                  ],
                  save: (v) =>
                    client.setSetting(
                      scope,
                      s.key,
                      s.type === 'int' || s.type === 'duration'
                        ? Number(v.value)
                        : s.type === 'bool'
                          ? v.value === 'true'
                          : v.value,
                    ),
                })
              }
            >
              {tr('Edit', 'Modifica')}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={s.locked || a.busy}
              onClick={() => a.execute(() => client.resetSetting(scope, s.key))}
            >
              {tr('Inherit', 'Eredita')}
            </Button>
          </Row>
        ))}
      {a.dialogs}
    </section>
  );
}
export function UsersPanel({ client }: { client: AdminClient }) {
  const [search, setSearch] = useState('');
  const [cursor, setCursor] = useState('');
  const state = useLoad(() => client.users(search, cursor), [client, search, cursor]);
  const [expanded, setExpanded] = useState('');
  const a = useAdminActions(state.reload);
  const edit = (u: User) =>
    a.edit({
      title: u.username,
      fields: [
        { key: 'status', label: tr('Status', 'Stato'), value: u.status, options: ['active', 'disabled'] },
        {
          key: 'admin',
          label: tr('Instance administrator', 'Amministratore istanza'),
          value: String(u.is_instance_admin),
          options: ['false', 'true'],
        },
      ],
      save: (v) => client.updateUser(u.id, { status: v.status, is_instance_admin: v.admin === 'true' }),
    });
  return (
    <section className="el-card">
      <h3>
        {tr('Instance users', 'Utenti istanza')}
        <span className="el-sp" />
        <Button
          onClick={() =>
            a.edit({
              title: tr('Create user', 'Crea utente'),
              fields: [
                { key: 'email', label: 'Email', type: 'email', required: true },
                { key: 'username', label: 'Username', required: true },
                { key: 'password', label: 'Password', type: 'password' },
              ],
              save: (v) => client.createUser({ email: v.email, username: v.username, password: v.password }),
            })
          }
        >
          {tr('Create user', 'Crea utente')}
        </Button>
      </h3>
      <Input
        label={tr('Search users', 'Cerca utenti')}
        value={search}
        onChange={(e) => {
          setSearch(e.target.value);
          setCursor('');
        }}
      />
      <LoadState {...state} retry={state.reload} empty={!state.data?.items?.length} />
      {(state.data?.items ?? []).map((u) => (
        <div key={u.id}>
          <Row title={u.username} detail={`${u.email} · ${u.status}${u.is_instance_admin ? ' · Admin' : ''}`}>
            <Button size="sm" onClick={() => setExpanded(expanded === u.id ? '' : u.id)}>
              {tr('Profile', 'Profilo')}
            </Button>
          </Row>
          {expanded === u.id && (
            <div className="el-admin-detail">
              <div className="el-acts">
                <Button disabled={a.busy} onClick={() => edit(u)}>
                  {tr('Status and role', 'Stato e ruolo')}
                </Button>
                <Button
                  onClick={() =>
                    a.edit({
                      title: tr('Reset password', 'Reimposta password'),
                      fields: [{ key: 'password', label: 'Password', type: 'password', required: true }],
                      save: (v) => client.resetPassword(u.id, v.password),
                    })
                  }
                >
                  {tr('Reset password', 'Reimposta password')}
                </Button>
                <Button
                  variant="danger"
                  onClick={() =>
                    a.confirm(tr('Delete user', 'Elimina utente'), u.username, () => client.deleteUser(u.id))
                  }
                >
                  {tr('Delete', 'Elimina')}
                </Button>
              </div>
              <Sessions client={client} user={u.id} />
            </div>
          )}
        </div>
      ))}
      <div className="el-acts">
        {cursor && <Button onClick={() => setCursor('')}>{tr('First page', 'Prima pagina')}</Button>}
        {state.data?.next_cursor && (
          <Button onClick={() => setCursor(state.data?.next_cursor ?? '')}>
            {tr('Next page', 'Pagina successiva')}
          </Button>
        )}
      </div>
      {a.dialogs}
    </section>
  );
}
function Sessions({ client, user }: { client: AdminClient; user: string }) {
  const state = useLoad(() => client.sessions(user), [client, user]);
  const a = useAdminActions(state.reload);
  return (
    <>
      <h4>{tr('Active sessions', 'Sessioni attive')}</h4>
      <LoadState {...state} retry={state.reload} empty={!state.data?.length} />
      {state.data?.map((s) => (
        <Row
          key={s.id}
          title={s.ip}
          detail={`${s.user_agent} · ${new Date(s.last_seen_at ?? s.created_at).toLocaleString()}`}
        >
          <Button size="sm" onClick={() => a.execute(() => client.revokeSessions(user, s.id))}>
            {tr('Revoke', 'Revoca')}
          </Button>
        </Row>
      ))}
      {!!state.data?.length && (
        <Button
          variant="danger"
          onClick={() =>
            a.confirm(tr('Revoke all sessions', 'Revoca tutte le sessioni'), user, () => client.revokeSessions(user))
          }
        >
          {tr('Revoke all', 'Revoca tutte')}
        </Button>
      )}
      {a.dialogs}
    </>
  );
}
export function WorkspacesPanel({ client }: { client: AdminClient }) {
  const state = useLoad(client.workspaces, [client]);
  const users = useLoad(() => client.users(), [client]);
  const [selected, setSelected] = useState('');
  const a = useAdminActions(state.reload);
  const workspace = state.data?.find((w) => w.id === selected);
  return (
    <>
      <section className="el-card">
        <h3>
          Workspace
          <span className="el-sp" />
          <Button
            onClick={() =>
              a.edit({
                title: tr('Create workspace', 'Crea workspace'),
                fields: [
                  nameField,
                  slugField,
                  { key: 'owner_id', label: tr('Owner user ID', 'ID utente proprietario'), required: true },
                ],
                save: (v) => client.createWorkspace({ name: v.name, slug: v.slug, owner_id: v.owner_id }),
              })
            }
          >
            {tr('Create', 'Crea')}
          </Button>
        </h3>
        <LoadState {...state} retry={state.reload} empty={!state.data?.length} />
        {(state.data ?? []).map((w) => (
          <Row key={w.id} title={w.name} detail={w.slug}>
            <Button size="sm" onClick={() => setSelected(w.id)}>
              {tr('Manage', 'Gestisci')}
            </Button>
            <Button
              size="sm"
              onClick={() =>
                a.edit({
                  title: tr('Rename workspace', 'Rinomina workspace'),
                  fields: [{ ...nameField, value: w.name }],
                  save: (v) => client.renameWorkspace(w.id, v.name),
                })
              }
            >
              {tr('Rename', 'Rinomina')}
            </Button>
            <Button
              size="sm"
              variant="danger"
              onClick={() =>
                a.confirm(
                  tr('Delete workspace and all its projects', 'Elimina workspace e tutti i suoi progetti'),
                  w.slug,
                  () => client.deleteWorkspace(w.id, w.slug),
                )
              }
            >
              {tr('Delete', 'Elimina')}
            </Button>
          </Row>
        ))}
        <details>
          <summary>{tr('User IDs for workspace ownership', 'ID utenti per assegnare il proprietario')}</summary>
          {!!users.error && <p role="alert">{errText(users.error)}</p>}
          {(users.data?.items ?? [])
            .filter((u) => u.status === 'active')
            .map((u) => (
              <Row key={u.id} title={u.username} detail={u.id} />
            ))}
        </details>
        {a.dialogs}
      </section>
      {workspace && (
        <>
          <MembersPanel key={workspace.id} client={client} workspace={workspace} />
          <RuntimeSettings client={client} scope={`/workspaces/${workspace.id}`} />
          <RetentionPanel client={client} scope="workspace" scopeId={workspace.id} />
          <AuditPanel client={client} workspace={workspace.id} />
        </>
      )}
    </>
  );
}
function MembersPanel({ client, workspace }: { client: AdminClient; workspace: Workspace }) {
  const members = useLoad(() => client.members(workspace.id), [client, workspace.id]);
  const invitations = useLoad(() => client.invitations(workspace.id), [client, workspace.id]);
  const a = useAdminActions(() => {
    members.reload();
    invitations.reload();
  });
  const fields: Field[] = [
    { key: 'email', label: 'Email', type: 'email', required: true },
    { key: 'role', label: tr('Role', 'Ruolo'), value: 'viewer', options: roles },
  ];
  return (
    <section className="el-card">
      <h3>
        {workspace.name} · {tr('Members and invitations', 'Membri e inviti')}
      </h3>
      <div className="el-acts">
        <Button
          onClick={() =>
            a.edit({
              title: tr('Add existing user', 'Aggiungi utente esistente'),
              fields,
              save: (v) => client.addMember(workspace.id, v.email, v.role),
            })
          }
        >
          {tr('Add member', 'Aggiungi membro')}
        </Button>
        <Button
          onClick={() =>
            a.edit({
              title: tr('Invite by email', 'Invita tramite email'),
              fields,
              result: true,
              save: async (v) => {
                const r = await client.invite(workspace.id, v.email, v.role);
                return r.url ?? r.mail_error ?? tr('Invitation sent by email.', 'Invito inviato tramite email.');
              },
            })
          }
        >
          {tr('Invite', 'Invita')}
        </Button>
      </div>
      <LoadState {...members} retry={members.reload} empty={!members.data?.length} />
      {members.data?.map((m) => (
        <Row key={m.user_id} title={m.username} detail={`${m.email} · ${m.role}`}>
          <Button
            size="sm"
            onClick={() =>
              a.edit({
                title: `${m.username} · ${tr('Role', 'Ruolo')}`,
                fields: [{ key: 'role', label: tr('Role', 'Ruolo'), value: m.role, options: roles }],
                save: (v) => client.updateMember(workspace.id, m.user_id, v.role),
              })
            }
          >
            {tr('Role', 'Ruolo')}
          </Button>
          <Button
            variant="danger"
            size="sm"
            onClick={() =>
              a.confirm(tr('Remove member', 'Rimuovi membro'), m.username, () =>
                client.removeMember(workspace.id, m.user_id),
              )
            }
          >
            {tr('Remove', 'Rimuovi')}
          </Button>
        </Row>
      ))}
      <h4>{tr('Pending invitations', 'Inviti in attesa')}</h4>
      <LoadState {...invitations} retry={invitations.reload} empty={!invitations.data?.length} />
      {invitations.data?.map((i) => (
        <Row key={i.id} title={i.email} detail={`${i.role} · ${new Date(i.expires_at).toLocaleString()}`}>
          <Button size="sm" onClick={() => a.execute(() => client.revokeInvitation(workspace.id, i.id))}>
            {tr('Revoke', 'Revoca')}
          </Button>
        </Row>
      ))}
      {a.dialogs}
    </section>
  );
}
export function ProjectsPanel({ client }: { client: AdminClient }) {
  const workspaces = useLoad(client.workspaces, [client]);
  const [workspaceId, setWorkspaceId] = useState('');
  const current = workspaceId || workspaces.data?.[0]?.id || '';
  const state = useLoad(() => (current ? client.projects(current) : Promise.resolve([])), [client, current]);
  const [selected, setSelected] = useState('');
  const project = state.data?.find((p) => p.id === selected);
  const a = useAdminActions(state.reload);
  return (
    <>
      <section className="el-card">
        <h3>{tr('Projects', 'Progetti')}</h3>
        <Select
          label="Workspace"
          value={current}
          options={(workspaces.data ?? []).map((w) => ({ value: w.id, label: w.name }))}
          onChange={(v) => {
            setWorkspaceId(v);
            setSelected('');
          }}
        />
        <LoadState {...workspaces} retry={workspaces.reload} empty={!workspaces.data?.length} />
        <Button
          disabled={!current}
          onClick={() =>
            a.edit({
              title: tr('Create project', 'Crea progetto'),
              fields: [nameField, slugField],
              save: (v) => client.createProject({ workspace_id: current, name: v.name, slug: v.slug }),
            })
          }
        >
          {tr('Create project', 'Crea progetto')}
        </Button>
        <LoadState {...state} retry={state.reload} empty={!state.data?.length} />
        {(state.data ?? []).map((p) => (
          <Row key={p.id} title={p.name} detail={p.slug}>
            <Button size="sm" onClick={() => setSelected(p.id)}>
              {tr('Manage', 'Gestisci')}
            </Button>
            <Button
              size="sm"
              onClick={() =>
                a.edit({
                  title: tr('Rename project', 'Rinomina progetto'),
                  fields: [{ ...nameField, value: p.name }],
                  save: (v) => client.renameProject(p.id, v.name),
                })
              }
            >
              {tr('Rename', 'Rinomina')}
            </Button>
            <Button
              size="sm"
              variant="danger"
              onClick={() =>
                a.confirm(tr('Delete project', 'Elimina progetto'), p.slug, () => client.deleteProject(p.id, p.slug))
              }
            >
              {tr('Delete', 'Elimina')}
            </Button>
          </Row>
        ))}
        {a.dialogs}
      </section>
      {project && <ProjectDetail key={project.id} client={client} project={project} />}
    </>
  );
}
function ProjectDetail({ client, project }: { client: AdminClient; project: Project }) {
  const [tab, setTab] = useState('streams');
  const tabs = [
    { value: 'streams', label: tr('Streams', 'Stream') },
    { value: 'environments', label: tr('Environments', 'Ambienti') },
    { value: 'tokens', label: 'Token' },
    { value: 'settings', label: tr('Settings', 'Impostazioni') },
    { value: 'retention', label: tr('Retention', 'Conservazione') },
  ];
  return (
    <>
      <section className="el-card">
        <h3>{project.name}</h3>
        <Select label={tr('Project section', 'Sezione progetto')} value={tab} options={tabs} onChange={setTab} />
      </section>
      {(tab === 'streams' || tab === 'environments') && (
        <MetadataPanel client={client} project={project.id} kind={tab} />
      )}
      {tab === 'tokens' && <TokensPanel client={client} project={project.id} />}
      {tab === 'settings' && <RuntimeSettings client={client} scope={`/projects/${project.id}`} />}
      {tab === 'retention' && <RetentionPanel client={client} scope="project" scopeId={project.id} />}
    </>
  );
}
function MetadataPanel({
  client,
  project,
  kind,
}: {
  client: AdminClient;
  project: string;
  kind: 'streams' | 'environments';
}) {
  const state = useLoad(
    () => (kind === 'streams' ? client.streams(project) : client.environments(project)),
    [client, project, kind],
  );
  const a = useAdminActions(state.reload);
  const metadata = (item?: Environment): Field[] => [
    { key: 'display_name', label: tr('Display name', 'Nome visibile'), value: item?.display_name },
    { key: 'description', label: tr('Description', 'Descrizione'), value: item?.description },
    { key: 'color', label: tr('Color (#rrggbb or empty)', 'Colore (#rrggbb o vuoto)'), value: item?.color },
  ];
  const title = kind === 'streams' ? tr('Streams', 'Stream') : tr('Environments', 'Ambienti');
  return (
    <section className="el-card">
      <h3>
        {title}
        <span className="el-sp" />
        <Button
          onClick={() =>
            a.edit({
              title: `${tr('Create', 'Crea')} · ${title}`,
              fields: [nameField, ...metadata()],
              save: (v) =>
                kind === 'streams'
                  ? client.createStream(project, {
                      name: v.name,
                      display_name: v.display_name,
                      description: v.description,
                      color: v.color,
                    })
                  : client.createEnvironment(project, {
                      name: v.name,
                      display_name: v.display_name,
                      description: v.description,
                      color: v.color,
                    }),
            })
          }
        >
          {tr('Create', 'Crea')}
        </Button>
      </h3>
      <LoadState {...state} retry={state.reload} empty={!state.data?.length} />
      {state.data?.map((item) => (
        <Row key={item.id} title={item.display_name || item.name} detail={`${item.name} · ${item.description ?? ''}`}>
          <Button
            size="sm"
            onClick={() =>
              a.edit({
                title: item.name,
                fields: metadata(item),
                save: (v) => client.updateMetadata(project, kind, item.id, v),
              })
            }
          >
            {tr('Edit', 'Modifica')}
          </Button>
          <Button
            size="sm"
            variant="danger"
            onClick={() =>
              a.confirm(tr('Delete', 'Elimina'), item.name, () => client.deleteMetadata(project, kind, item.id))
            }
          >
            {tr('Delete', 'Elimina')}
          </Button>
        </Row>
      ))}
      {a.dialogs}
    </section>
  );
}
function TokensPanel({ client, project }: { client: AdminClient; project: string }) {
  const state = useLoad(() => client.tokens(project), [client, project]);
  const a = useAdminActions(state.reload);
  return (
    <section className="el-card">
      <h3>
        {tr('Ingest tokens', 'Token ingest')}
        <span className="el-sp" />
        <Button
          onClick={() =>
            a.edit({
              title: tr('Create ingest token', 'Crea token ingest'),
              fields: [
                nameField,
                { key: 'expires_at', label: tr('Expiry (optional)', 'Scadenza (facoltativa)'), type: 'datetime-local' },
              ],
              result: true,
              save: async (v) =>
                (
                  await client.createToken({
                    project_id: project,
                    name: v.name,
                    scopes: ['logs:ingest'],
                    expires_at: v.expires_at ? new Date(v.expires_at).toISOString() : null,
                  })
                ).token,
            })
          }
        >
          {tr('Create', 'Crea')}
        </Button>
      </h3>
      <p className="el-muted">
        {tr(
          'Operators can issue ingest tokens. Log read and export tokens require a workspace member.',
          'Gli operatori possono creare token ingest. Per lettura ed export serve un membro del workspace.',
        )}
      </p>
      <LoadState {...state} retry={state.reload} empty={!state.data?.length} />
      {state.data?.map((token) => (
        <Row
          key={token.id}
          title={token.name}
          detail={`${token.prefix} · ${token.scopes.join(', ')} · ${token.expires_at ? new Date(token.expires_at).toLocaleString() : tr('No expiry', 'Senza scadenza')}`}
        >
          <Badge tone={token.revoked_at ? 'neutral' : 'ok'}>
            {token.revoked_at ? tr('Revoked', 'Revocato') : tr('Active', 'Attivo')}
          </Badge>
          {!token.revoked_at && (
            <Button
              size="sm"
              variant="danger"
              onClick={() =>
                a.confirm(tr('Revoke token', 'Revoca token'), token.name, () => client.revokeToken(token.id))
              }
            >
              {tr('Revoke', 'Revoca')}
            </Button>
          )}
        </Row>
      ))}
      {a.dialogs}
    </section>
  );
}
export function AuditPanel({ client, workspace }: { client: AdminClient; workspace?: string }) {
  const [cursor, setCursor] = useState('');
  const state = useLoad(() => client.audit(workspace, cursor), [client, workspace, cursor]);
  return (
    <section className="el-card">
      <h3>{tr('Activity log', 'Registro attività')}</h3>
      <LoadState {...state} retry={state.reload} empty={!state.data?.items?.length} />
      {state.data?.items?.map((item) => (
        <Row
          key={item.id}
          title={item.action}
          detail={`${item.actor_name} · ${new Date(item.created_at).toLocaleString()} · ${item.target_type} ${item.target_id}`}
        />
      ))}
      <div className="el-acts">
        {cursor && <Button onClick={() => setCursor('')}>{tr('First page', 'Prima pagina')}</Button>}
        {state.data?.next_cursor && (
          <Button onClick={() => setCursor(state.data?.next_cursor ?? '')}>
            {tr('Next page', 'Pagina successiva')}
          </Button>
        )}
      </div>
    </section>
  );
}
export function RetentionPanel({
  client,
  scope = 'instance',
  scopeId = INSTANCE_SCOPE,
}: {
  client: AdminClient;
  scope?: string;
  scopeId?: string;
}) {
  const state = useLoad(client.retention, [client, scope, scopeId]);
  const policy = state.data?.find((r) => r.scope_type === scope && r.scope_id === scopeId);
  const a = useAdminActions(state.reload);
  return (
    <section className="el-card">
      <h3>
        {tr('Retention', 'Conservazione')} · {scope}
      </h3>
      <LoadState {...state} retry={state.reload} />
      <Row
        title={tr('Local / total retention', 'Conservazione locale / totale')}
        detail={`${policy?.local_age_hours ?? tr('Inherited', 'Ereditata')} / ${policy?.global_age_hours ?? tr('Inherited', 'Ereditata')} h`}
      />
      {scope === 'instance' && (
        <Row
          title={tr('Cache budget', 'Budget cache')}
          detail={`${policy?.cache_max_bytes ?? 0} B · ${policy?.low_pct ?? '—'}% / ${policy?.high_pct ?? '—'}%`}
        />
      )}
      <div className="el-acts">
        <Button
          onClick={() =>
            a.edit({
              title: tr('Edit retention', 'Modifica conservazione'),
              fields: [
                {
                  key: 'local_age_hours',
                  label: tr('Local hours (empty inherits)', 'Ore locali (vuoto eredita)'),
                  type: 'number',
                  value: policy?.local_age_hours?.toString(),
                },
                {
                  key: 'global_age_hours',
                  label: tr('Total hours (empty inherits)', 'Ore totali (vuoto eredita)'),
                  type: 'number',
                  value: policy?.global_age_hours?.toString(),
                },
                ...(scope === 'instance'
                  ? [
                      {
                        key: 'cache_max_bytes',
                        label: tr('Cache budget bytes', 'Budget cache byte'),
                        type: 'number',
                        value: policy?.cache_max_bytes?.toString(),
                      },
                      {
                        key: 'high_pct',
                        label: tr('High watermark %', 'Soglia alta %'),
                        type: 'number',
                        value: policy?.high_pct?.toString(),
                      },
                      {
                        key: 'low_pct',
                        label: tr('Low watermark %', 'Soglia bassa %'),
                        type: 'number',
                        value: policy?.low_pct?.toString(),
                      },
                    ]
                  : []),
              ],
              save: (v) => {
                const input: RetentionInput = {
                  local_age_hours: v.local_age_hours ? Number(v.local_age_hours) : null,
                  global_age_hours: v.global_age_hours ? Number(v.global_age_hours) : null,
                };
                if (scope === 'instance') {
                  if (v.cache_max_bytes) input.cache_max_bytes = Number(v.cache_max_bytes);
                  if (v.high_pct) input.high_pct = Number(v.high_pct);
                  if (v.low_pct) input.low_pct = Number(v.low_pct);
                }
                return client.setRetention(scope, scopeId, input);
              },
            })
          }
        >
          {tr('Edit', 'Modifica')}
        </Button>
        {policy && (
          <Button variant="ghost" onClick={() => a.execute(() => client.resetRetention(scope, scopeId))}>
            {tr('Reset to inherited', 'Ripristina ereditata')}
          </Button>
        )}
      </div>
      {a.dialogs}
    </section>
  );
}
function providerFields(provider?: Provider): Field[] {
  const fields: Field[] = [
    { key: 'endpoint', label: 'Endpoint URL', type: 'url', provider: 's3' },
    { key: 'region', label: tr('Region', 'Regione'), value: 'us-east-1', provider: 's3' },
    { key: 'bucket', label: 'Bucket', required: true, provider: 's3' },
    { key: 'prefix', label: tr('Object prefix', 'Prefisso oggetti'), provider: 's3' },
    { key: 'path_style', label: 'Path style', value: 'false', options: ['false', 'true'], provider: 's3' },
    {
      key: 'no_server_checksum',
      label: tr('Verify by reading objects back', 'Verifica rileggendo gli oggetti'),
      value: 'false',
      options: ['false', 'true'],
      provider: 's3',
    },
    { key: 'host', label: 'Host', required: true, provider: 'sftp' },
    { key: 'port', label: tr('Port', 'Porta'), type: 'number', value: '22', required: true, provider: 'sftp' },
    { key: 'user', label: tr('User', 'Utente'), required: true, provider: 'sftp' },
    { key: 'base_dir', label: tr('Base directory', 'Directory base'), required: true, provider: 'sftp' },
    {
      key: 'host_key',
      label: tr('SSH host public key', 'Chiave pubblica SSH del server'),
      required: true,
      provider: 'sftp',
    },
    {
      key: 'auth',
      label: tr('Authentication', 'Autenticazione'),
      value: 'key',
      options: ['key', 'password'],
      provider: 'sftp',
    },
    { key: 'root', label: tr('Absolute directory', 'Directory assoluta'), required: true, provider: 'local' },
  ];
  return fields.map((field) => ({
    ...field,
    value: provider?.config[field.key] === undefined ? field.value : String(provider.config[field.key]),
  }));
}
export function StoragePanel({ client }: { client: AdminClient }) {
  const state = useLoad(client.storage, [client]);
  const secrets = useLoad(client.secrets, [client]);
  const a = useAdminActions(() => {
    state.reload();
    secrets.reload();
  });
  const providerForm = (provider?: Provider) =>
    a.edit({
      title: provider ? tr('Edit destination', 'Modifica destinazione') : tr('Create destination', 'Crea destinazione'),
      fields: [
        { ...nameField, value: provider?.name },
        { key: 'type', label: tr('Type', 'Tipo'), value: provider?.type ?? 's3', options: ['s3', 'sftp', 'local'] },
        ...providerFields(provider),
        {
          key: 'secret_file',
          label: tr('Credential file name', 'Nome file credenziali'),
          value: provider?.secret_file ?? '',
        },
      ],
      save: (v) => {
        if (v.type !== 's3' && v.type !== 'sftp' && v.type !== 'local') throw new Error('Invalid provider type');
        const config: Record<string, unknown> = {};
        for (const field of providerFields()) {
          if (field.provider !== v.type) continue;
          const value = v[field.key];
          config[field.key] =
            field.options && field.key !== 'auth' ? value === 'true' : field.type === 'number' ? Number(value) : value;
        }
        const input: ProviderInput = { name: v.name, type: v.type, config, secret_file: v.secret_file || null };
        return provider ? client.updateProvider(provider.id, input) : client.createProvider(input);
      },
    });
  return (
    <>
      <section className="el-card">
        <h3>
          {tr('Archive destinations', 'Destinazioni archivio')}
          <span className="el-sp" />
          <Button onClick={() => providerForm()}>{tr('Create destination', 'Crea destinazione')}</Button>
        </h3>
        <LoadState {...state} retry={state.reload} empty={!state.data?.providers?.length} />
        {state.data?.providers?.map((p) => (
          <Row key={p.id} title={p.name} detail={`${p.type} · ${p.status} · ${p.last_error ?? ''}`}>
            <Badge tone={p.active ? 'ok' : 'neutral'}>
              {p.active ? tr('Active', 'Attiva') : tr('Inactive', 'Inattiva')}
            </Badge>
            <Button size="sm" onClick={() => providerForm(p)}>
              {tr('Edit', 'Modifica')}
            </Button>
            <Button size="sm" onClick={() => a.execute(() => client.testProvider(p.id))}>
              {tr('Test', 'Verifica')}
            </Button>
            <Button size="sm" onClick={() => a.execute(() => client.activateProvider(p.active ? null : p.id))}>
              {p.active ? tr('Deactivate', 'Disattiva') : tr('Activate', 'Attiva')}
            </Button>
            <Button
              size="sm"
              variant="danger"
              onClick={() =>
                a.confirm(tr('Delete destination', 'Elimina destinazione'), p.name, () => client.deleteProvider(p.id))
              }
            >
              {tr('Delete', 'Elimina')}
            </Button>
          </Row>
        ))}
        {state.data?.usage && (
          <Row
            title={tr('Local usage', 'Uso locale')}
            detail={`${state.data.usage.hot_bytes} B hot · ${state.data.usage.cache_bytes} B cache / ${state.data.usage.max_bytes} B`}
          />
        )}
        <details>
          <summary>{tr('Advanced: archive segments', 'Avanzate: segmenti archivio')}</summary>
          {Object.entries(state.data?.segments ?? {}).map(([status, summary]) => (
            <Row key={status} title={status} detail={`${summary.count} · ${summary.bytes} B`} />
          ))}
        </details>
        <p className="el-muted">
          {tr(
            'S3: endpoint, bucket, region, prefix, path_style. SFTP: host, port, user, base_dir, host_key, auth. Local: root. Credentials are stored separately.',
            'S3: endpoint, bucket, region, prefix, path_style. SFTP: host, port, user, base_dir, host_key, auth. Locale: root. Le credenziali si salvano separatamente.',
          )}
        </p>
        {a.dialogs}
      </section>
      <section className="el-card">
        <h3>
          {tr('Credentials', 'Credenziali')}
          <span className="el-sp" />
          <Button
            onClick={() =>
              a.edit({
                title: tr('Write credential file', 'Scrivi file credenziali'),
                fields: [
                  nameField,
                  {
                    key: 'value',
                    label: tr('Credential content', 'Contenuto credenziali'),
                    required: true,
                    multiline: true,
                  },
                ],
                save: (v) => client.putSecret(v.name, v.value),
              })
            }
          >
            {tr('Write file', 'Scrivi file')}
          </Button>
        </h3>
        <LoadState {...secrets} retry={secrets.reload} empty={!secrets.data?.length} />
        {secrets.data?.map((name) => (
          <Row key={name} title={name} />
        ))}
        <p className="el-muted">
          {tr(
            'Only file names are returned. Existing files with the same name are replaced.',
            'Vengono restituiti solo i nomi. I file con lo stesso nome vengono sostituiti.',
          )}
        </p>
      </section>
      <RetentionPanel client={client} />
    </>
  );
}
