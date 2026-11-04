import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Check, Copy } from 'lucide-react';
import { api, ApiError, tailUrl, type Project } from '../lib/api';
import { cn } from '../lib/cn';

type Step = 'token' | 'send' | 'waiting';

/**
 * Onboarding for a project with no events: create an ingest token, send one event, see it arrive.
 * The steps are a real sequence, so they carry numbers.
 */
export default function FirstEvent({ project, onEvent }: { project: Project; onEvent: () => void }) {
  const { t } = useTranslation();
  const [token, setToken] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sent, setSent] = useState(false);
  const [copied, setCopied] = useState(false);

  const step: Step = !token ? 'token' : sent ? 'waiting' : 'send';
  const origin = window.location.origin;
  const curl = `curl -X POST ${origin}/api/v1/ingest \\
  -H "Authorization: Bearer ${token ?? '<token>'}" \\
  -H "Content-Type: application/json" \\
  -d '{"level":"info","event":"server.start","message":"Hello from my server"}'`;

  // Any event that reaches the project ends the wizard, whether it came from this page or from curl.
  useEffect(() => {
    if (!token || typeof EventSource === 'undefined') return;
    const src = new EventSource(tailUrl({ project_id: project.id }));
    src.addEventListener('log', () => onEvent());
    return () => src.close();
  }, [token, project.id, onEvent]);

  const create = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await api.createToken({ project_id: project.id, name: t('first.tokenName', { date: new Date().toISOString().slice(0, 19).replace('T', ' ') }), scopes: ['logs:ingest'] });
      setToken(res.token);
    } catch (err) {
      setError(err instanceof ApiError && err.status === 403 ? t('first.forbidden') : t('first.tokenFailed'));
    } finally {
      setBusy(false);
    }
  };

  const sendTest = async () => {
    if (!token) return;
    setBusy(true);
    setError(null);
    try {
      // The token authenticates; the session cookie stays out so the call matches what a server sends.
      const res = await fetch('/api/v1/ingest', {
        method: 'POST',
        credentials: 'omit',
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ level: 'info', event: 'ervisio.test', message: t('first.testMessage') }),
      });
      if (res.status !== 202) throw new Error(String(res.status));
      setSent(true);
    } catch {
      setError(t('first.sendFailed'));
    } finally {
      setBusy(false);
    }
  };

  const copy = () => void navigator.clipboard?.writeText(curl).then(() => setCopied(true));

  const stepClass = (s: Step) => cn('grid grid-cols-[28px_minmax(0,1fr)] gap-3', step !== s && 'text-ink2');
  const num = (n: number, done: boolean) => (
    <span aria-hidden className={cn('grid size-7 place-items-center rounded-full text-[13px] font-bold', done ? 'bg-accent text-accent-ink' : 'bg-raised')}>
      {done ? <Check className="size-4" /> : n}
    </span>
  );

  return (
    <section aria-labelledby="first-title" className="mx-auto grid max-w-[620px] grid-cols-[minmax(0,1fr)] gap-6 p-6 pt-[6vh]">
      <div>
        <h2 id="first-title" className="mb-1 text-xl font-bold">
          {t('first.title', { project: project.name })}
        </h2>
        <p className="text-ink2">{t('first.text')}</p>
      </div>

      <ol className="grid gap-6">
        <li className={stepClass('token')}>
          {num(1, !!token)}
          <div className="grid gap-2">
            <h3 className="font-semibold">{t('first.step1')}</h3>
            {!token ? (
              <>
                <p className="text-ink2">{t('first.step1Text')}</p>
                <button type="button" onClick={() => void create()} disabled={busy} className="bg-accent text-accent-ink flex h-11 w-fit items-center gap-2 rounded-row px-4 font-bold whitespace-nowrap hover:brightness-110 disabled:opacity-60">
                  {busy && <span className="spinner" aria-hidden />}
                  {t('first.createToken')}
                </button>
              </>
            ) : (
              <p className="text-ink2">
                {t('first.tokenReady')}{' '}
                <Link to={`/projects/${project.id}/tokens`} className="text-hue font-semibold underline underline-offset-2">
                  {t('first.manageTokens')}
                </Link>
              </p>
            )}
          </div>
        </li>

        <li className={stepClass('send')}>
          {num(2, sent)}
          <div className="grid min-w-0 gap-2">
            <h3 className="font-semibold">{t('first.step2')}</h3>
            {token && (
              <>
                <p className="text-ink2">{t('first.step2Text')}</p>
                <div className="bg-sunk border-raised relative rounded-row border">
                  <pre className="overflow-x-auto p-3 pr-24 font-mono text-[12px] leading-relaxed">{curl}</pre>
                  <button type="button" onClick={copy} className="bg-raised text-ink absolute top-2 right-2 flex h-9 items-center gap-1.5 rounded-row px-2.5 text-[13px] whitespace-nowrap">
                    <Copy className="size-3.5" aria-hidden />
                    {copied ? t('detail.copied') : t('first.copy')}
                  </button>
                </div>
                <p className="text-ink2 text-[13px]">{t('first.tokenOnce')}</p>
                {!sent && (
                  <button type="button" onClick={() => void sendTest()} disabled={busy} className="text-hue h-11 w-fit font-semibold underline underline-offset-2 disabled:opacity-60">
                    {t('first.sendTest')}
                  </button>
                )}
              </>
            )}
          </div>
        </li>

        <li className={stepClass('waiting')}>
          {num(3, false)}
          <div className="grid gap-2">
            <h3 className="font-semibold">{t('first.step3')}</h3>
            {token && (
              <p role="status" className="text-ink2 flex items-center gap-2">
                <span className="spinner" aria-hidden />
                {t('first.waiting')}
              </p>
            )}
          </div>
        </li>
      </ol>
      {error && (
        <p role="alert" className="text-err">
          {error}
        </p>
      )}
    </section>
  );
}
