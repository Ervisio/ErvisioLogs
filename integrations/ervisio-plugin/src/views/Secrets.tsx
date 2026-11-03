/**
 * Archive credentials: writes one file per archive destination into the secrets folder the app reads
 * (ERVISIO_LOGS_ARCHIVE_SECRETS_DIR). Values go in and never come back out: the card lists names only.
 */
import { useState } from 'react';
import * as logs from '../api/logs';
import { Button, Icon, Input, Segmented, Textarea } from '../kit';
import type { Ctx } from './LogsPage';
import { t, useLoad } from './util';

type Kind = 'password' | 'key';

export function Secrets({ ctx }: { ctx: Ctx }) {
  const { mode, runner } = ctx;
  const names = useLoad(() => logs.listSecrets(mode), [mode]);
  const [name, setName] = useState('storagebox');
  const [kind, setKind] = useState<Kind>('password');
  const [value, setValue] = useState('');
  const [done, setDone] = useState<string | null>(null);

  const nameOk = logs.validSecretName(name.trim());
  const exists = (names.data ?? []).includes(name.trim());

  const save = async () => {
    const n = name.trim();
    setDone(null);
    // The runner shows a failure in the console and as a toast.
    const ok = await runner.run(t('sec.save'), (log) => logs.writeSecret(mode, n, kind === 'password' ? value.replace(/\r?\n$/, '') : value, log));
    if (!ok) return;
    setValue('');
    setDone(n);
    names.reload();
  };

  return (
    <section className="el-card">
      <h3><Icon name="lock" />{t('sec.title')}<span className="el-count el-mono">{logs.SECRETS_DIR[mode]}</span></h3>
      <p className="el-muted">{t('sec.intro')}</p>
      {!!names.data?.length && (
        <p className="el-muted">{t('sec.existing')} <span className="el-mono">{names.data.join(', ')}</span></p>
      )}
      <div className="el-sets">
        <div className="el-set">
          <span className="el-set-t"><b>{t('sec.name')}</b><span>{t('sec.name.help')}</span></span>
          <span className="el-set-c">
            <Input aria-label={t('sec.name')} autoComplete="off" mono compact value={name} onChange={(e) => setName(e.target.value)} />
          </span>
        </div>
        <div className="el-set">
          <span className="el-set-t"><b>{t('sec.kind')}</b></span>
          <span className="el-set-c">
            <Segmented
              aria-label={t('sec.kind')}
              value={kind}
              onChange={(v) => { setKind(v as Kind); setValue(''); }}
              options={[{ value: 'password', label: t('sec.kind.password') }, { value: 'key', label: t('sec.kind.key') }]}
            />
          </span>
        </div>
        <div className="el-set">
          <span className="el-set-t"><b>{t(kind === 'password' ? 'sec.password' : 'sec.key')}</b><span>{t('sec.never')}</span></span>
          <span className="el-set-c">
            {kind === 'password' ? (
              <Input aria-label={t('sec.password')} type="password" autoComplete="new-password" mono compact value={value} onChange={(e) => setValue(e.target.value)} />
            ) : (
              <Textarea aria-label={t('sec.key')} mono rows={5} spellCheck={false} autoComplete="off" value={value} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" onChange={(e) => setValue(e.target.value)} />
            )}
          </span>
        </div>
      </div>
      {!nameOk && name.trim() !== '' && <div className="el-note is-err"><Icon name="alert" /><span>{t('sec.badName')}</span></div>}
      {exists && nameOk && <div className="el-note is-warn"><Icon name="alert" /><span>{t('sec.replace')}</span></div>}
      {done && (
        <div className="el-note is-ok">
          <Icon name="check" />
          <span>{t(kind === 'password' ? 'sec.done.password' : 'sec.done.key', { name: done })}</span>
        </div>
      )}
      <div className="el-acts">
        <Button variant="primary" icon="check" disabled={runner.busy || !nameOk || !value.trim()} onClick={save}>{t('sec.save')}</Button>
      </div>
    </section>
  );
}
