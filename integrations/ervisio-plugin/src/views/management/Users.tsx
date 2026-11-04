import { useState } from 'react';
import type { AdminClient, User } from '../../api/admin';
import { Badge, Button, ConfirmDialog, Dialog, Input, Select, Switch } from '../../kit';
import { useLoad } from '../util';
import { ErrorNote, LoadingData, mt, useMutation } from './shared';
export function Users({ client }: { client: AdminClient }) {
  const [query, setQuery] = useState(''), [search, setSearch] = useState(''), [cursor, setCursor] = useState<string>(), [selected, setSelected] = useState<User | null>(null), [creating, setCreating] = useState(false);
  const result = useLoad(() => client.adminUsers.list(search, cursor), [client, search, cursor]);
  return <>
    <form className="el-acts" onSubmit={event => { event.preventDefault(); setCursor(undefined); setSearch(query); }}>
      <Input label={mt('search')} value={query} onChange={event => setQuery(event.target.value)} />
      <Button type="submit">{mt('search')}</Button>
      <span className="el-sp" />
      <Button variant="primary" onClick={() => setCreating(true)}>{mt('create')}</Button>
    </form>
    <LoadingData {...result} retry={result.reload} empty={result.data?.items?.length === 0} />{result.data?.items?.map(user =>
      <div key={user.id} className="el-li">
        <div className="el-li-t">
          <b>{user.username}</b>
          <span>{user.email}</span>
        </div>
        <Badge tone={user.status === 'active' ? 'ok' : 'warn'}>{user.status === 'active' ? mt('active') : mt('disabled')}</Badge>{user.is_instance_admin && <Badge>{mt('admin')}</Badge>}<Button onClick={() => setSelected(user)}>{mt('edit')}</Button>
      </div>)}<div className="el-acts">{cursor && <Button onClick={() => setCursor(undefined)}>{mt('first')}</Button>}{result.data?.next_cursor && <Button onClick={() => setCursor(result.data!.next_cursor!)}>{mt('next')}</Button>}</div>{creating && <CreateUser client={client} close={() => setCreating(false)} done={result.reload} />} {selected && <UserDetail key={selected.id} client={client} user={selected} close={() => setSelected(null)} done={result.reload} />}</>;
}
function CreateUser({ client, close, done }: { client: AdminClient; close(): void; done(): void }) {
  const [email, setEmail] = useState(''), [username, setUsername] = useState(''), [password, setPassword] = useState(''); const mutation = useMutation();
  const save = () => mutation.run(async () => { await client.adminUsers.create({ email, username, ...(password ? { password } : {}) }); setPassword(''); done(); close(); });
  return <Dialog open onClose={() => { setPassword(''); close(); }} title={mt('create')} footer={<>
    <Button disabled={mutation.busy} onClick={close}>{mt('cancel')}</Button>
    <Button variant="primary" disabled={!email || !username || mutation.busy} loading={mutation.busy} onClick={save}>{mt('create')}</Button>
  </>}>
    <Input label={mt('email')} type="email" value={email} maxLength={254} onChange={event => setEmail(event.target.value)} />
    <Input label={mt('username')} value={username} minLength={3} maxLength={32} onChange={event => setUsername(event.target.value)} />
    <Input label={mt('password')} type="password" autoComplete="new-password" value={password} minLength={12} onChange={event => setPassword(event.target.value)} hint={mt('createPasswordHint')} />
    <ErrorNote error={mutation.error} />
  </Dialog>;
}
function UserDetail({ client, user, close, done }: { client: AdminClient; user: User; close(): void; done(): void }) {
  const [status, setStatus] = useState(user.status), [admin, setAdmin] = useState(user.is_instance_admin), [password, setPassword] = useState(''), [remove, setRemove] = useState(false), [showSessions, setShowSessions] = useState(false);
  const mutation = useMutation();
  const changed = () => { done(); };
  const action = (fn: () => Promise<unknown>) => mutation.run(async () => { await fn(); changed(); });
  return <>
    <Dialog open onClose={() => { setPassword(''); close(); }} title={user.username} size="lg" footer={<>
      <Button disabled={mutation.busy} onClick={close}>{mt('cancel')}</Button>
      <Button variant="primary" loading={mutation.busy} disabled={mutation.busy} onClick={() => action(() => client.adminUsers.update(user.id, { status: status as 'active' | 'disabled', is_instance_admin: admin }))}>{mt('save')}</Button>
    </>}>
      <p className="el-muted">{user.email}</p>
      <code className="el-mono">{user.id}</code>
      <Select label={mt('status')} value={status} onChange={setStatus} options={[{ value: 'active', label: mt('active') }, { value: 'disabled', label: mt('disabled') }]} />
      <Switch checked={admin} onChange={setAdmin} label={mt('admin')} />
      <div className="el-acts">
        <Button disabled={mutation.busy} onClick={() => setShowSessions(!showSessions)}>{mt('sessions')}</Button>
        <Button variant="danger" disabled={mutation.busy} onClick={() => setRemove(true)}>{mt('remove')}</Button>
      </div>
      <Input label={mt('password')} type="password" minLength={12} autoComplete="new-password" value={password} onChange={event => setPassword(event.target.value)} hint={mt('passwordHint')} />
      <Button disabled={mutation.busy || password.length < 12} onClick={() => mutation.run(async () => { await client.adminUsers.resetPassword(user.id, password); setPassword(''); done(); })}>{mt('resetPassword')}</Button>{showSessions && <Sessions client={client} user={user} />}<ErrorNote error={mutation.error} />
    </Dialog>
    <ConfirmDialog open={remove} onClose={() => setRemove(false)} title={mt('remove')} description={mt('deleteWarning')} confirmText={user.username} confirmLabel={mt('remove')} danger onConfirm={async () => { if (await mutation.run(() => client.adminUsers.remove(user.id))) { done(); close(); } else setRemove(false); }} />
  </>;
}
function Sessions({ client, user }: { client: AdminClient; user: User }) {
  const result = useLoad(() => client.adminUsers.sessions(user.id), [client, user.id]); const mutation = useMutation();
  const revoke = (id?: string) => mutation.run(async () => { if (id) await client.adminUsers.revokeSession(user.id, id); else await client.adminUsers.revokeSessions(user.id); result.reload(); });
  return <section className="el-card">
    <h3>{mt('sessions')}</h3>
    <LoadingData {...result} retry={result.reload} empty={!result.data?.length} />
    <ErrorNote error={mutation.error} />{result.data?.map(session =>
      <div className="el-li" key={session.id}>
        <div className="el-li-t">
          <b>{session.ip}</b>
          <span>{session.user_agent}</span>
          <span>{new Date(session.created_at).toLocaleString()} Â· {new Date(session.expires_at).toLocaleString()}</span>
        </div>{session.current ? <Badge>{mt('sessionCurrent')}</Badge> : <Button disabled={mutation.busy} onClick={() => revoke(session.id)}>{mt('revoke')}</Button>}</div>)}{!!result.data?.length && <Button variant="danger" disabled={mutation.busy} onClick={() => revoke()}>{mt('revokeAll')}</Button>}</section>;
}
