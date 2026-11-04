import { useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { adminConnection, type AdminClient } from '../../api/admin';
import type { ConnectionTarget } from '../../api/admin-transport';
import { getSdk } from '../../sdk';
import { Badge, Button, EmptyState, Icon, Select } from '../../kit';
import type { Ctx } from '../LogsPage';
import { Address } from '../Address';
import { Console } from '../Console';
import { Overview } from '../Overview';
import { Settings } from '../Settings';
import { Secrets } from '../Secrets';
import { t } from '../util';
import { Archive } from './Archive';
import { Connection } from './Connection';
import { Health } from './Health';
import { RuntimeSettings } from './RuntimeSettings';
import { Tenancy } from './Tenancy';
import { Users } from './Users';
import { mt } from './shared';
const sections = [
  { id: 'overview', label: () => t('nav.overview'), icon: 'overview', local: false }, { id: 'instance', label: () => mt('instance'), icon: 'cog', local: false },
  { id: 'workspaces', label: () => mt('workspaces'), icon: 'layers', local: false }, { id: 'projects', label: () => mt('projects'), icon: 'layers', local: false },
  { id: 'users', label: () => mt('users'), icon: 'user', local: false }, { id: 'archive', label: () => mt('archive'), icon: 'database', local: false },
  { id: 'server', label: () => mt('server'), icon: 'server', local: true }, { id: 'address', label: () => t('nav.address'), icon: 'globe', local: true },
  { id: 'console', label: () => t('nav.console'), icon: 'terminal', local: true },
];
export function Management({ ctx, setup }: { ctx?: Ctx; setup?: ReactNode }) {
  const connection = useMemo(() => adminConnection(), []);
  const [client, setClient] = useState<AdminClient | null>(null), [remote, setRemote] = useState(false), [origin, setOrigin] = useState(''), [generation, setGeneration] = useState(0), [open, setOpen] = useState(false), [section, setSection] = useState('overview');
  useEffect(() => () => connection.disconnect(), [connection]);
  const connect = (target: ConnectionTarget) => { connection.connect(target); setRemote(target.type === 'https'); setOrigin(target.type === 'https' ? target.origin : ''); setClient(connection.client); setGeneration(value => value + 1); };
  const disconnect = () => { connection.disconnect(); setClient(null); setRemote(false); setOrigin(''); setGeneration(value => value + 1); };
  const localCtx = ctx ? { ...ctx, go: (value: Parameters<Ctx['go']>[0]) => setSection(value === 'settings' ? 'server' : value) } : undefined;
  const local = sections.find(item => item.id === section)?.local;
  const webUrl = remote ? origin : ctx?.status.publicUrl;
  let content: ReactNode;
  if (local && (remote || !localCtx)) content = <EmptyState icon="server" title={mt(remote ? 'localOnly' : 'noLocal')} />;
  else if (section === 'server' && localCtx) content = <>
    <div className="el-note">{mt('serverHint')}</div>
    <Settings ctx={localCtx} />
  </>;
  else if (section === 'address' && localCtx) content = <Address ctx={localCtx} />;
  else if (section === 'console' && localCtx) content = <Console ctx={localCtx} />;
  else if (section === 'overview') content = <>{client && <Health client={client} />} {!remote && (localCtx ? <Overview ctx={localCtx} /> : setup)}</>;
  else if (!client) content = <>
    <EmptyState icon="shield" title={mt('connection')} text={mt('disconnected')} action={<Button onClick={() => setOpen(true)}>{mt('connect')}</Button>} />{section === 'archive' && localCtx && <Secrets ctx={localCtx} />}</>;
  else if (section === 'instance') content = <RuntimeSettings client={client} scope={{ type: 'instance' }} />;
  else if (section === 'workspaces' || section === 'projects') content = <Tenancy client={client} projectMode={section === 'projects'} />;
  else if (section === 'users') content = <Users client={client} />;
  else if (section === 'archive') content = <Archive client={client} ctx={remote ? undefined : localCtx} />;
  return <div className="el-root el-management hue-log">
    <header className="el-ph">
      <span className="el-tile">
        <Icon name="logs" />
      </span>
      <div className="el-ph-t">
        <h1>Ervisio Logs</h1>
        <p>{ctx && !remote && <Badge>{t(ctx.mode === 'docker' ? 'mode.docker' : 'mode.standalone')}</Badge>}{client && <span>{remote ? origin : mt('socket')}</span>}{ctx?.status.version && !remote && <span>v{ctx.status.version}</span>}</p>
      </div>
      <Button icon="cog" onClick={() => { disconnect(); setOpen(true); }}>{mt('connection')}</Button>{client && <Button variant="ghost" onClick={disconnect}>{mt('disconnect')}</Button>}{webUrl && <Button icon="externallink" onClick={() => getSdk().openExternal(webUrl)}>{t('act.open')}</Button>}</header>
    <div className="el-mobile-section">
      <Select label={mt('management')} value={section} onChange={setSection} options={sections.map(item => ({ value: item.id, label: item.label() }))} />
    </div>
    <div className="el-inner">
      <nav className="el-nav" aria-label={mt('management')}>{sections.map((item, index) =>
        <div key={item.id}>{(index === 0 || index === 6) && <div className="el-nav-lab">{mt(index === 6 ? 'local' : 'management')}</div>}<button type="button" className="el-nav-it" aria-current={section === item.id ? 'page' : undefined} onClick={() => setSection(item.id)}>
          <span className="el-ki">
            <Icon name={item.icon} />
          </span>{item.label()}</button>
        </div>)}</nav>
      <div className="el-main" key={generation}>
        <h2 className="el-section-title">{sections.find(item => item.id === section)?.label()}</h2>{content}{section === 'overview' && !client && <div className="el-note">
          <span>{mt('disconnected')}</span>
          <Button onClick={() => setOpen(true)}>{mt('connect')}</Button>
        </div>}</div>
    </div>{open && <Connection open mode={ctx?.mode} onClose={() => setOpen(false)} onConnect={connect} />}</div>;
}
