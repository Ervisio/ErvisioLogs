/**
 * The Ervisio Logs page: a header with the state of the app, an inner menu like plugin-docker's, and one section at a
 * time (overview, address and Caddy, settings, console). With no mode chosen yet, the setup steps (Setup.tsx).
 */
import { useState } from 'react';
import { getSdk } from '../sdk';
import * as logs from '../api/logs';
import { Badge, Button, EmptyState, Icon, Skeleton, type HueId } from '../kit';
import { Address } from './Address';
import { Console } from './Console';
import { Overview } from './Overview';
import { Settings } from './Settings';
import { Setup } from './Setup';
import { errText, hostOf, t, useLoad, useRunner, type Runner } from './util';

export type SectionId = 'overview' | 'address' | 'settings' | 'console';

const SECTIONS: { id: SectionId; hue: HueId; icon: string }[] = [
  { id: 'overview', hue: 'ov', icon: 'overview' },
  { id: 'address', hue: 'svc', icon: 'globe' },
  { id: 'settings', hue: 'sw', icon: 'cog' },
  { id: 'console', hue: 'term', icon: 'terminal' },
];

/** What every section gets: the mode, the last state read, a reload, and the shared action runner. */
export interface Ctx {
  mode: logs.Mode;
  status: logs.Status;
  reload(): void;
  runner: Runner;
  go(s: SectionId): void;
  changeMode(): void;
}

export function LogsPage() {
  const prefs = useLoad(logs.loadPrefs);
  const mode = prefs.data?.mode ?? null;
  if (!prefs.data) {
    if (prefs.loading) return <Loading />;
    return (
      <div className="el-root hue-log">
        <EmptyState icon="alert" hue="log" title={t('error.title')} text={errText(prefs.error)}
          action={<Button icon="refresh" onClick={prefs.reload}>{t('retry')}</Button>} />
      </div>
    );
  }
  if (!mode) {
    return (
      <div className="el-root hue-log">
        <Setup onChosen={prefs.reload} />
      </div>
    );
  }
  return <Installed mode={mode} onModeReset={async () => { await logs.savePrefs({ mode: null }); prefs.reload(); }} />;
}

function Loading() {
  return (
    <div className="el-root hue-log">
      <Skeleton height={56} style={{ borderRadius: 18 }} />
      <div className="el-inner"><Skeleton height={200} style={{ borderRadius: 18 }} /><Skeleton height={320} style={{ borderRadius: 18 }} /></div>
    </div>
  );
}

function Installed({ mode, onModeReset }: { mode: logs.Mode; onModeReset(): void }) {
  const st = useLoad(() => logs.status(mode), [mode]);
  const runner = useRunner(st.reload);
  const [section, setSection] = useState<SectionId>('overview');

  if (!st.data) {
    if (st.loading) return <Loading />;
    return (
      <div className="el-root hue-log">
        <EmptyState icon="alert" hue="log" title={t('error.title')} text={errText(st.error)}
          action={<Button icon="refresh" onClick={st.reload}>{t('retry')}</Button>} />
      </div>
    );
  }

  const s = st.data;
  const ctx: Ctx = { mode, status: s, reload: st.reload, runner, go: setSection, changeMode: onModeReset };
  const host = hostOf(s.publicUrl);
  const tone = !s.installed ? 'neutral' : !s.running ? 'warn' : s.healthy === false ? 'err' : 'ok';
  const label = !s.installed ? t('status.notInstalled') : !s.running ? t('status.stopped') : s.healthy === false ? t('status.unhealthy') : t('status.running');
  const canOpen = s.running && !!s.publicUrl;

  return (
    <div className="el-root hue-log">
      <header className="el-ph">
        <span className="el-tile"><Icon name="logs" /></span>
        <div className="el-ph-t">
          <h1>Ervisio Logs</h1>
          <p>
            <Badge tone={tone} dot>{label}</Badge>
            <span>{t(mode === 'docker' ? 'mode.docker' : 'mode.standalone')}</span>
            {s.version && <span>{t('status.version', { v: s.version })}</span>}
            {host && canOpen && (
              <button type="button" className="el-linkbtn" onClick={() => getSdk().openExternal(s.publicUrl!)}>{host}</button>
            )}
          </p>
        </div>
        {runner.busy && <Badge tone="info" dot>{runner.job?.title}</Badge>}
        {canOpen && (
          <Button variant="primary" icon="externallink" onClick={() => getSdk().openExternal(s.publicUrl!)}>{t('act.open')}</Button>
        )}
      </header>

      <div className="el-inner">
        <nav className="el-nav" aria-label={t('nav.label')}>
          <div className="el-nav-lab">{t('nav.label')}</div>
          {SECTIONS.map((it) => (
            <button key={it.id} type="button" className="el-nav-it" aria-current={section === it.id ? 'page' : undefined} onClick={() => setSection(it.id)}>
              <span className={`el-ki hue-${it.hue}`}><Icon name={it.icon} /></span>
              {t(`nav.${it.id}`)}
              {it.id === 'console' && runner.job && <i className={`el-nav-dot ${runner.job.running ? 'is-run' : runner.job.failed ? 'is-err' : 'is-ok'}`} />}
            </button>
          ))}
        </nav>
        <div className="el-main">
          {section === 'overview' && <Overview ctx={ctx} />}
          {section === 'address' && <Address ctx={ctx} />}
          {section === 'settings' && <Settings ctx={ctx} />}
          {section === 'console' && <Console ctx={ctx} />}
        </div>
      </div>
    </div>
  );
}
