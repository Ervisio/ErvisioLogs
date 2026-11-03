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
  title: string;
  lines: string[];
  running: boolean;
  failed: boolean;
}

export interface Runner {
  job: Job | null;
  busy: boolean;
  /** Runs `fn` with a log callback; resolves to false when it threw (the error is in the log and a toast). */
  run(title: string, fn: (log: (line: string) => void) => Promise<unknown>): Promise<boolean>;
  clear(): void;
}

const MAX_LINES = 2000;

export function useRunner(onDone?: () => void): Runner {
  const [job, setJob] = useState<Job | null>(null);
  const busyRef = useRef(false);
  const doneRef = useRef(onDone);
  doneRef.current = onDone;
  const run = useCallback(async (title: string, fn: (log: (line: string) => void) => Promise<unknown>) => {
    if (busyRef.current) return false;
    busyRef.current = true;
    setJob({ title, lines: [], running: true, failed: false });
    const log = (line: string) =>
      setJob((j) => (j ? { ...j, lines: j.lines.length >= MAX_LINES ? [...j.lines.slice(-MAX_LINES + 1), line] : [...j.lines, line] } : j));
    let ok = true;
    try {
      await fn(log);
    } catch (e) {
      ok = false;
      log(`! ${errText(e)}`);
      getSdk().ui.toast.err(errText(e));
    } finally {
      busyRef.current = false;
      setJob((j) => (j ? { ...j, running: false, failed: !ok } : j));
      doneRef.current?.();
    }
    return ok;
  }, []);
  const clear = useCallback(() => setJob(null), []);
  return { job, busy: !!job?.running, run, clear };
}
