import { useState } from 'react';
import type { AdminClient, ProviderInput, StorageProvider } from '../../api/admin';
import { Badge, Button, ConfirmDialog, Dialog, Input, Select, Textarea } from '../../kit';
import type { Ctx } from '../LogsPage';
import { Secrets } from '../Secrets';
import { useLoad } from '../util';
import { Retention } from './Retention';
import { ErrorNote, LoadingData, mt, useMutation } from './shared';
export function Archive({ client, ctx }: { client: AdminClient; ctx?: Ctx }) {
  const result = useLoad(() => client.storage(), [client]);
  const [edit, setEdit] = useState<StorageProvider | true | null>(null), [remove, setRemove] = useState<StorageProvider | null>(null); const mutation = useMutation();
  return <>
    <section className="el-card">
      <h3>{mt('providers')}<span className="el-sp" />
        <Button variant="primary" onClick={() => setEdit(true)}>{mt('create')}</Button>
      </h3>
      <LoadingData {...result} retry={result.reload} empty={!result.data?.providers?.length} />{result.data?.providers?.map(provider =>
        <div className="el-provider" key={provider.id}>
          <div className="el-li">
            <div className="el-li-t">
              <b>{provider.name}</b>
              <span>{provider.type} · {provider.secret_file ?? '—'}</span>
            </div>
            <Badge tone={provider.status === 'ok' ? 'ok' : provider.status === 'error' ? 'err' : 'neutral'}>{provider.status}</Badge>{provider.active && <Badge tone="ok">{mt('active')}</Badge>}</div>{provider.last_error && <ErrorNote error={new Error(provider.last_error)} />}<div className="el-acts">
            <Button disabled={mutation.busy} onClick={() => setEdit(provider)}>{mt('edit')}</Button>
            <Button disabled={mutation.busy} onClick={() => mutation.run(async () => { await client.testProvider(provider.id); result.reload(); })}>{mt('test')}</Button>
            <Button disabled={mutation.busy} onClick={() => mutation.run(async () => { await client.setActiveProvider(provider.active ? null : provider.id); result.reload(); })}>{mt(provider.active ? 'deactivate' : 'activate')}</Button>
            <Button variant="danger" disabled={mutation.busy} onClick={() => setRemove(provider)}>{mt('remove')}</Button>
          </div>
        </div>)}<ErrorNote error={mutation.error} />{edit && <ProviderForm key={edit === true ? 'new' : edit.id} client={client} provider={edit === true ? undefined : edit} close={() => setEdit(null)} done={result.reload} />}<ConfirmDialog open={!!remove} onClose={() => setRemove(null)} title={mt('remove')} description={mt('deleteWarning')} confirmText={remove?.name} confirmLabel={mt('remove')} danger onConfirm={async () => { if (remove) await mutation.run(async () => { await client.deleteProvider(remove.id); result.reload(); }); setRemove(null); }} />
    </section>{ctx ? <Secrets ctx={ctx} /> : <RemoteSecrets client={client} />}<Retention client={client} type="instance" id="00000000-0000-0000-0000-000000000000" />
  </>;
}
function ProviderForm({ client, provider, close, done }: { client: AdminClient; provider?: StorageProvider; close(): void; done(): void }) {
  const [name, setName] = useState(provider?.name ?? ''), [type, setType] = useState(provider?.type ?? 's3'), [config, setConfig] = useState(JSON.stringify(provider?.config ?? {}, null, 2)), [secret, setSecret] = useState(provider?.secret_file ?? ''); const mutation = useMutation();
  const save = () => mutation.run(async () => { const parsed: unknown = JSON.parse(config); if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(mt('config')); const body: ProviderInput = { name, type, config: parsed as Record<string, unknown>, secret_file: secret || null }; if (provider) await client.updateProvider(provider.id, { name, config: body.config, secret_file: body.secret_file }); else await client.createProvider(body); done(); close(); });
  return <Dialog open onClose={close} title={mt(provider ? 'edit' : 'create')} size="lg" footer={<>
    <Button disabled={mutation.busy} onClick={close}>{mt('cancel')}</Button>
    <Button variant="primary" loading={mutation.busy} disabled={mutation.busy || !name} onClick={save}>{mt('save')}</Button>
  </>}>
    <Input label={mt('name')} maxLength={100} value={name} onChange={event => setName(event.target.value)} />
    <Select label={mt('type')} value={type} disabled={!!provider} onChange={value => setType(value as StorageProvider['type'])} options={['s3', 'sftp', 'local'].map(value => ({ value, label: value }))} />
    <Textarea label={mt('config')} value={config} onChange={event => setConfig(event.target.value)} mono rows={8} hint={mt('configHint')} />
    <Input label={mt('secretFile')} value={secret} maxLength={100} onChange={event => setSecret(event.target.value)} />
    <ErrorNote error={mutation.error} />
  </Dialog>;
}
function RemoteSecrets({ client }: { client: AdminClient }) {
  const result = useLoad(() => client.secrets(), [client]);
  const [name, setName] = useState(''), [value, setValue] = useState(''), [kind, setKind] = useState('password'); const mutation = useMutation();
  return <section className="el-card">
    <h3>{mt('credentials')}</h3>
    <p className="el-muted">{mt('noSecrets')}</p>
    <LoadingData {...result} retry={result.reload} />
    <p className="el-mono">{result.data?.join(', ')}</p>
    <Input label={mt('secretFile')} value={name} maxLength={100} onChange={event => setName(event.target.value)} />
    <Select label={mt('type')} value={kind} onChange={next => { setKind(next); setValue(''); }} options={[{ value: 'password', label: mt('password') }, { value: 'key', label: 'SSH key' }]} />{kind === 'password' ? <Input label={mt('password')} type="password" autoComplete="new-password" value={value} onChange={event => setValue(event.target.value)} /> : <Textarea label="SSH key" rows={6} value={value} onChange={event => setValue(event.target.value)} />}<Button variant="primary" disabled={mutation.busy || !value || !/^[A-Za-z0-9_-][A-Za-z0-9._-]{0,99}$/.test(name)} onClick={() => mutation.run(async () => { await client.putSecret(name, value); setValue(''); result.reload(); })}>{mt('save')}</Button>
    <ErrorNote error={mutation.error} />
  </section>;
}
