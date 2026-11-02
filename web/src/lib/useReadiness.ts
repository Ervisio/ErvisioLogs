import { useEffect, useState } from 'react';

export type CheckState = 'ok' | 'fail';
export type Readiness =
  | { state: 'loading' }
  | { state: 'unreachable' }
  | { state: 'ready' | 'not_ready'; checks: Record<string, CheckState> };

/** Polls GET /readyz. A 503 still carries a body with per-check results. */
export function useReadiness(intervalMs = 10_000): Readiness {
  const [value, setValue] = useState<Readiness>({ state: 'loading' });
  useEffect(() => {
    let alive = true;
    const ctrl = new AbortController();
    const poll = async () => {
      try {
        const res = await fetch('/readyz', { signal: ctrl.signal });
        const body = (await res.json()) as { status: 'ready' | 'not_ready'; checks: Record<string, CheckState> };
        if (alive) setValue({ state: body.status, checks: body.checks });
      } catch {
        if (alive && !ctrl.signal.aborted) setValue({ state: 'unreachable' });
      }
    };
    void poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      alive = false;
      ctrl.abort();
      clearInterval(id);
    };
  }, [intervalMs]);
  return value;
}
