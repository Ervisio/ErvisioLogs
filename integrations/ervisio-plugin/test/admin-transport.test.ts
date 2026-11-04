import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createAdminConnection, httpsOrigin } from '../src/api/admin-transport.ts';
const response = (data: unknown, status=200) => ({status,headers:{},body:JSON.stringify(data),json:()=>data,bytes:()=>new Uint8Array()});
test('socket uses declared API and query separately, without auth headers',async()=>{
  const calls:unknown[][]=[];
  const connection=createAdminConnection(async(...args)=>{calls.push(args);return response({items:[]});});
  connection.connect({type:'docker'});
  await connection.request('GET','/admin/users?q=a%2Bb');
  assert.deepEqual(calls,[['logs-admin-docker',{method:'GET',path:'/api/v1/admin/users',query:'q=a%2Bb'}]]);
  connection.disconnect();await assert.rejects(connection.request('GET','/admin/health'));
});
test('HTTPS restricts origin, omits cookies, refuses redirects, and clears Bearer on disconnect/change',async()=>{
  for(const origin of ['http://example.test','https://user:password@example.test','https://example.test/?token=secret','https://example.test/#token','https://example.test/path'])assert.throws(()=>httpsOrigin(origin));
  const calls:Array<{url:string;init:RequestInit}>=[];let socketCalls=0;
  const fetcher=(async(url:string|URL|Request,init:RequestInit)=>{calls.push({url:String(url),init});return new Response('{}',{status:200});}) as typeof fetch;
  const connection=createAdminConnection(async()=>{socketCalls++;return response({});},fetcher);
  connection.connect({type:'https',origin:'https://example.test',token:'sample-secret'});
  await connection.request('PUT','/admin/settings/auth.open_registration',{value:false});
  assert.equal(calls[0].init.credentials,'omit');assert.equal(calls[0].init.redirect,'error');
  assert.equal((calls[0].init.headers as Record<string,string>).Authorization,'Bearer sample-secret');
  assert.equal(calls[0].init.body,'{"value":false}');assert.equal(socketCalls,0);
  connection.disconnect();await assert.rejects(connection.request('GET','/admin/health'));assert.equal(calls.length,1);
  connection.connect({type:'standalone'});await connection.request('GET','/admin/health');assert.equal(socketCalls,1);assert.equal(calls.length,1);
});
test('HTTPS errors redact the token and disconnect aborts in-flight requests',async()=>{
  const token='sample-private-token';
  const connection=createAdminConnection(async()=>response({}),(async()=>{throw new Error(`request ${token} refused`);}) as typeof fetch);
  connection.connect({type:'https',origin:'https://example.test',token});
  await assert.rejects(connection.request('GET','/admin/health'),error=>error instanceof Error&&!error.message.includes(token)&&error.message.includes('[REDACTED]'));
  let aborted=false;
  const waiting=createAdminConnection(async()=>response({}),(async(_url:unknown,init:RequestInit)=>new Promise((_resolve,reject)=>{init.signal?.addEventListener('abort',()=>{aborted=true;reject(new Error('aborted'));});})) as typeof fetch);
  waiting.connect({type:'https',origin:'https://example.test',token});
  const pending=waiting.request('GET','/admin/health');waiting.disconnect();await assert.rejects(pending);assert.equal(aborted,true);
});
