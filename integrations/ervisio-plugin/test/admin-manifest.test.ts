import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const manifest=JSON.parse(readFileSync(new URL('../plugin/manifest.json',import.meta.url),'utf8'));
test('admin socket capabilities admit management endpoints and reject logs and session-only admin tokens',()=>{
  const uuid='11111111-1111-4111-8111-111111111111';
  for(const name of ['logs-admin-docker','logs-admin-system']) {
    const api=manifest.capabilities.http.find((item:{name:string})=>item.name===name);
    const allowed=(method:string,path:string)=>api.rules.some((rule:{methods:string[];path:string})=>rule.methods.includes(method)&&new RegExp(`^(?:${rule.path})$`).test(path));
    for(const [method,path] of [['GET','/admin/health'],['PUT','/admin/settings/auth.open_registration'],['DELETE',`/admin/users/${uuid}/sessions/${uuid}`],['PATCH',`/projects/${uuid}/streams/${uuid}`],['POST','/tokens'],['PUT',`/admin/retention/project/${uuid}`]])assert.equal(allowed(method,`/api/v1${path}`),true,`${name} ${method} ${path}`);
    for(const [method,path] of [['POST','/logs/search'],['GET','/logs/tail'],['POST','/logs/export'],['POST',`/projects/${uuid}/purges`],['POST','/admin/tokens'],['GET','/auth/me'],['GET','/admin/users/../../logs/search'],['PUT','/admin/settings/password?token=x']])assert.equal(allowed(method,`/api/v1${path}`),false,`${name} ${method} ${path}`);
    assert.deepEqual(api.headers,['Content-Type']);assert.equal(api.admin,true);
  }
});
