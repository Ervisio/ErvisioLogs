import { useState } from 'react';
import type { AdminClient, ApiToken } from '../../api/admin';
import { Badge, Button, ConfirmDialog, Dialog, Input } from '../../kit';
import { copy, t, useLoad } from '../util';
import { getSdk } from '../../sdk';
import { ErrorNote, LoadingData, mt, useMutation } from './shared';
export function ProjectTokens({ client, project }: { client: AdminClient; project: string }) {
  const result = useLoad(() => client.tokens(project), [client, project]);
  const [name, setName] = useState(''), [expires, setExpires] = useState(''), [token, setToken] = useState(''), [remove, setRemove] = useState<ApiToken | null>(null); const mutation = useMutation();
  return <section className="el-card">
    <h3>{mt('tokens')}</h3>
    <p className="el-muted">{mt('tokenHint')}</p>
    <LoadingData {...result} retry={result.reload} empty={!result.data?.length} />{result.data?.map(item =>
      <div className="el-li" key={item.id}>
        <div className="el-li-t">
          <b>{item.name}</b>
          <span>{item.prefix} · {item.scopes.join(', ')} · {item.expires_at ? new Date(item.expires_at).toLocaleString() : '—'}</span>
        </div>{item.revoked_at ? <Badge>{mt('disabled')}</Badge> : <Button variant="danger" disabled={mutation.busy} onClick={() => setRemove(item)}>{mt('revoke')}</Button>}</div>)}<Input label={mt('name')} maxLength={100} value={name} onChange={event => setName(event.target.value)} />
    <Input label={mt('expires')} type="datetime-local" value={expires} onChange={event => setExpires(event.target.value)} />
    <Button variant="primary" disabled={mutation.busy || !name} onClick={() => mutation.run(async () => { const created = await client.createToken({ project_id: project, name, scopes: ['logs:ingest'], ...(expires ? { expires_at: new Date(expires).toISOString() } : {}) }); setToken(created.token); setName(''); setExpires(''); result.reload(); })}>{mt('create')}</Button>
    <ErrorNote error={mutation.error} />
    <Dialog open={!!token} onClose={() => setToken('')} title={mt('newToken')} footer={<Button onClick={() => setToken('')}>{mt('confirm')}</Button>}>
      <Input label={mt('tokens')} type="password" readOnly value={token} autoComplete="off" />
      <Button onClick={async () => { const ok = await copy(token); getSdk().ui.toast[ok ? 'ok' : 'err'](t(ok ? 'copied' : 'copyFail')); }}>{t('act.copy')}</Button>
    </Dialog>
    <ConfirmDialog open={!!remove} onClose={() => setRemove(null)} title={mt('revoke')} description={remove?.name} confirmLabel={mt('revoke')} danger onConfirm={async () => { if (remove) await mutation.run(async () => { await client.revokeToken(remove.id); result.reload(); }); setRemove(null); }} />
  </section>;
}
