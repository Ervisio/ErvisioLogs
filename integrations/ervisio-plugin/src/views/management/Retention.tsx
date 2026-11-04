import { useEffect, useState } from 'react';
import type { AdminClient, RetentionScope } from '../../api/admin';
import { Button, Input } from '../../kit';
import { useLoad } from '../util';
import { ErrorNote, LoadingData, mt, useMutation } from './shared';
export function Retention({ client, type, id }: { client: AdminClient; type: RetentionScope; id: string }) {
  const result = useLoad(() => client.retention(), [client]);
  const [local, setLocal] = useState(''), [global, setGlobal] = useState(''); const mutation = useMutation();
  const [cache, setCache] = useState(''), [high, setHigh] = useState(''), [low, setLow] = useState('');
  const policy = result.data?.find(item => item.scope_type === type && item.scope_id === id);
  useEffect(() => { setLocal(policy?.local_age_hours == null ? '' : String(policy.local_age_hours)); setGlobal(policy?.global_age_hours == null ? '' : String(policy.global_age_hours)); setCache(policy?.cache_max_bytes == null ? '' : String(policy.cache_max_bytes)); setHigh(policy?.high_pct == null ? '' : String(policy.high_pct)); setLow(policy?.low_pct == null ? '' : String(policy.low_pct)); }, [policy]);
  return <section className="el-card">
    <h3>{mt('retention')}</h3>
    <LoadingData {...result} retry={result.reload} />{result.data && <>
      <Input label={mt('localHours')} type="number" min={1} max={87600} value={local} onChange={event => setLocal(event.target.value)} />
      <Input label={mt('globalHours')} type="number" min={1} max={87600} value={global} onChange={event => setGlobal(event.target.value)} />{type === 'instance' && <>
        <Input label={mt('cacheBytes')} type="number" min={1048576} value={cache} onChange={event => setCache(event.target.value)} />
        <Input label={mt('highPct')} type="number" min={1} max={100} value={high} onChange={event => setHigh(event.target.value)} />
        <Input label={mt('lowPct')} type="number" min={1} max={99} value={low} onChange={event => setLow(event.target.value)} />
      </>}<div className="el-acts">
        <Button variant="primary" disabled={mutation.busy || (type === 'instance' && (!cache || !high || !low || !local || !global))} onClick={() => mutation.run(async () => { await client.putRetention(type, id, { local_age_hours: local === '' ? null : Number(local), global_age_hours: global === '' ? null : Number(global), ...(type === 'instance' ? { cache_max_bytes: Number(cache), high_pct: Number(high), low_pct: Number(low) } : {}) }); result.reload(); })}>{mt('save')}</Button>{policy && type !== 'instance' && <Button disabled={mutation.busy} onClick={() => mutation.run(async () => { await client.deleteRetention(type, id); result.reload(); })}>{mt('reset')}</Button>}</div>
    </>}<ErrorNote error={mutation.error} />
  </section>;
}
