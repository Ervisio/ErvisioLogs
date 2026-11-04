/**
 * Settings: the values of the env file people change, as rows like plugin-firewall's, with a save bar that appears
 * when something changed. Secrets are never shown: an empty field keeps the current value. Production is refused
 * while the backend would refuse to start with these values.
 */
import { useMemo, useState } from 'react';
import { envGet, envSet, oneLine } from '../api/env';
import * as logs from '../api/logs';
import { Badge, Button, Icon, Input, Segmented, Skeleton, Switch } from '../kit';
import type { Ctx } from './LogsPage';
import { errText, t, useLoad } from './util';

interface FieldDef {
  key: string;
  label: string;
  help?: string;
  kind: 'text' | 'secret' | 'env' | 'bool';
  placeholder?: string;
}

const DOCKER: FieldDef[] = [
  { key: 'ERVISIO_LOGS_PUBLIC_URL', label: 'set.publicUrl', help: 'set.publicUrl.help', kind: 'text', placeholder: 'https://logs.example.com' },
  { key: 'ERVISIO_LOGS_ENV', label: 'set.envMode', help: 'set.envMode.help', kind: 'env' },
  { key: 'ERVISIO_LOGS_TRUST_PROXY', label: 'set.trustProxy', help: 'set.trustProxy.help', kind: 'bool' },
  { key: 'ERVISIO_LOGS_PORT', label: 'set.port', help: 'set.port.help', kind: 'text', placeholder: '8080' },
  { key: 'ERVISIO_LOGS_SETUP_CODE', label: 'set.setupCode', help: 'set.setupCode.help', kind: 'secret' },
  { key: 'ERVISIO_LOGS_SECRET_KEY', label: 'manage.serviceKey', kind: 'secret' },
  { key: 'ERVISIO_LOGS_POSTGRES_DSN', label: 'set.pgDsn', help: 'set.pgDsn.help', kind: 'secret' },
  { key: 'ERVISIO_LOGS_CLICKHOUSE_ADDR', label: 'set.chAddr', kind: 'text', placeholder: 'clickhouse:9000' },
  { key: 'ERVISIO_LOGS_ADMIN_SOCKET', label: 'manage.adminSocketPath', kind: 'text', placeholder: '/var/lib/ervisio-logs/socket/admin.sock' },
];

const STANDALONE: FieldDef[] = [
  { key: 'ERVISIO_LOGS_PUBLIC_URL', label: 'set.publicUrl', help: 'set.publicUrl.help', kind: 'text', placeholder: 'https://logs.example.com' },
  { key: 'ERVISIO_LOGS_ENV', label: 'set.envMode', help: 'set.envMode.help', kind: 'env' },
  { key: 'ERVISIO_LOGS_TRUST_PROXY', label: 'set.trustProxy', help: 'set.trustProxy.help', kind: 'bool' },
  { key: 'ERVISIO_LOGS_HTTP_ADDR', label: 'set.httpAddr', help: 'set.httpAddr.help', kind: 'text', placeholder: '127.0.0.1:8080' },
  { key: 'ERVISIO_LOGS_POSTGRES_DSN', label: 'set.pgDsn', help: 'set.pgDsn.help', kind: 'secret' },
  { key: 'ERVISIO_LOGS_CLICKHOUSE_ADDR', label: 'set.chAddr', kind: 'text', placeholder: '127.0.0.1:9000' },
  { key: 'ERVISIO_LOGS_CLICKHOUSE_USER', label: 'set.chUser', kind: 'text', placeholder: 'ervisio_logs' },
  { key: 'ERVISIO_LOGS_CLICKHOUSE_PASSWORD', label: 'set.chPassword', kind: 'secret' },
  { key: 'ERVISIO_LOGS_SETUP_CODE', label: 'set.setupCode', help: 'set.setupCode.help', kind: 'secret' },
  { key: 'ERVISIO_LOGS_SECRET_KEY', label: 'manage.serviceKey', kind: 'secret' },
  { key: 'ERVISIO_LOGS_CLICKHOUSE_DATABASE', label: 'manage.database', kind: 'text', placeholder: 'ervisio_logs' },
  { key: 'ERVISIO_LOGS_SPOOL_DIR', label: 'manage.spoolPath', kind: 'text' },
  { key: 'ERVISIO_LOGS_ARCHIVE_CACHE_DIR', label: 'manage.cachePath', kind: 'text' },
  { key: 'ERVISIO_LOGS_ARCHIVE_SECRETS_DIR', label: 'manage.secretsPath', kind: 'text' },
  { key: 'ERVISIO_LOGS_ADMIN_SOCKET', label: 'manage.adminSocketPath', kind: 'text', placeholder: '/run/ervisio-logs/admin.sock' },
];

type Draft = Record<string, string>;

/** The values in the file: secrets start empty (empty = keep), an unset key stays empty. */
function initial(fields: FieldDef[], env: string): Draft {
  const d: Draft = {};
  for (const f of fields) d[f.key] = f.kind === 'secret' ? '' : (envGet(env, f.key) ?? '');
  return d;
}

function apply(fields: FieldDef[], env: string, d: Draft): string {
  let out = env;
  for (const f of fields) {
    const v = oneLine(d[f.key]);
    if (f.kind === 'secret' && !v) continue;
    if (f.kind !== 'secret' && v === (envGet(env, f.key) ?? '')) continue;
    out = envSet(out, f.key, v);
  }
  return out;
}

export function Settings({ ctx }: { ctx: Ctx }) {
  const { mode, runner, settingsEdits: edits, setSettingsEdits: setEdits } = ctx;
  const file = useLoad(() => logs.readEnv(mode), [mode]);
  const fields = mode === 'docker' ? DOCKER : STANDALONE;
  const env = file.data ?? null;
  const base = useMemo(() => (env == null ? null : initial(fields, env)), [env, fields]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const ignored = useLoad(() => logs.ignoredEnvKeys(mode, env), [mode, env]);

  if (file.loading && !file.data) {
    return (
      <section className="el-card">
        <h3><Icon name="cog" />{t('set.title')}</h3>
        <Skeleton height={260} style={{ borderRadius: 12 }} />
      </section>
    );
  }
  if (file.error) {
    return (
      <section className="el-card">
        <h3><Icon name="cog" />{t('set.title')}</h3>
        <div className="el-note is-err"><Icon name="alert" /><span>{errText(file.error)}</span></div>
      </section>
    );
  }
  if (env == null || !base) {
    return (
      <section className="el-card">
        <h3><Icon name="cog" />{t('set.title')}</h3>
        <div className="el-note is-info"><Icon name="info" /><span>{t('set.noFile')}</span></div>
      </section>
    );
  }

  // The edits sit on top of the file, so a reload of the file keeps what the user has typed.
  const draft: Draft = { ...base, ...edits };
  const dirty = fields.some((f) => draft[f.key] !== base[f.key]);
  const next = apply(fields, env, draft);
  const problems = logs.productionProblems(mode, envSet(next, 'ERVISIO_LOGS_ENV', 'production'));
  const wantsProd = draft.ERVISIO_LOGS_ENV === 'production';
  const blocked = wantsProd && problems.length > 0;
  const set = (k: string, v: string) => { setSaved(false); setEdits({ ...edits, [k]: v }); };

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await logs.writeEnv(mode, next);
      // Typed secrets are written and forgotten; the other fields match the file once it is read again.
      setEdits(Object.fromEntries(fields.filter((f) => f.kind !== 'secret' && f.key in edits).map((f) => [f.key, edits[f.key]])));
      setSaved(true);
      file.reload();
    } catch (e) {
      setError(errText(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <section className="el-card">
        <h3><Icon name="cog" />{t('set.title')}<span className="el-count el-mono">{logs.envPathFor(mode)}</span></h3>
        <div className="el-sets">
          {fields.map((f) => {
            const cur = envGet(env, f.key);
            return (
              <div key={f.key} className="el-set">
                <span className="el-set-t">
                  <b>{t(f.label)}</b>
                  {f.help && <span>{t(f.help)}</span>}
                  {f.kind === 'secret' && <span>{t(cur ? 'set.secretSet' : 'set.secretEmpty')}</span>}
                </span>
                <span className="el-set-c">
                  {f.kind === 'env' ? (
                    <Segmented
                      aria-label={t(f.label)}
                      value={draft[f.key] || 'development'}
                      onChange={(v) => set(f.key, v)}
                      options={[{ value: 'development', label: t('set.dev') }, { value: 'production', label: t('set.prod') }]}
                    />
                  ) : f.kind === 'bool' ? (
                    <Switch aria-label={t(f.label)} checked={draft[f.key] === 'true'} onChange={(v) => set(f.key, v ? 'true' : 'false')} />
                  ) : (
                    <Input
                      aria-label={t(f.label)}
                      type={f.kind === 'secret' ? 'password' : 'text'}
                      autoComplete="off"
                      mono
                      compact
                      value={draft[f.key]}
                      placeholder={f.kind === 'secret' ? t('set.secretKeep') : f.placeholder}
                      onChange={(e) => set(f.key, e.target.value)}
                    />
                  )}
                </span>
              </div>
            );
          })}
        </div>
        {problems.length > 0 && (
          <div className={`el-note ${wantsProd ? 'is-err' : 'is-warn'}`}>
            <Icon name="alert" />
            <span>
              <b>{t('prod.blocked')}</b>
              <ul className="el-probs">{problems.map((p) => <li key={p}>{t(`prob.${p}`)}</li>)}</ul>
            </span>
          </div>
        )}
        {!problems.length && !wantsProd && (
          <div className="el-note is-info"><Icon name="shield" /><span>{t('prod.ready')}</span></div>
        )}
        {ignored.data && ignored.data.length > 0 && (
          <div className="el-note is-warn">
            <Icon name="alert" />
            <span>{t('set.ignored', { keys: ignored.data.join(', ') })}</span>
          </div>
        )}
        <p className="el-muted">{t('set.otherKeys')}</p>
      </section>

      {saved && !dirty && (
        <div className="el-note is-ok">
          <Icon name="check" />
          <span>{t('set.saved')}</span>
          <span className="el-sp" />
          <Button size="sm" variant="primary" icon="refresh" disabled={runner.busy} onClick={() => runner.run(t('act.restart'), (log) => logs.restart(mode, log)).then(() => setSaved(false))}>
            {t('set.applyNow')}
          </Button>
        </div>
      )}
      {error && <div className="el-note is-err"><Icon name="alert" /><span>{error}</span></div>}

      {dirty && (
        <div className="el-savebar" role="region" aria-label={t('set.unsaved')}>
          <Badge tone="warn" dot>{t('set.unsaved')}</Badge>
          {blocked && <span className="el-muted">{t('prod.fixFirst')}</span>}
          <span className="el-sp" />
          <Button variant="ghost" disabled={saving} onClick={() => setEdits({})}>{t('set.restore')}</Button>
          <Button variant="primary" icon="check" loading={saving} disabled={saving || blocked} onClick={save}>{t('set.save')}</Button>
        </div>
      )}
    </>
  );
}
