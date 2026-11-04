import { useState } from 'react';
import type { AdminClient } from '../../api/admin';
import { Button } from '../../kit';
import { useLoad } from '../util';
import { LoadingData, mt } from './shared';
export function Audit({ client, workspace }: { client: AdminClient; workspace?: string }) {
  const [cursor, setCursor] = useState<string>();
  const result = useLoad(() => client.audit(workspace, cursor), [client, workspace, cursor]);
  return <section className="el-card">
    <h3>{mt('audit')}<span className="el-sp" />
      <Button onClick={result.reload}>{mt('reload')}</Button>
    </h3>
    <LoadingData {...result} retry={result.reload} empty={!result.data?.items?.length} />{result.data?.items?.map(item =>
      <div className="el-li" key={item.id}>
        <div className="el-li-t">
          <b>{item.action}</b>
          <span>{new Date(item.created_at).toLocaleString()} · {item.actor_name || item.actor_user_id || '—'}</span>
          <span>{item.target_type} · {item.target_id}</span>
          <code className="el-audit-detail">{JSON.stringify(item.detail)}</code>
        </div>
      </div>)}<div className="el-acts">{cursor && <Button onClick={() => setCursor(undefined)}>{mt('first')}</Button>}{result.data?.next_cursor && <Button onClick={() => setCursor(result.data!.next_cursor!)}>{mt('next')}</Button>}</div>
  </section>;
}
