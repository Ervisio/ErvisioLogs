/** Small helpers shared by the views: translation, a loader hook, clipboard, and a streamed console. */
import { useCallback, useEffect, useRef, useState } from 'react';
import { getSdk } from '../sdk';

export const t = (key: string, vars?: Record<string, string | number>) => getSdk().t(key, vars);

export const errText = (e: unknown) => (e as Error)?.message ?? String(e);

/** The host of a URL, for display. */
export function hostOf(url: string | null | undefined): string | null {
  if (!url) return null;
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

export async function copy(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export interface Loaded<T> {
  data: T | undefined;
  error: unknown;
  loading: boolean;
  reload(): void;
}

/** Runs `fn` on mount and on reload(); keeps the last data while reloading. */
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[] = []): Loaded<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  useEffect(() => {
    let live = true;
    setLoading(true);
    fnRef.current().then(
      (d) => {
        if (!live) return;
        setData(d);
        setError(null);
        setLoading(false);
      },
      (e) => {
        if (!live) return;
        setError(e);
        setLoading(false);
      },
    );
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tick, ...deps]);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  return { data, error, loading, reload };
}

/** The output of the last action, shared by every section (Console shows it in full). */
export interface Job {
  id: string;
  title: string;
  lines: string[];
  running: boolean;
  failed: boolean;
}

export interface Runner {
  job: Job | null;
  history: Job[];
  busy: boolean;
  /** Runs `fn` with a log callback; resolves to false when it threw (the error is in the log and a toast). */
  run(title: string, fn: (log: (line: string) => void) => Promise<unknown>): Promise<boolean>;
  clear(): void;
}

const MAX_LINES = 2000;

export function useRunner(onDone?: () => void): Runner {
  const [job, setJob] = useState<Job | null>(null);
  const [history, setHistory] = useState<Job[]>([]);
  const busyRef = useRef(false);
  const doneRef = useRef(onDone);
  doneRef.current = onDone;
  const run = useCallback(async (title: string, fn: (log: (line: string) => void) => Promise<unknown>) => {
    if (busyRef.current) return false;
    busyRef.current = true;
    let current: Job = { id: crypto.randomUUID(), title, lines: [], running: true, failed: false };
    setJob(current);
    const log = (line: string) => {
      current = { ...current, lines: current.lines.length >= MAX_LINES ? [...current.lines.slice(-MAX_LINES + 1), line] : [...current.lines, line] };
      setJob(current);
    };
    let ok = true;
    try {
      await fn(log);
    } catch (e) {
      ok = false;
      log(`! ${errText(e)}`);
      getSdk().ui.toast.err(errText(e));
    } finally {
      busyRef.current = false;
      const completed = { ...current, running: false, failed: !ok };
      setJob(completed);
      setHistory(previous => [completed, ...previous].slice(0, 20));
      doneRef.current?.();
    }
    return ok;
  }, []);
  const clear = useCallback(() => { setJob(null); setHistory([]); }, []);
  return { job, history, busy: !!job?.running, run, clear };
}
