import { useState } from 'react';
import * as logs from '../api/logs';
import { Button, EmptyState, Skeleton } from '../kit';
import { Setup } from './Setup';
import { Management } from './management/Management';
import { errText, t, useLoad, useRunner, type Runner } from './util';
export type SectionId = 'overview' | 'address' | 'settings' | 'console';
export interface Ctx {
  mode: logs.Mode;
  status: logs.Status;
  reload(): void;
  runner: Runner;
  go(s: SectionId): void;
  changeMode(): void;
  settingsEdits: Record<string, string>;
  setSettingsEdits(edits: Record<string, string>): void;
}
export function LogsPage() {
  const prefs = useLoad(logs.loadPrefs);
  if (!prefs.data) {
    if (prefs.loading) return <Loading />;
    return <Management setup={<EmptyState icon="alert" title={t('error.title')} text={errText(prefs.error)} action={<Button onClick={prefs.reload}>{t('retry')}</Button>} />} />;
  }
  if (!prefs.data.mode) return <Management setup={<Setup onChosen={prefs.reload} />} />;
  return <Installed mode={prefs.data.mode} onModeReset={async () => { await logs.savePrefs({ mode: null }); prefs.reload(); }} />;
}
function Loading() {
return <div className="el-root">
    <Skeleton height={56} />
    <Skeleton height={320} />
  </div>;
}
function Installed({ mode, onModeReset }: { mode: logs.Mode; onModeReset(): void }) {
  const status = useLoad(() => logs.status(mode), [mode]);
  const runner = useRunner(status.reload);
  const [settingsEdits, setSettingsEdits] = useState<Record<string, string>>({});
  if (!status.data) {
    if (status.loading) return <Loading />;
    return <Management setup={<EmptyState icon="alert" title={t('error.title')} text={errText(status.error)} action={<Button onClick={status.reload}>{t('retry')}</Button>} />} />;
  }
  const ctx: Ctx = { mode, status: status.data, reload: status.reload, runner, go: () => { }, changeMode: onModeReset, settingsEdits, setSettingsEdits };
  return <Management ctx={ctx} />;
}
