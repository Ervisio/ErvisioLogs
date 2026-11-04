import { useRef, useState } from 'react';
import { Button, EmptyState, Icon, Skeleton } from '../../kit';
import { errText, t } from '../util';
export const mt = (key: string) => t(`manage.${key}`);
export function ErrorNote({ error }: { error: unknown }) {
  return error ? <div className="el-note is-err" role="alert">
    <Icon name="alert" />
    <span>{errText(error)}</span>
  </div> : null;
}
export function LoadingData({ loading, error, retry, empty }: { loading: boolean; error: unknown; retry(): void; empty?: boolean }) {
  if (loading) return <Skeleton height={160} />;
  if (error) return <>
    <ErrorNote error={error} />
    <Button onClick={retry}>{t('retry')}</Button>
  </>;
  return empty ? <EmptyState icon="layers" title={mt('empty')} /> : null;
}
export function useMutation() {
  const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(null);
  const pending = useRef(false);
  const run = async (action: () => Promise<unknown>) => {
    if (pending.current) return false;
    pending.current = true;
    setBusy(true); setError(null);
    try { await action(); return true; } catch (failure) { setError(failure); return false; } finally { pending.current = false; setBusy(false); }
  };
  return { busy, error, run, clear: () => setError(null) };
}
