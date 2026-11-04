import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createAdminClient, type RequestTransport } from '../src/index.ts';
test('settings encode scope and key, preserve typed values and AbortSignal', async () => {
  const calls: unknown[][] = [];
  const request: RequestTransport = async <T>(...args: unknown[]) => { calls.push(args); return { items: null } as T; };
  const client = createAdminClient(request), signal = new AbortController().signal;
  assert.deepEqual(await client.settings({ type: 'project', id: 'a/b' }, signal), []);
  await client.putSetting({ type: 'instance' }, 'a/b', false, signal);
  await client.resetSetting({ type: 'workspace', id: 'w ?' }, 'ingest.max_streams', signal);
  assert.deepEqual(calls, [['GET', '/projects/a%2Fb/settings', undefined, signal], ['PUT', '/admin/settings/a%2Fb', { value: false }, signal], ['DELETE', '/workspaces/w%20%3F/settings/ingest.max_streams', undefined, signal]]);
});
test('admin user protocol preserves pagination, password body and individual sessions', async () => {
  const calls: unknown[][] = [];
  const request: RequestTransport = async <T>(...args: unknown[]) => { calls.push(args); return { items: [] } as T; };
  const users = createAdminClient(request).adminUsers;
  await users.list('a+b@example.test', 'x/y'); await users.resetPassword('a/b', 'sample-password'); await users.revokeSession('a/b', 'x y');
  assert.deepEqual(calls, [['GET','/admin/users?q=a%2Bb%40example.test&cursor=x%2Fy',undefined,undefined],['PUT','/admin/users/a%2Fb/password',{password:'sample-password'},undefined],['DELETE','/admin/users/a%2Fb/sessions/x%20y',undefined,undefined]]);
});
test('transport owns authentication and its errors propagate unchanged', async () => {
  const error = Object.assign(new Error('conflict'), { status: 409 });
  const request: RequestTransport = async () => { throw error; };
  await assert.rejects(createAdminClient(request).adminUsers.remove('id'), candidate => candidate === error);
  assert.equal('token' in createAdminClient(request), false);
});
test('tenancy rename sends only name, deletion includes typed slug confirmation, metadata has no stream type', async () => {
  const calls: unknown[][] = [];
  const request: RequestTransport = async <T>(...args: unknown[]) => { calls.push(args); return undefined as T; };
  const client=createAdminClient(request);
  await client.updateWorkspace('w',{name:'Renamed'});
  await client.updateProject('p',{name:'Project'});
  await client.deleteWorkspace('w','workspace-slug');
  await client.deleteProject('p','project-slug');
  await client.createStream('p',{name:'players',display_name:'Giocatori'});
  assert.deepEqual(calls,[['PATCH','/workspaces/w',{name:'Renamed'}],['PATCH','/projects/p',{name:'Project'}],['DELETE','/workspaces/w',{confirm:'workspace-slug'}],['DELETE','/projects/p',{confirm:'project-slug'}],['POST','/projects/p/streams',{name:'players',display_name:'Giocatori'}]]);
});
