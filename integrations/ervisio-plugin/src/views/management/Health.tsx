import type { AdminClient } from '../../api/admin';
import { Badge, Button, Icon } from '../../kit';
import { useLoad } from '../util';
import { ErrorNote, LoadingData, mt } from './shared';
const stamp = (value: string | null) => value ? new Date(value).toLocaleString() : mt('unavailable');
export function Health({ client }: { client: AdminClient }) {
  const result = useLoad(() => client.health(), [client]);
  if (!result.data) return <LoadingData {...result} retry={result.reload} />;
  const health = result.data;
  return <section className="el-card">
    <h3>
      <Icon name="overview" />{mt('health')}<span className="el-sp" />
      <Badge tone={health.status === 'ok' ? 'ok' : 'warn'}>{health.status}</Badge>
      <Button size="sm" onClick={result.reload}>{mt('reload')}</Button>
    </h3>
    <ErrorNote error={result.error} />
    <p className="el-muted">{mt('healthStatus')}</p>
    <div className="el-health-grid">
      <div>
        <span>{mt('wal')}</span>
        <b>{health.wal ? `${health.wal.bytes.toLocaleString()} / ${health.wal.max_bytes.toLocaleString()} B` : mt('unavailable')}</b>
        <small>{mt('pending')}: {health.wal?.pending_records ?? '—'}</small>
      </div>
      <div>
        <span>{mt('uploads')}</span>
        <b>{health.upload.waiting.count}</b>
        <small>{health.upload.waiting.bytes.toLocaleString()} B</small>
      </div>
      <div>
        <span>{mt('failed')}</span>
        <b>{health.upload.failed.count}</b>
        <small>{health.upload.failed.bytes.toLocaleString()} B</small>
      </div>
      <div>
        <span>{mt('archive')}</span>
        <b>{health.archive.provider?.name ?? mt('unavailable')}</b>
        <small>{health.archive.provider?.status ?? mt('disabled')}</small>
      </div>
    </div>
    <p className="el-muted">{mt('verified')}: {stamp(health.upload.last_verified_at)}<br />{mt('oldest')}: {stamp(health.upload.oldest_unarchived_event_at)}</p>
    <div className="el-acts">
      <Badge tone={health.postgres.ok ? 'ok' : 'err'}>PostgreSQL</Badge>
      <Badge tone={health.clickhouse.ok ? 'ok' : 'err'}>ClickHouse</Badge>
    </div>{[health.postgres.error, health.clickhouse.error, health.archive.provider?.last_error].filter(Boolean).map((error, index) =>
      <ErrorNote key={index} error={new Error(error!)} />)}</section>;
}
