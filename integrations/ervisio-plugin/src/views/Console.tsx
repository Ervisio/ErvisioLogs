/** Console: the full output of the last action, and the recent logs of the app on request. */
import { useEffect, useRef, useState } from 'react';
import * as logs from '../api/logs';
import { Badge, Button, EmptyState, Icon, Select } from '../kit';
import type { Ctx } from './LogsPage';
import { tr } from './Admin';
import { copy, t } from './util';

export function Console({ ctx }: { ctx: Ctx }) {
  const { mode, runner } = ctx;
  const [selected, setSelected] = useState('');
  const jobs = [...(runner.job ? [runner.job] : []), ...runner.history];
  const job = jobs.find((entry) => entry.id === selected) ?? runner.job;
  const box = useRef<HTMLPreElement>(null);
  const lines = job?.lines.length ?? 0;

  useEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines]);

  return (
    <section className="el-card el-console">
      <h3>
        <Icon name="terminal" />
        {job ? job.title : t('nav.console')}
        {job && <Badge tone={job.running ? 'info' : job.failed ? 'err' : 'ok'} dot>{t(job.running ? 'job.running' : job.failed ? 'job.failed' : 'job.done')}</Badge>}
        <span className="el-sp" />
        <Button size="sm" icon="logs" disabled={runner.busy} onClick={() => runner.run(t('act.logs'), (log) => logs.logs(mode, log))}>{t('act.logs')}</Button>
        {job && !job.running && (
          <>
            <Button size="sm" variant="ghost" icon="copy" onClick={() => void copy(job.lines.join('\n'))}>{t('act.copy')}</Button>
            <Button size="sm" variant="ghost" icon="close" disabled={runner.busy} onClick={runner.clear}>{t('act.clear')}</Button>
          </>
        )}
      </h3>
      {!!jobs.length && <Select label={tr('Job history (this session)', 'Storico operazioni (questa sessione)')} value={job?.id ?? ''} options={jobs.map((entry) => ({ value: entry.id, label: `${new Date(entry.startedAt).toLocaleString()} · ${entry.title} · ${t(entry.running ? 'job.running' : entry.failed ? 'job.failed' : 'job.done')}` }))} onChange={setSelected} />}
      {job ? (
        <pre ref={box} className="el-log" aria-live="polite">
          {job.lines.length ? job.lines.join('\n') : t('job.waiting')}
        </pre>
      ) : (
        <EmptyState icon="terminal" hue="term" title={t('console.empty')} text={t('console.emptyText')} />
      )}
    </section>
  );
}
