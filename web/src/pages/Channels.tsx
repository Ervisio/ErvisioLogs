import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { useOutletContext } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Webhook } from 'lucide-react';
import type { ShellContext } from '../components/Shell';
import { api, ApiError, type ChannelFormat, type NotificationChannel, type Workspace } from '../lib/api';
import { useAuth } from '../lib/auth';
import { cn } from '../lib/cn';
import { CopyButton } from './Tokens';

const FORMATS: ChannelFormat[] = ['ervisio', 'slack', 'discord'];
/** Roles holding alerts:manage. */
const MANAGERS = new Set(['owner', 'admin', 'developer']);

type Loaded = { status: 'ready'; channels: NotificationChannel[] } | { status: 'failed'; forbidden: boolean };

const INPUT = 'bg-sunk border-field placeholder:text-ink3 h-12 rounded-row border px-3 text-base md:h-11 md:text-sm';
const ROW_BTN = 'hover:bg-sunk h-11 rounded-row px-3 font-semibold disabled:opacity-60 md:h-9';

export default function Channels({ workspace }: { workspace: Workspace }) {
  const { t, i18n } = useTranslation();
  const { retry } = useAuth();
  const { topbar } = useOutletContext<ShellContext>();
  const [attempt, setAttempt] = useState(0);
  const [loaded, setLoaded] = useState<{ key: string; result: Loaded } | null>(null);
  const [secret, setSecret] = useState<{ name: string; value: string } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null);
  const canManage = MANAGERS.has(workspace.role);
  const key = `${workspace.id}:${attempt}`;

  useEffect(() => {
    const ctrl = new AbortController();
    api
      .channels(workspace.id, ctrl.signal)
      .then((channels) => setLoaded({ key, result: { status: 'ready', channels } }))
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) retry();
        setLoaded({ key, result: { status: 'failed', forbidden: err instanceof ApiError && err.status === 403 } });
      });
    return () => ctrl.abort();
  }, [key, workspace.id, retry]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const result = loaded?.key === key ? loaded.result : null;
  const channels = result?.status === 'ready' ? result.channels : [];
  const when = (iso: string) => new Date(iso).toLocaleString(i18n.language, { dateStyle: 'medium', timeStyle: 'short' });

  const act = async (ch: NotificationChannel, run: () => Promise<void>, failed: string) => {
    setBusy(ch.id);
    setNotice(null);
    try {
      await run();
    } catch (err) {
      setNotice({ ok: false, text: err instanceof ApiError && err.status < 500 ? err.message : t(failed) });
    } finally {
      setBusy(null);
      reload();
    }
  };
  const test = (ch: NotificationChannel) =>
    act(
      ch,
      async () => {
        const res = await api.testChannel(ch.id);
        setNotice(res.ok ? { ok: true, text: t('channels.testSent', { name: ch.name }) } : { ok: false, text: t('channels.testFailed', { name: ch.name, error: res.error ?? '' }) });
      },
      'channels.testError',
    );
  const rotate = (ch: NotificationChannel) => {
    if (!window.confirm(t('channels.rotateConfirm', { name: ch.name }))) return;
    void act(
      ch,
      async () => {
        const res = await api.rotateChannelSecret(ch.id);
        if (res.secret) setSecret({ name: ch.name, value: res.secret });
      },
      'channels.saveFailed',
    );
  };
  const toggle = (ch: NotificationChannel) => act(ch, () => api.updateChannel(ch.id, { enabled: !ch.enabled }).then(() => undefined), 'channels.saveFailed');
  const remove = (ch: NotificationChannel) => {
    if (window.confirm(t('channels.deleteConfirm', { name: ch.name }))) void act(ch, () => api.deleteChannel(ch.id), 'channels.deleteFailed');
  };

  return (
    <>
      {topbar(<h1 className="truncate text-base font-bold">{t('nav.integrations')}</h1>)}
      <div className="flex-1 overflow-auto p-4 md:p-6">
        <div className="mx-auto grid max-w-[760px] grid-cols-[minmax(0,1fr)] gap-8">
          <p className="text-ink2 max-w-[60ch]">{t('channels.intro')}</p>

          {secret && (
            <div role="status" className="border-accent grid gap-2 rounded-row border p-4">
              <p className="font-semibold">{t('channels.secretTitle', { name: secret.name })}</p>
              <p className="text-ink2 text-[13px]">{t('channels.secretText')}</p>
              <div className="bg-sunk flex items-center gap-2 rounded-row p-2">
                <code className="min-w-0 flex-1 truncate font-mono text-[13px]">{secret.value}</code>
                <CopyButton text={secret.value} />
              </div>
              <button type="button" onClick={() => setSecret(null)} className="text-hue h-11 w-fit font-semibold underline underline-offset-2">
                {t('tokens.done')}
              </button>
            </div>
          )}

          <section aria-labelledby="ch-list" className="grid gap-3">
            <h2 id="ch-list" className="text-[17px] font-bold">
              {t('channels.list')}
            </h2>
            {!result && (
              <p role="status" className="text-ink2 flex items-center gap-2">
                <span className="spinner" aria-hidden />
                {t('channels.loading')}
              </p>
            )}
            {result?.status === 'failed' && (
              <div role="alert">
                <p className="text-ink2 mb-2">{result.forbidden ? t('channels.forbidden') : t('channels.loadFailed')}</p>
                {!result.forbidden && (
                  <button type="button" onClick={reload} className="text-hue min-h-11 font-semibold underline underline-offset-2">
                    {t('common.retry')}
                  </button>
                )}
              </div>
            )}
            {result?.status === 'ready' && channels.length === 0 && <p className="text-ink2">{t('channels.none')}</p>}
            {channels.length > 0 && (
              <ul className="border-line divide-line grid divide-y rounded-row border">
                {channels.map((ch) => (
                  <li key={ch.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 p-3">
                    <Webhook className="text-ink3 size-4 flex-none" aria-hidden />
                    <div className="min-w-0 flex-1">
                      <p className="flex flex-wrap items-center gap-2">
                        <span className="truncate font-semibold">{ch.name}</span>
                        <span className="bg-raised rounded px-1.5 py-0.5 text-[12px] font-semibold">{t(`channels.format.${ch.format}`)}</span>
                        {!ch.enabled && <span className="text-ink3 text-[12px]">{t('channels.disabled')}</span>}
                      </p>
                      <p className="text-ink2 truncate font-mono text-[13px]">{ch.url_hint}</p>
                      <p className={cn('text-[12px]', ch.last_status === 'error' ? 'text-err' : 'text-ink3')}>
                        {ch.last_sent_at
                          ? ch.last_status === 'error'
                            ? t('channels.lastFailed', { date: when(ch.last_sent_at), error: ch.last_error ?? '' })
                            : t('channels.lastOk', { date: when(ch.last_sent_at) })
                          : t('channels.neverSent')}
                      </p>
                    </div>
                    {canManage && (
                      <div className="flex flex-none flex-wrap items-center gap-1">
                        <button type="button" onClick={() => void test(ch)} disabled={busy === ch.id} className={ROW_BTN}>
                          {t('channels.test')}
                        </button>
                        {ch.format === 'ervisio' && (
                          <button type="button" onClick={() => rotate(ch)} disabled={busy === ch.id} className={ROW_BTN}>
                            {t('channels.rotate')}
                          </button>
                        )}
                        <button type="button" onClick={() => void toggle(ch)} disabled={busy === ch.id} className={ROW_BTN}>
                          {ch.enabled ? t('channels.disable') : t('channels.enable')}
                        </button>
                        <button type="button" onClick={() => remove(ch)} disabled={busy === ch.id} aria-label={`${t('channels.delete')} ${ch.name}`} className={cn(ROW_BTN, 'text-err')}>
                          {t('channels.delete')}
                        </button>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            )}
            {notice && (
              <p role={notice.ok ? 'status' : 'alert'} className={notice.ok ? 'text-ink' : 'text-err'}>
                {notice.text}
              </p>
            )}
          </section>

          {canManage && result?.status === 'ready' && (
            <CreateChannel
              workspace={workspace}
              onCreated={(ch) => {
                if (ch.secret) setSecret({ name: ch.name, value: ch.secret });
                reload();
              }}
            />
          )}
        </div>
      </div>
    </>
  );
}

function CreateChannel({ workspace, onCreated }: { workspace: Workspace; onCreated: (ch: NotificationChannel) => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [format, setFormat] = useState<ChannelFormat>('ervisio');
  const [url, setUrl] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const value = name.trim();
    const target = url.trim();
    if (!value) return setError(t('channels.nameRequired'));
    if (!target) return setError(t('channels.urlRequired'));
    setBusy(true);
    setError(null);
    try {
      const ch = await api.createChannel({ workspace_id: workspace.id, name: value, format, url: target });
      setName('');
      setUrl('');
      onCreated(ch);
    } catch (err) {
      setError(err instanceof ApiError && err.status < 500 ? err.message : t('channels.createFailed'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section aria-labelledby="ch-new" className="grid gap-3">
      <h2 id="ch-new" className="text-[17px] font-bold">
        {t('channels.new')}
      </h2>
      <form noValidate onSubmit={(e) => void submit(e)} className="grid gap-4">
        <div className="grid gap-1.5">
          <label htmlFor="ch-name" className="font-semibold">
            {t('channels.name')}
          </label>
          <input id="ch-name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} placeholder={t('channels.namePlaceholder')} className={INPUT} />
        </div>
        <fieldset className="grid gap-1">
          <legend className="mb-1.5 font-semibold">{t('channels.formatLabel')}</legend>
          {FORMATS.map((f) => (
            <label key={f} className="flex min-h-11 items-start gap-3 py-1 md:min-h-9">
              <input type="radio" name="ch-format" checked={format === f} onChange={() => setFormat(f)} className="accent-accent mt-1 size-4" />
              <span>
                <span className="font-semibold">{t(`channels.format.${f}`)}</span>
                <span className="text-ink2 block text-[13px]">{t(`channels.formatHint.${f}`)}</span>
              </span>
            </label>
          ))}
        </fieldset>
        <div className="grid gap-1.5">
          <label htmlFor="ch-url" className="font-semibold">
            {t('channels.url')}
          </label>
          <input id="ch-url" type="url" inputMode="url" autoComplete="off" spellCheck={false} value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://" aria-describedby="ch-url-hint" className={cn(INPUT, 'font-mono')} />
          <p id="ch-url-hint" className="text-ink3 text-[12px]">
            {t('channels.urlHint')}
          </p>
        </div>
        {error && (
          <p role="alert" className="text-err">
            {error}
          </p>
        )}
        <button type="submit" disabled={busy} className="bg-accent text-accent-ink flex h-12 w-fit items-center justify-center gap-2 rounded-row px-5 font-bold hover:brightness-110 disabled:opacity-60 md:h-11">
          {busy && <span className="spinner" aria-hidden />}
          {t('channels.create')}
        </button>
      </form>
    </section>
  );
}
