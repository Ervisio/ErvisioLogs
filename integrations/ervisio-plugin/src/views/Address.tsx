/**
 * Address: publish Ervisio Logs through the Caddy already on this server (the system service, or a container), the
 * same way plugin-caddy writes sites. Shows what is there (our block, or a site written by hand), proposes
 * logs.<your domain>, writes, validates and reloads with rollback, then sets the public URL of the app.
 */
import { useEffect, useMemo, useState } from 'react';
import * as caddy from '../api/caddy';
import { dockerProxyLabels, generateBlock, proxyTarget } from '../api/caddy-core';
import * as logs from '../api/logs';
import { getSdk } from '../sdk';
import { Badge, Button, ConfirmDialog, Icon, Input, Skeleton, Switch } from '../kit';
import type { Ctx } from './LogsPage';
import { copy, errText, t, useLoad } from './util';

interface Outcome {
  res: caddy.CaddyResult;
  production?: boolean;
  problems?: logs.Problem[];
}

function Snippet({ text }: { text: string }) {
  return (
    <div className="el-snipw">
      <pre className="el-snip">{text}</pre>
      <Button size="sm" variant="ghost" icon="copy" onClick={async () => { const ok = await copy(text); getSdk().ui.toast[ok ? 'ok' : 'err'](t(ok ? 'copied' : 'copyFail')); }}>{t('act.copy')}</Button>
    </div>
  );
}

const instanceLabel = (i: caddy.CaddyInstance) => (i.kind === 'system' ? t('caddy.system') : t('caddy.container', { n: i.name }));

export function Address({ ctx }: { ctx: Ctx }) {
  const { mode, status: s, runner } = ctx;
  const prefs = useLoad(logs.loadPrefs);
  const askDocker = !!prefs.data?.askDocker;
  const prop = useLoad(() => (prefs.data ? caddy.propose(s.publicUrl, { askDocker }) : new Promise<never>(() => {})), [askDocker, s.publicUrl, !!prefs.data]);
  const up = useMemo(() => logs.upstream(mode, s.env), [mode, s.env]);

  const p = prop.data;
  const inst = p?.instance ?? null;
  const [host, setHost] = useState('');
  const [setUrl, setSetUrl] = useState(true);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [confirmRemove, setConfirmRemove] = useState(false);

  useEffect(() => {
    if (p) setHost(p.current?.host ?? p.manualHost ?? p.suggestion?.host ?? '');
  }, [p]);

  const busy = runner.busy;
  const canEdit = !!inst && !inst.unsupported && caddy.reachable(inst, up);

  const publish = () =>
    runner.run(t('act.publish'), async (log) => {
      if (!inst) return;
      const extra: Omit<Outcome, 'res'> = {};
      log(`caddy: ${instanceLabel(inst)} (${inst.configPath})`);
      const res = await caddy.apply(inst, host, up, {
        setPublicUrl: setUrl
          ? async (url) => {
              const r = await logs.setPublicUrl(mode, url, log);
              extra.production = r.production;
              extra.problems = r.problems;
            }
          : undefined,
      });
      for (const w of res.warnings) log(`warning: ${w}`);
      log(res.ok ? `caddy: ${res.host} -> ${res.target}` : `caddy: ${res.step}: ${res.message}`);
      setOutcome({ res, ...extra });
      prop.reload();
      if (!res.ok) throw new Error(res.message);
      getSdk().ui.toast.ok(t('addr.published', { h: res.host! }));
    });

  const remove = () =>
    runner.run(t('act.unpublish'), async (log) => {
      if (!inst) return;
      const res = await caddy.remove(inst);
      log(res.ok ? `caddy: removed ${res.host ?? ''}` : `caddy: ${res.step}: ${res.message}`);
      setOutcome({ res });
      prop.reload();
      if (!res.ok) throw new Error(res.message);
    });

  // What to paste by hand when the plugin cannot write (no Caddy, unsupported, bridge).
  const handSnippet = (h: string) => {
    if (inst?.unsupported === 'labels') return null;
    const kind = inst && inst.kind === 'docker' ? (inst.hostNetwork ? 'docker-host' : up.container ? 'docker' : 'system') : 'system';
    return generateBlock(h || 'logs.example.com', proxyTarget(kind, up));
  };

  if (!prefs.data || (!p && prop.loading)) {
    return (
      <section className="el-card">
        <h3><Icon name="globe" />{t('addr.title')}</h3>
        <p className="el-muted">{t('addr.detecting')}</p>
        <Skeleton height={44} style={{ borderRadius: 12 }} />
        <Skeleton height={120} style={{ borderRadius: 12 }} />
      </section>
    );
  }
  if (!p) {
    return (
      <section className="el-card">
        <h3><Icon name="globe" />{t('addr.title')}</h3>
        <div className="el-note is-err"><Icon name="alert" /><span>{errText(prop.error)}</span></div>
        <div className="el-acts"><Button icon="refresh" onClick={prop.reload}>{t('retry')}</Button></div>
      </section>
    );
  }

  const others = p.detection.instances.filter((i) => i !== inst);
  return (
    <>
      <section className="el-card">
        <h3>
          <Icon name="shield" />{t('addr.caddy')}
          <span className="el-count">
            <button type="button" className="el-linkbtn" disabled={prop.loading} onClick={prop.reload}>{t('addr.rescan')}</button>
          </span>
        </h3>
        {inst ? (
          <div className="el-li el-li--big">
            <span className={`el-ki hue-${inst.kind === 'system' ? 'svc' : 'file'}`}><Icon name={inst.kind === 'system' ? 'shield' : 'box'} /></span>
            <span className="el-li-t">
              <b>{instanceLabel(inst)}</b>
              <span className="el-mono">{inst.configPath}</span>
            </span>
            <Badge tone={inst.running ? 'ok' : 'warn'} dot>{t(inst.running ? 'status.running' : 'status.stopped')}</Badge>
          </div>
        ) : (
          <div className="el-note is-warn"><Icon name="alert" /><span>{t('addr.noCaddy')}</span></div>
        )}
        {others.length > 0 && <p className="el-muted">{t('addr.others', { list: others.map(instanceLabel).join(', ') })}</p>}
        {p.detection.dockerSkipped && (
          <div className="el-note is-info">
            <Icon name="box" />
            <span>{t('addr.askDocker')}</span>
            <span className="el-sp" />
            <Button size="sm" icon="search" onClick={async () => { await logs.savePrefs({ askDocker: true }); prefs.reload(); }}>{t('addr.lookDocker')}</Button>
          </div>
        )}
        {p.detection.dockerError && <p className="el-muted">{t('addr.dockerError', { e: p.detection.dockerError })}</p>}
        {inst?.unsupported && (
          <div className="el-note is-warn"><Icon name="alert" /><span>{t(`unsup.${inst.unsupported}`)}</span></div>
        )}
        {inst && !inst.unsupported && !caddy.reachable(inst, up) && (
          <div className="el-note is-warn"><Icon name="alert" /><span>{t('unsup.bridge')}</span></div>
        )}
        {inst?.ephemeral && <div className="el-note is-warn"><Icon name="alert" /><span>{t('addr.ephemeral')}</span></div>}
      </section>

      <section className="el-card">
        <h3><Icon name="globe" />{t('addr.title')}</h3>
        {p.current && (
          <div className="el-pub">
            <span className="el-lk"><Icon name="lock" /></span>
            <span className="el-li-t">
              <b>{p.current.host}</b>
              <span>{t('addr.byPlugin', { target: p.current.target })}</span>
            </span>
            <Button size="sm" icon="externallink" onClick={() => getSdk().openExternal(`https://${p.current!.host}`)}>{t('act.open')}</Button>
            {canEdit && <Button size="sm" variant="ghost" icon="trash" disabled={busy} onClick={() => setConfirmRemove(true)}>{t('act.unpublish')}</Button>}
          </div>
        )}
        {p.current && p.current.layout === 'file' && !p.current.imported && (
          <div className="el-note is-warn"><Icon name="alert" /><span>{t('addr.notImported', { path: p.current.path })}</span></div>
        )}
        {!p.current && p.manualHost && (
          <div className="el-pub is-manual">
            <span className="el-lk"><Icon name="lock" /></span>
            <span className="el-li-t">
              <b>{p.manualHost}</b>
              <span>{t('addr.manual')}</span>
            </span>
            <Button size="sm" icon="externallink" onClick={() => getSdk().openExternal(`https://${p.manualHost}`)}>{t('act.open')}</Button>
          </div>
        )}
        {!p.current && p.manualHost && <p className="el-muted">{t('addr.manualHint')}</p>}

        {canEdit && !p.manualHost && (
          <>
            <div className="el-form">
              <Input
                label={t('addr.host')}
                value={host}
                mono
                placeholder="logs.example.com"
                onChange={(e) => setHost(e.target.value)}
                hint={
                  p.current
                    ? t('addr.hintCurrent')
                    : p.suggestion
                      ? t(p.suggestion.renamed ? 'addr.hintRenamed' : 'addr.hintSuggested', { base: p.suggestion.base })
                      : t('addr.hintNone')
                }
              />
              <Button variant="primary" icon="globe" loading={busy} disabled={busy || !host.trim() || !s.installed} onClick={publish}>
                {t(p.current ? 'act.republish' : 'act.publish')}
              </Button>
            </div>
            <Switch checked={setUrl} onChange={setSetUrl} label={t('addr.setUrl')} />
            <ul className="el-steps">
              <li><Icon name="check" />{t('addr.s1', { target: caddy.targetFor(inst!, up) })}</li>
              <li><Icon name="check" />{t('addr.s2')}</li>
              {setUrl && <li><Icon name="check" />{t('addr.s3')}</li>}
            </ul>
            {!s.installed && <p className="el-muted">{t('addr.installFirst')}</p>}
          </>
        )}

        {(!inst || inst.unsupported || !caddy.reachable(inst, up)) && (
          <>
            <p className="el-muted">{t(inst?.unsupported === 'labels' ? 'addr.pasteLabels' : 'addr.paste')}</p>
            {inst?.unsupported === 'labels' ? (
              <Snippet text={dockerProxyLabels(host || p.suggestion?.host || 'logs.example.com', up)} />
            ) : (
              <Snippet text={handSnippet(host || p.suggestion?.host || '')!} />
            )}
            <p className="el-muted">{t('addr.thenUrl')}</p>
          </>
        )}

      </section>

      {outcome && (
        <Result
          o={outcome}
          onSettings={() => ctx.go('settings')}
          onUseExisting={(h) =>
            runner.run(t('addr.useExisting'), async (log) => {
              const r = await logs.setPublicUrl(mode, `https://${h}`, log);
              setOutcome({ res: { ok: true, step: 'done', message: t('res.urlOnly', { h }), snippet: null, warnings: [] }, ...r });
            })
          }
        />
      )}

      <ConfirmDialog
        open={confirmRemove}
        onClose={() => setConfirmRemove(false)}
        onConfirm={() => { setConfirmRemove(false); void remove(); }}
        title={t('act.unpublish')}
        description={t('addr.removeConfirm', { h: p.current?.host ?? '' })}
        confirmLabel={t('act.unpublish')}
        danger
        icon="trash"
      />
    </>
  );
}

function Result({ o, onSettings, onUseExisting }: { o: Outcome; onSettings(): void; onUseExisting(host: string): void }) {
  const r = o.res;
  // Caddy already serves this host in a site written by hand: it may already point at Ervisio Logs.
  const handWritten = !r.ok && r.step === 'conflict' && !r.reason && !!r.host;
  return (
    <section className="el-card">
      <h3><Icon name={r.ok ? 'check' : 'alert'} />{t(r.ok ? 'res.ok' : 'res.fail')}</h3>
      {r.ok ? (
        <div className="el-note is-ok">
          <Icon name="check" />
          <span>
            {r.notRunning ? t('res.notRunning') : r.host ? t('res.serving', { h: r.host, target: r.target ?? '' }) : r.message || t('res.removed')}
          </span>
        </div>
      ) : (
        <div className="el-note is-err">
          <Icon name="alert" />
          <span>
            <b>{t(`step.${r.step}`)}</b> {r.message}
            {r.rolledBack && <> {t('res.rolledBack')}</>}
          </span>
        </div>
      )}
      {handWritten && (
        <div className="el-note is-info">
          <Icon name="info" />
          <span>{t('res.handWritten', { h: r.host! })}</span>
          <span className="el-sp" />
          <Button size="sm" icon="check" onClick={() => onUseExisting(r.host!)}>{t('addr.useExisting')}</Button>
        </div>
      )}
      {o.production === true &&<div className="el-note is-ok"><Icon name="shield" /><span>{t('res.production')}</span></div>}
      {o.production === false && (
        <div className="el-note is-warn">
          <Icon name="alert" />
          <span>{t('res.stillDev')} {(o.problems ?? []).map((p) => t(`prob.${p}`)).join(' ')}</span>
          <span className="el-sp" />
          <Button size="sm" icon="cog" onClick={onSettings}>{t('nav.settings')}</Button>
        </div>
      )}
      {r.warnings.map((w) => (
        <div key={w} className="el-note is-warn"><Icon name="alert" /><span>{w}</span></div>
      ))}
      {!r.ok && r.snippet && (
        <>
          <p className="el-muted">{t('addr.paste')}</p>
          <Snippet text={r.snippet} />
        </>
      )}
    </section>
  );
}
