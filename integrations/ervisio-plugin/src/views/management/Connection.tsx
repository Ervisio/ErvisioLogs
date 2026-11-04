import { useEffect, useRef, useState } from 'react';
import { getSdk } from '../../sdk';
import { httpsOrigin, type ConnectionTarget } from '../../api/admin-transport';
import { Button, Dialog, Input, Segmented } from '../../kit';
import { mt, ErrorNote } from './shared';
export function Connection({ open, onClose, onConnect, mode }: { open: boolean; onClose(): void; onConnect(target: ConnectionTarget): void; mode?: 'docker' | 'standalone' }) {
  const [type, setType] = useState('socket'), [local, setLocal] = useState(mode ?? 'docker');
  const [origin, setOrigin] = useState(''), [token, setToken] = useState(''), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(null);
  const cancelled = useRef(false);
  useEffect(() => () => { cancelled.current = true; }, []);
  const close = () => { cancelled.current = true; setToken(''); setError(null); onClose(); };
  const connect = async () => {
    setBusy(true); setError(null);
    try {
      if (type === 'https') {
        const url = httpsOrigin(origin);
        if (!token.trim()) throw new Error(mt('tokenRequired'));
        const network = await getSdk().network.request(new URL(url).host, { scheme: 'https' });
        if (cancelled.current) return;
        if (network.reloading) { setToken(''); throw new Error(mt('remoteReload')); }
        onConnect({ type: 'https', origin: url, token });
      } else onConnect({ type: local });
      close();
    } catch (failure) { setToken(''); setError(new Error(failure instanceof Error && failure.message === mt('remoteReload') ? mt('remoteReload') : mt('connectFailed'))); } finally { setBusy(false); }
  };
  return <Dialog open={open} onClose={close} title={mt('connection')} footer={<>
    <Button disabled={busy} onClick={close}>{mt('cancel')}</Button>
    <Button variant="primary" loading={busy} disabled={busy} onClick={connect}>{mt('connect')}</Button>
  </>}>
    <Segmented value={type} onChange={value => { setType(value); setToken(''); setError(null); }} options={[{ value: 'socket', label: mt('socket') }, { value: 'https', label: mt('remote') }]} />{type === 'socket' ? <>
      <Segmented value={local} onChange={value => setLocal(value as 'docker' | 'standalone')} options={[{ value: 'docker', label: 'Docker Compose' }, { value: 'standalone', label: 'systemd' }]} />
      <p className="el-muted">{mt('socketHint')}</p>
    </> : <>
      <Input label={mt('origin')} type="url" value={origin} placeholder="https://logs.example.com" onChange={event => setOrigin(event.target.value)} />
      <Input label={mt('adminToken')} type="password" autoComplete="off" value={token} onChange={event => setToken(event.target.value)} />
      <p className="el-muted">{mt('ephemeral')}</p>
    </>}<ErrorNote error={error} />
  </Dialog>;
}
