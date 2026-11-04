import { useState } from 'react';
import type { AdminClient, SettingEntry, SettingsScope, SettingValue } from '../../api/admin';
import { Badge, Button, Input, Select, Switch, Tabs } from '../../kit';
import { t, useLoad } from '../util';
import { ErrorNote, LoadingData, mt, useMutation } from './shared';
import { Audit } from './Audit';
const groupOf = (key: string) => key.startsWith('auth.') ? 'access' : key.startsWith('email.') ? 'email' : key.startsWith('ingest.') ? 'ingest' : 'advanced';
export function RuntimeSettings({ client, scope }: { client: AdminClient; scope: SettingsScope }) {
  const data = useLoad(() => client.settings(scope), [client, JSON.stringify(scope)]);
  const [group, setGroup] = useState('access');
  const [draft, setDraft] = useState<Record<string, SettingValue>>({});
  const mutation = useMutation();
  if (!data.data) return <LoadingData {...data} retry={data.reload} />;
  const groups = ['access', 'email', 'ingest', 'advanced'].filter(value => data.data!.some(entry => groupOf(entry.key) === value));
  if (scope.type === 'instance') groups.push('audit');
  const active = groups.includes(group) ? group : groups[0];
  const save = () => mutation.run(async () => { for (const [key, value] of Object.entries(draft)) { await client.putSetting(scope, key, value); setDraft(current => { const next = { ...current }; delete next[key]; return next; }); } data.reload(); });
  const reset = (entry: SettingEntry) => mutation.run(async () => { await client.resetSetting(scope, entry.key); setDraft(current => { const next = { ...current }; delete next[entry.key]; return next; }); data.reload(); });
  return <>
    <Badge tone="info">{mt('runtime')}</Badge>
    <Tabs items={groups.map(id => ({ id, label: mt(id) }))} value={active} onChange={setGroup} aria-label={mt('instance')} />{active === 'audit' ? <Audit client={client} /> : <>
      <ErrorNote error={mutation.error || data.error} />
      <section className="el-card">
        <h3>{mt(active)}</h3>
        <div className="el-sets">{data.data.filter(entry => groupOf(entry.key) === active).map(entry => {
          const value = draft[entry.key] ?? entry.value, disabled = entry.locked || mutation.busy;
          const change = (next: SettingValue) => setDraft(current => ({ ...current, [entry.key]: next }));
          const label = t(`runtime.${entry.key}`);
          return <div className="el-set el-runtime-row" key={entry.key}>
            <span className="el-set-t">
              <b>{label}</b>
              <code>{entry.key}</code>
              <span>{entry.type === 'duration' ? `${mt('seconds')} · ` : ''}{entry.min !== undefined ? `${entry.min} – ${entry.max}` : ''}</span>
              <span>{entry.locked ? mt('locked') : `${mt('source')}: ${mt(entry.source)}`}{entry.env && <code>{entry.env}</code>}</span>
            </span>
            <span className="el-set-c">
              <span className="el-runtime-control">{entry.type === 'bool' ? <Switch checked={Boolean(value)} disabled={disabled} onChange={change} aria-label={label} /> : entry.type === 'choice' ? <Select label={label} options={(entry.options ?? []).map(option => ({ value: option, label: option }))} value={String(value)} onChange={change} disabled={disabled} /> : <Input aria-label={label} type={entry.type === 'int' || entry.type === 'duration' ? 'number' : 'text'} min={entry.min} max={entry.max} step={1} maxLength={entry.type === 'string' ? entry.max : undefined} value={String(value)} disabled={disabled} onChange={event => change(entry.type === 'int' || entry.type === 'duration' ? Number(event.target.value) : event.target.value)} />}<Button size="sm" variant="ghost" disabled={disabled || entry.source === 'default'} onClick={() => reset(entry)}>{mt('reset')}</Button>
              </span>
            </span>
          </div>;
        })}</div>
      </section>
      <div className="el-savebar">
        <span className="el-muted">{Object.keys(draft).length ? mt('changed') : mt('runtime')}</span>
        <span className="el-sp" />
        <Button disabled={mutation.busy || !Object.keys(draft).length} onClick={() => setDraft({})}>{mt('cancel')}</Button>
        <Button variant="primary" loading={mutation.busy} disabled={mutation.busy || !Object.keys(draft).length} onClick={save}>{mt('save')}</Button>
      </div>
    </>}</>;
}
