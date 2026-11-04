import { useState } from 'react';
import type { AdminClient, Environment } from '../../api/admin';
import { Button, ConfirmDialog, Dialog, Input } from '../../kit';
import { useLoad } from '../util';
import { ErrorNote, LoadingData, mt, useMutation } from './shared';
export function ProjectMetadata({ client, project, kind }: { client: AdminClient; project: string; kind: 'streams' | 'environments' }) {
  const result = useLoad(() => kind === 'streams' ? client.streams(project) : client.environments(project), [client, project, kind]);
  const [edit, setEdit] = useState<Environment | true | null>(null), [remove, setRemove] = useState<Environment | null>(null); const mutation = useMutation();
  return <section className="el-card">
    <h3>{mt(kind)}<span className="el-sp" />
      <Button onClick={() => setEdit(true)}>{mt('create')}</Button>
    </h3>
    <LoadingData {...result} retry={result.reload} empty={!result.data?.length} />{result.data?.map(item =>
      <div className="el-li" key={item.id}>
        <div className="el-li-t">
          <b>{item.display_name || item.name}</b>
          <span>{item.name} · {item.description}</span>
        </div>
        <Button onClick={() => setEdit(item)}>{mt('edit')}</Button>
        <Button variant="danger" disabled={mutation.busy} onClick={() => setRemove(item)}>{mt('remove')}</Button>
      </div>)}<ErrorNote error={mutation.error} />{edit && <MetadataForm key={edit === true ? 'new' : edit.id} client={client} project={project} kind={kind} item={edit === true ? undefined : edit} close={() => setEdit(null)} done={result.reload} />}<ConfirmDialog open={!!remove} onClose={() => setRemove(null)} title={mt('remove')} description={mt('deleteWarning')} confirmText={remove?.name} confirmLabel={mt('remove')} danger onConfirm={async () => { if (remove) await mutation.run(async () => { if (kind === 'streams') await client.deleteStream(project, remove.id); else await client.deleteEnvironment(project, remove.id); result.reload(); }); setRemove(null); }} />
  </section>;
}
function MetadataForm({ client, project, kind, item, close, done }: { client: AdminClient; project: string; kind: 'streams' | 'environments'; item?: Environment; close(): void; done(): void }) {
  const [name, setName] = useState(item?.name ?? ''), [display, setDisplay] = useState(item?.display_name ?? ''), [description, setDescription] = useState(item?.description ?? ''), [color, setColor] = useState(item?.color ?? ''); const mutation = useMutation();
  const save = () => mutation.run(async () => { const body = { display_name: display, description, color }; if (item) { if (kind === 'streams') await client.updateStream(project, item.id, body); else await client.updateEnvironment(project, item.id, body); } else if (kind === 'streams') await client.createStream(project, { name, ...body }); else await client.createEnvironment(project, { name, ...body }); done(); close(); });
  return <Dialog open onClose={close} title={mt(item ? 'edit' : 'create')} footer={<>
    <Button disabled={mutation.busy} onClick={close}>{mt('cancel')}</Button>
    <Button variant="primary" loading={mutation.busy} disabled={mutation.busy || !name} onClick={save}>{mt('save')}</Button>
  </>}>
    <Input label={mt('name')} value={name} disabled={!!item} onChange={event => setName(event.target.value)} />
    <Input label={mt('displayName')} value={display} maxLength={100} onChange={event => setDisplay(event.target.value)} />
    <Input label={mt('description')} value={description} maxLength={500} onChange={event => setDescription(event.target.value)} />
    <Input label={mt('color')} value={color} placeholder="#ffb547" onChange={event => setColor(event.target.value)} />
    <ErrorNote error={mutation.error} />
  </Dialog>;
}
