/**
 * Overview: the route a request takes (internet → Caddy → app → databases) with the state of each hop, the services,
 * and the actions (install or update, start, restart, stop, change mode, remove the standalone service).
 */
import { useState } from 'react';
import { envGet } from '../api/env';
import * as logs from '../api/logs';
import { Badge, Button, ConfirmDialog, Icon, type Tone } from '../kit';
import type { Ctx } from './LogsPage';
import { hostOf, t } from './util';

type HopState = 'ok' | 'warn' | 'err' | 'off';

function Hop({ icon, hue, title, sub, state, onClick }: { icon: string; hue: string; title: string; sub: string; state: HopState; onClick?(): void }) {
  const body = (
    <>
      <span className={`el-ki hue-${hue}`}><Icon name={icon} /></span>
      <span className="el-hop-t">
        <b>{title}</b>
        <span>{sub}</span>
      </span>
      <i className={`el-hop-dot is-${state}`} />
    </>
  );
  return onClick ? (
    <button type="button" className="el-hop is-link" onClick={onClick}>{body}</button>
  ) : (
    <div className="el-hop">{body}</div>
  );
}

function Wire({ down }: { down: boolean }) {
  return (
    <span className={`el-wire${down ? ' is-down' : ''}`} aria-hidden="true">
      <svg viewBox="0 0 100 10" preserveAspectRatio="none"><path d="M0 5 H100" /></svg>
    </span>
  );
}

const serviceTone = (s: logs.Service): Tone =>
  s.state !== 'running' ? (s.state === 'exited' && /\(0\)/.test(s.status) ? 'neutral' : 'err') : s.health === 'unhealthy' ? 'err' : s.health === 'starting' ? 'warn' : 'ok';

export function Overview({ ctx }: { ctx: Ctx }) {
  const { mode, status: s, runner } = ctx;
  const [confirm, setConfirm] = useState<'stop' | 'uninstall' | 'mode' | null>(null);
  const env = s.env ?? '';
  const envMode = envGet(s.env, 'ERVISIO_LOGS_ENV') || 'development';
  const problems = s.env ? logs.productionProblems(mode, env) : [];
  const https = !!s.publicUrl && s.publicUrl.startsWith('https://');
  const host = hostOf(s.publicUrl);
  const busy = runner.busy;

  const act = (title: string, fn: (log: logs.Log) => Promise<unknown>) => runner.run(title, fn);
  const install = () => act(t(s.installed ? 'act.update' : 'act.install'), (log) => logs.install(mode, log));

  const appState: HopState = !s.installed ? 'off' : !s.running ? 'warn' : s.healthy === false ? 'err' : 'ok';
  const dbs = s.services.filter((x) => x.name === 'postgres' || x.name === 'clickhouse');
  const dbState: HopState =
    mode === 'standalone' ? (s.healthy ? 'ok' : s.installed ? 'warn' : 'off') : !dbs.length ? 'off' : dbs.every((d) => serviceTone(d) === 'ok') ? 'ok' : 'err';

  return (
    <>
      {logs.updateAvailable(s) && (
        <section className={`el-up${busy ? ' is-run' : ''}`}>
          <div className="el-up-row">
            <span className="el-up-ic"><Icon name="download" /></span>
            <span className="el-up-t">
              <b>{t('update.title', { v: logs.BUNDLE.version })}</b>
              <span>{t('update.text', { from: s.version })}</span>
            </span>
            <Button variant="primary" icon="download" loading={busy} disabled={busy} onClick={install}>{t('act.update')}</Button>
          </div>
          {busy && <div className="el-up-bar"><span /></div>}
        </section>
      )}

      {!s.installed && (
        <section className="el-card el-hero">
          <h2>{t('install.title')}</h2>
          <p>{t(mode === 'docker' ? 'install.docker' : 'install.standalone')}</p>
          <ul className="el-steps">
            {(mode === 'docker' ? ['install.d1', 'install.d2', 'install.d3'] : ['install.s1', 'install.s2', 'install.s3']).map((k) => (
              <li key={k}><Icon name="check" />{t(k)}</li>
            ))}
          </ul>
          {!logs.isRelease && <div className="el-note is-warn"><Icon name="alert" /><span>{t('devBuild')}</span></div>}
          <div className="el-acts">
            <Button variant="primary" icon="download" loading={busy} disabled={busy || !logs.isRelease} onClick={install}>{t('act.install')}</Button>
            <Button variant="ghost" icon="refresh" disabled={busy} onClick={() => setConfirm('mode')}>{t('mode.change')}</Button>
          </div>
        </section>
      )}

      {s.error && <div className="el-note is-err"><Icon name="alert" /><span>{s.error}</span></div>}

      {s.installed && (
        <section className="el-card">
          <h3><Icon name="net" />{t('route.title')}</h3>
          <div className="el-route">
            <Hop icon="globe" hue="usr" title={t('route.internet')} sub={host ?? t('route.noUrl')} state={https ? 'ok' : 'warn'} onClick={() => ctx.go('address')} />
            <Wire down={!https} />
            <Hop icon="shield" hue="svc" title="Caddy" sub={https ? t('route.https') : t('route.notPublished')} state={https ? 'ok' : 'off'} onClick={() => ctx.go('address')} />
            <Wire down={!s.running} />
            <Hop icon="logs" hue="log" title="Ervisio Logs" sub={s.version ? `v${s.version}` : t(`status.${s.running ? 'running' : 'stopped'}`)} state={appState} />
            <Wire down={dbState !== 'ok'} />
            <Hop icon="database" hue="file" title={t('route.db')} sub="PostgreSQL, ClickHouse" state={dbState} />
          </div>
          {!https && (
            <div className="el-note is-info">
              <Icon name="info" />
              <span>{t('route.publishHint')}</span>
              <span className="el-sp" />
              <Button size="sm" icon="globe" onClick={() => ctx.go('address')}>{t('act.publish')}</Button>
            </div>
          )}
          {envMode !== 'production' && (
            <div className="el-note is-warn">
              <Icon name="alert" />
              <span>
                {t('prod.dev')}
                {problems.length > 0 && <> {problems.map((p) => t(`prob.${p}`)).join(' ')}</>}
              </span>
              <span className="el-sp" />
              <Button size="sm" icon="cog" onClick={() => ctx.go('settings')}>{t('nav.settings')}</Button>
            </div>
          )}
        </section>
      )}

      {s.installed && mode === 'docker' && s.services.length > 0 && (
        <section className="el-card">
          <h3><Icon name="layers" />{t('svc.title')}<span className="el-count">{t('svc.count', { n: s.services.length })}</span></h3>
          <div className="el-list">
            {s.services.map((x) => (
              <div key={x.name} className="el-li">
                <b className="el-mono">{x.name}</b>
                <span className="el-muted">{x.status}</span>
                <Badge tone={serviceTone(x)} dot>{x.health || x.state}</Badge>
              </div>
            ))}
          </div>
        </section>
      )}

      {s.installed && (
        <section className="el-card">
          <h3><Icon name="power" />{t('act.title')}</h3>
          <div className="el-acts">
            {s.running ? (
              <Button icon="refresh" disabled={busy} onClick={() => act(t('act.restart'), (log) => logs.restart(mode, log))}>{t('act.restart')}</Button>
            ) : (
              <Button variant="primary" icon="play" disabled={busy} onClick={() => act(t('act.start'), (log) => logs.start(mode, log))}>{t('act.start')}</Button>
            )}
            {s.running && <Button icon="stop" disabled={busy} onClick={() => setConfirm('stop')}>{t('act.stop')}</Button>}
            <Button icon="logs" disabled={busy} onClick={() => { ctx.go('console'); void act(t('act.logs'), (log) => logs.logs(mode, log)); }}>{t('act.logs')}</Button>
            {!logs.updateAvailable(s) && logs.isRelease && (
              <Button variant="ghost" icon="download" disabled={busy} onClick={install}>{t('act.reinstall')}</Button>
            )}
            <span className="el-sp" />
            <Button variant="ghost" icon="refresh" disabled={busy} onClick={() => setConfirm('mode')}>{t('mode.change')}</Button>
            {mode === 'standalone' && (
              <Button variant="danger" icon="trash" disabled={busy} onClick={() => setConfirm('uninstall')}>{t('act.uninstall')}</Button>
            )}
          </div>
        </section>
      )}

      <section className="el-card">
        <h3><Icon name="info" />{t('notes.title')}</h3>
        <div className="el-notes">
          {mode === 'docker' && <div className="el-note"><Icon name="layers" /><span>{t('docker.inDockerPlugin')}</span></div>}
          {mode === 'docker' && <div className="el-note"><Icon name="file" /><span>{t('docker.note')}</span></div>}
          {mode === 'docker' && <div className="el-note"><Icon name="key" /><span>{t('docker.secrets')}</span></div>}
          {mode === 'standalone' && <div className="el-note"><Icon name="file" /><span>{t('standalone.placeholders')}</span></div>}
          <div className="el-note"><Icon name="user" /><span>{t('setupCode')}</span></div>
          <div className="el-note"><Icon name="shield" /><span>{t('removeNote')}</span></div>
        </div>
      </section>

      <ConfirmDialog
        open={confirm === 'stop'}
        onClose={() => setConfirm(null)}
        onConfirm={() => { setConfirm(null); void act(t('act.stop'), (log) => logs.stop(mode, log)); }}
        title={t('act.stop')}
        description={t(mode === 'docker' ? 'stop.docker' : 'stop.standalone')}
        confirmLabel={t('act.stop')}
        icon="stop"
      />
      <ConfirmDialog
        open={confirm === 'uninstall'}
        onClose={() => setConfirm(null)}
        onConfirm={() => { setConfirm(null); ctx.go('console'); void act(t('act.uninstall'), (log) => logs.uninstallStandalone(log)); }}
        title={t('act.uninstall')}
        description={t('uninstall.confirm')}
        confirmLabel={t('act.uninstall')}
        danger
        icon="trash"
      />
      <ConfirmDialog
        open={confirm === 'mode'}
        onClose={() => setConfirm(null)}
        onConfirm={() => { setConfirm(null); ctx.changeMode(); }}
        title={t('mode.change')}
        description={t('mode.changeConfirm')}
        confirmLabel={t('mode.change')}
        icon="refresh"
      />
    </>
  );
}
