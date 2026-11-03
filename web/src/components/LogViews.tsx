import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Bookmark, Download, Trash2 } from 'lucide-react';
import { api, ApiError, type ExportRequest, type Project, type SavedFilter, type SavedQuery } from '../lib/api';
import { cn } from '../lib/cn';

const CONTROL = 'bg-sunk border-raised text-ink hover:bg-raised flex h-11 items-center gap-1.5 rounded-row border px-3 font-semibold whitespace-nowrap disabled:opacity-50 md:h-10';

/** Saved views and export for the log explorer. `current` is the filter on screen. */
export default function LogViews({
  project,
  current,
  onApply,
  exportRequest,
}: {
  project: Project;
  current: SavedFilter;
  onApply: (view: SavedFilter) => void;
  exportRequest: (format: ExportRequest['format']) => ExportRequest;
}) {
  const { t } = useTranslation();
  const [views, setViews] = useState<SavedQuery[]>([]);
  const [picked, setPicked] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null);
  const menu = useRef<HTMLDetailsElement>(null);

  useEffect(() => {
    const ctrl = new AbortController();
    api
      .savedQueries(project.id, ctrl.signal)
      .then(setViews)
      // The explorer works without saved views; a failed list only hides them.
      .catch(() => !ctrl.signal.aborted && setViews([]));
    return () => ctrl.abort();
  }, [project.id, attempt]);

  const error = useCallback((err: unknown, fallback: string) => setMessage({ ok: false, text: err instanceof ApiError && err.status < 500 ? err.message : t(fallback) }), [t]);

  const pick = (id: string) => {
    setPicked(id);
    setMessage(null);
    const view = views.find((v) => v.id === id);
    if (view) onApply(view.query);
  };

  const save = async () => {
    const name = window.prompt(t('views.namePrompt'))?.trim();
    if (!name) return;
    setBusy(true);
    setMessage(null);
    try {
      const view = await api.createSavedQuery({ project_id: project.id, name, query: current });
      setPicked(view.id);
      setAttempt((n) => n + 1);
      setMessage({ ok: true, text: t('views.saved', { name }) });
    } catch (err) {
      error(err, 'views.saveFailed');
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    const view = views.find((v) => v.id === picked);
    if (!view || !window.confirm(t('views.deleteConfirm', { name: view.name }))) return;
    setBusy(true);
    setMessage(null);
    try {
      await api.deleteSavedQuery(view.id);
      setPicked('');
      setAttempt((n) => n + 1);
    } catch (err) {
      error(err, 'views.deleteFailed');
    } finally {
      setBusy(false);
    }
  };

  const download = async (format: ExportRequest['format']) => {
    if (menu.current) menu.current.open = false;
    setBusy(true);
    setMessage(null);
    try {
      const blob = await api.exportLogs(exportRequest(format));
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${project.slug}-${new Date().toISOString().replace(/[:.]/g, '-')}.${format === 'csv' ? 'csv' : 'ndjson'}`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (err) {
      error(err, 'views.exportFailed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      {views.length > 0 && (
        <>
          <label className="sr-only" htmlFor="log-view">
            {t('views.label')}
          </label>
          <select id="log-view" value={picked} onChange={(e) => pick(e.target.value)} className="bg-sunk border-raised text-ink h-11 max-w-[14rem] rounded-row border px-2 md:h-10">
            <option value="">{t('views.choose')}</option>
            {views.map((v) => (
              <option key={v.id} value={v.id}>
                {v.name}
              </option>
            ))}
          </select>
          {picked && (
            <button type="button" onClick={() => void remove()} disabled={busy} aria-label={t('views.delete')} title={t('views.delete')} className={cn(CONTROL, 'px-2.5')}>
              <Trash2 className="size-4" aria-hidden />
            </button>
          )}
        </>
      )}
      <button type="button" onClick={() => void save()} disabled={busy} className={CONTROL}>
        <Bookmark className="size-4" aria-hidden />
        {t('views.save')}
      </button>
      <details ref={menu} className="relative">
        <summary className={cn(CONTROL, 'cursor-pointer list-none [&::-webkit-details-marker]:hidden', busy && 'pointer-events-none opacity-50')}>
          {busy ? <span className="spinner" aria-hidden /> : <Download className="size-4" aria-hidden />}
          {t('views.export')}
        </summary>
        <div className="bg-raised border-line absolute right-0 z-30 mt-1 grid min-w-[12rem] rounded-row border p-1 shadow-lg">
          <button type="button" onClick={() => void download('ndjson')} className="hover:bg-sunk h-11 rounded-[5px] px-3 text-left md:h-9">
            {t('views.exportNdjson')}
          </button>
          <button type="button" onClick={() => void download('csv')} className="hover:bg-sunk h-11 rounded-[5px] px-3 text-left md:h-9">
            {t('views.exportCsv')}
          </button>
        </div>
      </details>
      {message && (
        <p role={message.ok ? 'status' : 'alert'} className={cn('basis-full text-[13px]', message.ok ? 'text-ink2' : 'text-err')}>
          {message.text}
        </p>
      )}
    </>
  );
}
