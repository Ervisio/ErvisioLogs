/** First run: pick how Ervisio Logs runs on this server. Nothing is installed here; Overview offers the install. */
import { useState } from 'react';
import * as logs from '../api/logs';
import { Badge, Button, Icon } from '../kit';
import { errText, t, useLoad } from './util';

export function Setup({ onChosen }: { onChosen(): void }) {
  const dk = useLoad(logs.dockerCheck);
  const [saving, setSaving] = useState<logs.Mode | null>(null);
  const [error, setError] = useState<string | null>(null);

  const choose = async (mode: logs.Mode) => {
    setSaving(mode);
    setError(null);
    try {
      await logs.savePrefs({ mode });
      onChosen();
    } catch (e) {
      setError(errText(e));
      setSaving(null);
    }
  };

  const dockerOk = !!dk.data?.ok;
  return (
    <div className="el-wz">
      <header className="el-ph">
        <span className="el-tile"><Icon name="logs" /></span>
        <div className="el-ph-t">
          <h1>Ervisio Logs</h1>
          <p>{t('setup.lead')}</p>
        </div>
      </header>

      <h2 className="el-wz-h">{t('mode.title')}</h2>
      <div className="el-modes">
        <button type="button" className="el-mode" disabled={!!saving || !dockerOk} onClick={() => choose('docker')}>
          <span className="el-ki hue-file el-mode-ic"><Icon name="layers" /></span>
          <b>{t('mode.docker')}</b>
          <span>{t('mode.docker.text')}</span>
          <span className="el-mode-f">
            {dk.loading ? (
              <Badge tone="neutral">{t('mode.checking')}</Badge>
            ) : dockerOk ? (
              <Badge tone="ok" dot>{t('mode.dockerOk', { v: dk.data!.version })}</Badge>
            ) : (
              <Badge tone="warn" dot>{t('mode.dockerMissing')}</Badge>
            )}
            <small>{t('mode.recommended')}</small>
          </span>
        </button>
        <button type="button" className="el-mode" disabled={!!saving} onClick={() => choose('standalone')}>
          <span className="el-ki hue-sw el-mode-ic"><Icon name="power" /></span>
          <b>{t('mode.standalone')}</b>
          <span>{t('mode.standalone.text')}</span>
          <span className="el-mode-f"><Badge tone="neutral">systemd</Badge></span>
        </button>
      </div>
      {!dk.loading && !dockerOk && <p className="el-muted">{t('mode.dockerHint')}</p>}
      {error && (
        <div className="el-note is-err"><Icon name="alert" /><span>{error}</span></div>
      )}
      <p className="el-muted">{t('mode.later')}</p>
      {saving && <Button loading disabled>{t(`mode.${saving}`)}</Button>}
    </div>
  );
}
