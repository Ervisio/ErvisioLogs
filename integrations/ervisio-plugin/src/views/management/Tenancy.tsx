import { useState } from 'react';
import type { AdminClient, Project, Workspace } from '../../api/admin';
import { Button, ConfirmDialog, Dialog, Input, Select, Tabs } from '../../kit';
import { useLoad } from '../util';
import { ErrorNote, LoadingData, mt, useMutation } from './shared';
import { RuntimeSettings } from './RuntimeSettings';
import { Retention } from './Retention';
import { ProjectMetadata } from './ProjectMetadata';
import { ProjectTokens } from './ProjectTokens';
import { Audit } from './Audit';
export function Tenancy({ client, projectMode }: { client: AdminClient; projectMode: boolean }) {
  const workspaces = useLoad(() => client.workspaces(), [client]);
  const [selected, setSelected] = useState(''), [creating, setCreating] = useState(false);
  const workspace = workspaces.data?.find(item => item.id === selected) ?? workspaces.data?.[0];
  return <>
    <div className="el-acts">
      <Select label={mt('workspace')} value={workspace?.id ?? ''} onChange={setSelected} options={(workspaces.data ?? []).map(item => ({ value: item.id, label: item.name }))} />{!projectMode && <Button variant="primary" onClick={() => setCreating(true)}>{mt('create')}</Button>}</div>
    <LoadingData {...workspaces} retry={workspaces.reload} empty={!workspaces.data?.length} />{workspace && (projectMode ? <Projects key={workspace.id} client={client} workspace={workspace} /> : <WorkspaceDetail key={workspace.id} client={client} workspace={workspace} done={workspaces.reload} />)}{creating && <CreateScope client={client} close={() => setCreating(false)} done={workspaces.reload} />}</>;
}
function CreateScope({ client, workspace, close, done }: { client: AdminClient; workspace?: Workspace; close(): void; done(): void }) {
  const [name, setName] = useState(''), [slug, setSlug] = useState(''), [owner, setOwner] = useState(''); const mutation = useMutation();
  return <Dialog open onClose={close} title={`${mt('create')} ${mt(workspace ? 'project' : 'workspace')}`} footer={<>
    <Button onClick={close} disabled={mutation.busy}>{mt('cancel')}</Button>
    <Button variant="primary" disabled={mutation.busy || !name || (!workspace && !owner)} loading={mutation.busy} onClick={() => mutation.run(async () => { if (workspace) await client.createProject({ workspace_id: workspace.id, name, slug }); else await client.createWorkspace({ name, slug, owner_id: owner }); done(); close(); })}>{mt('create')}</Button>
  </>}>
    <Input label={mt('name')} value={name} maxLength={100} onChange={event => setName(event.target.value)} />
    <Input label={mt('slug')} value={slug} maxLength={39} onChange={event => setSlug(event.target.value)} />{!workspace && <Input label={mt('owner')} value={owner} onChange={event => setOwner(event.target.value)} hint={mt('ownerHint')} />}<ErrorNote error={mutation.error} />
  </Dialog>;
}
function Identity({ client, item, project, done }: { client: AdminClient; item: Workspace | Project; project?: boolean; done(): void }) {
  const [name, setName] = useState(item.name), [remove, setRemove] = useState(false); const mutation = useMutation();
  return <section className="el-card">
    <h3>{mt('name')}</h3>
    <Input label={mt('name')} value={name} maxLength={100} onChange={event => setName(event.target.value)} />
    <Input label={mt('slug')} value={item.slug} readOnly />
    <div className="el-acts">
      <Button variant="primary" disabled={mutation.busy || !name} onClick={() => mutation.run(async () => { if (project) await client.updateProject(item.id, { name }); else await client.updateWorkspace(item.id, { name }); done(); })}>{mt('save')}</Button>
      <Button variant="danger" disabled={mutation.busy} onClick={() => setRemove(true)}>{mt('remove')}</Button>
    </div>
    <ErrorNote error={mutation.error} />
    <ConfirmDialog open={remove} onClose={() => setRemove(false)} title={`${mt('remove')} ${item.name}`} description={mt('deleteScopeWarning')} confirmText={item.slug} confirmLabel={mt('remove')} danger onConfirm={async () => { await mutation.run(async () => { if (project) await client.deleteProject(item.id, item.slug); else await client.deleteWorkspace(item.id, item.slug); done(); }); setRemove(false); }} />
  </section>;
}
function WorkspaceDetail({ client, workspace, done }: { client: AdminClient; workspace: Workspace; done(): void }) {
  const [tab, setTab] = useState('general');
  return <>
    <Tabs items={['general', 'members', 'ingest', 'retention', 'audit'].map(id => ({ id, label: id === 'general' ? mt('name') : mt(id) }))} value={tab} onChange={setTab} />{tab === 'general' && <Identity client={client} item={workspace} done={done} />} {tab === 'members' && <Members client={client} workspace={workspace} />} {tab === 'ingest' && <RuntimeSettings key={workspace.id} client={client} scope={{ type: 'workspace', id: workspace.id }} />}{tab === 'retention' && <Retention client={client} type="workspace" id={workspace.id} />} {tab === 'audit' && <Audit client={client} workspace={workspace.id} />}</>;
}
function Projects({ client, workspace }: { client: AdminClient; workspace: Workspace }) {
  const result = useLoad(() => client.projects(workspace.id), [client, workspace.id]);
  const [selected, setSelected] = useState(''), [creating, setCreating] = useState(false);
  const project = result.data?.find(item => item.id === selected) ?? result.data?.[0];
  return <>
    <div className="el-acts">
      <Select label={mt('project')} value={project?.id ?? ''} onChange={setSelected} options={(result.data ?? []).map(item => ({ value: item.id, label: item.name }))} />
      <Button variant="primary" onClick={() => setCreating(true)}>{mt('create')}</Button>
    </div>
    <LoadingData {...result} retry={result.reload} empty={!result.data?.length} />{project && <ProjectDetail key={project.id} client={client} project={project} done={result.reload} />} {creating && <CreateScope client={client} workspace={workspace} close={() => setCreating(false)} done={result.reload} />}</>;
}
function ProjectDetail({ client, project, done }: { client: AdminClient; project: Project; done(): void }) {
  const [tab, setTab] = useState('general');
  return <>
    <Tabs items={['general', 'environments', 'streams', 'tokens', 'ingest', 'retention'].map(id => ({ id, label: id === 'general' ? mt('name') : mt(id) }))} value={tab} onChange={setTab} />{tab === 'general' && <Identity client={client} item={project} project done={done} />} {(tab === 'environments' || tab === 'streams') && <ProjectMetadata key={tab} client={client} project={project.id} kind={tab} />} {tab === 'tokens' && <ProjectTokens client={client} project={project.id} />} {tab === 'ingest' && <RuntimeSettings client={client} scope={{ type: 'project', id: project.id }} />} {tab === 'retention' && <Retention client={client} type="project" id={project.id} />}</>;
}
function Members({ client, workspace }: { client: AdminClient; workspace: Workspace }) {
  const result = useLoad(() => client.members(workspace.id), [client, workspace.id]);
  const [email, setEmail] = useState(''), [role, setRole] = useState('viewer'), [remove, setRemove] = useState<{ user_id: string; username: string } | null>(null); const mutation = useMutation();
  const roles = ['owner', 'admin', 'developer', 'analyst', 'viewer'];
  const [roleDraft, setRoleDraft] = useState<Record<string, string>>({});
  return <section className="el-card">
    <h3>{mt('members')}</h3>
    <LoadingData {...result} retry={result.reload} empty={!result.data?.length} />{result.data?.map(member =>
      <div className="el-li" key={member.user_id}>
        <div className="el-li-t">
          <b>{member.username}</b>
          <span>{member.email}</span>
          <span className="el-mono">{member.user_id}</span>
        </div>
        <Select label={mt('role')} value={roleDraft[member.user_id] ?? member.role} options={roles.map(value => ({ value, label: value }))} disabled={mutation.busy} onChange={next => setRoleDraft(current => ({ ...current, [member.user_id]: next }))} />
        <Button disabled={mutation.busy || !roleDraft[member.user_id] || roleDraft[member.user_id] === member.role} onClick={() => mutation.run(async () => { await client.setMemberRole(workspace.id, member.user_id, roleDraft[member.user_id]); setRoleDraft(current => { const next = { ...current }; delete next[member.user_id]; return next; }); result.reload(); })}>{mt('save')}</Button>
        <Button variant="danger" disabled={mutation.busy} onClick={() => setRemove(member)}>{mt('remove')}</Button>
      </div>)}<Input label={mt('email')} type="email" value={email} onChange={event => setEmail(event.target.value)} />
    <Select label={mt('role')} value={role} options={roles.map(value => ({ value, label: value }))} onChange={setRole} />
    <Button disabled={mutation.busy || !email} onClick={() => mutation.run(async () => { await client.addMember(workspace.id, { email, role }); setEmail(''); result.reload(); })}>{mt('add')}</Button>
    <ErrorNote error={mutation.error} />
    <ConfirmDialog open={!!remove} onClose={() => setRemove(null)} title={mt('remove')} description={remove?.username} confirmLabel={mt('remove')} danger onConfirm={async () => { if (remove) await mutation.run(async () => { await client.removeMember(workspace.id, remove.user_id); result.reload(); }); setRemove(null); }} />
  </section>;
}
