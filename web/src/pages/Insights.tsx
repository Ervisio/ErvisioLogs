import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, request, type Project, type SavedQuery } from '../lib/api';
import { items } from '../lib/manage-api';
import { useTenancy } from '../lib/tenancy';
import { projectPath } from '../lib/paths';
import { ActionForm, Field, Page, State, useAction, useRemote } from '../components/Management';
import { formatBytes } from './Storage';

type Panel = {
  id: string;
  project_id: string;
  saved_query_id: string;
  name: string;
  type: 'count' | 'timeseries' | 'top';
  field: string;
  interval_seconds: number;
  limit: number;
};
type PanelData = { items: { label: string; value: number }[]; partial: boolean; searched_tiers: string[] };
type UsageData = {
  items: { timestamp: string; stream: string; events: number; bytes: number }[];
  events: number;
  bytes: number;
  quota: { daily_events: number; daily_bytes: number };
  quota_used: { events: number; bytes: number };
  byte_measurement: string;
  partial: boolean;
};
export function Insights({ project, mode }: { project: Project; mode: 'dashboards' | 'usage' }) {
  const { t } = useTranslation();
  const { workspace } = useTenancy();
  const writable = !!workspace && ['owner', 'admin', 'developer'].includes(workspace.role);
  const tabs = ['dashboards', 'usage'] as const;
  return (
    <Page
      title={t(`nav.${mode}`)}
      subtitle={project.name}
      tabs={tabs.map((key) => ({ path: projectPath(project.workspace_id, project.id, key), label: t(`nav.${key}`) }))}
    >
      {mode === 'dashboards' ? <Panels project={project} writable={writable} /> : <Usage project={project} />}
    </Page>
  );
}
function Panels({ project, writable }: { project: Project; writable: boolean }) {
  const { t } = useTranslation();
  const load = useCallback(
    (signal: AbortSignal) => items<Panel>(`/dashboard-panels?project_id=${encodeURIComponent(project.id)}`, signal),
    [project.id],
  );
  const state = useRemote(load);
  const action = useAction(state.refresh);
  return (
    <>
      <State {...state} retry={state.refresh} />
      {state.data?.length === 0 && <p className="text-ink2">{t('manage.noPanels')}</p>}
      <div className="grid gap-4 md:grid-cols-2">
        {state.data?.map((panel) => (
          <div key={panel.id} className="border-line grid content-start gap-4 rounded-card border p-5">
            <PanelView panel={panel} />
            {writable && (
              <details>
                <summary className="text-hue min-h-11 cursor-pointer content-center font-semibold">
                  {t('manage.edit')}
                </summary>
                <PanelForm project={project} panel={panel} refresh={state.refresh} />
                <button
                  type="button"
                  disabled={action.busy}
                  className="manage-button text-err mt-4"
                  onClick={() => {
                    if (window.confirm(t('manage.deleteNamed', { name: panel.name })))
                      void action.run(() => request('DELETE', `/dashboard-panels/${panel.id}`));
                  }}
                >
                  {t('manage.delete')}
                </button>
              </details>
            )}
          </div>
        ))}
      </div>
      {action.message}
      {writable && (
        <details className="border-line rounded-card border p-5">
          <summary className="cursor-pointer font-semibold">{t('manage.createPanel')}</summary>
          <PanelForm project={project} refresh={state.refresh} />
        </details>
      )}
    </>
  );
}
function PanelView({ panel }: { panel: Panel }) {
  const { t } = useTranslation();
  const load = useCallback(
    (signal: AbortSignal) => request<PanelData>('GET', `/dashboard-panels/${panel.id}/data`, undefined, signal),
    [panel.id],
  );
  const state = useRemote(load);
  const max = Math.max(1, ...(state.data?.items.map((i) => i.value) ?? []));
  return (
    <>
      <div className="flex items-center justify-between gap-3">
        <h2 className="font-bold">{panel.name}</h2>
        <button type="button" className="manage-button" onClick={state.refresh}>
          {t('manage.refresh')}
        </button>
      </div>
      <State {...state} retry={state.refresh} />
      {state.data && (
        <>
          {state.data.partial && <p className="text-warn text-xs">{t('explorer.partial')}</p>}
          {panel.type === 'count' ? (
            <p className="font-mono text-4xl font-semibold">{(state.data.items[0]?.value ?? 0).toLocaleString()}</p>
          ) : (
            <div className="grid gap-3">
              {state.data.items.map((item) => (
                <div key={item.label}>
                  <p className="mb-1 flex items-center justify-between gap-3 text-xs">
                    <span className="truncate" title={item.label}>
                      {panel.type === 'timeseries' ? new Date(item.label).toLocaleString() : item.label}
                    </span>
                    <span className="font-mono">{item.value.toLocaleString()}</span>
                  </p>
                  <div className="bg-sunk h-2 rounded-full">
                    <div className="bg-accent h-full rounded-full" style={{ width: `${(item.value / max) * 100}%` }} />
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </>
  );
}
function PanelForm({ project, panel, refresh }: { project: Project; panel?: Panel; refresh: () => void }) {
  const { t } = useTranslation();
  const load = useCallback((signal: AbortSignal) => api.savedQueries(project.id, signal), [project.id]);
  const state = useRemote<SavedQuery[]>(load);
  const [name, setName] = useState(panel?.name ?? '');
  const [query, setQuery] = useState(panel?.saved_query_id ?? '');
  const [type, setType] = useState<Panel['type']>(panel?.type ?? 'count');
  const [field, setField] = useState(panel?.field ?? 'level');
  const action = useAction(refresh);
  return (
    <ActionForm
      className="mt-4"
      busy={action.busy}
      label={panel ? t('manage.save') : t('manage.create')}
      submit={() =>
        action.run(() =>
          request(panel ? 'PUT' : 'POST', `/dashboard-panels${panel ? `/${panel.id}` : ''}`, {
            project_id: project.id,
            saved_query_id: query,
            name,
            type,
            ...(type === 'top' ? { field } : {}),
            interval_seconds: 3600,
            limit: 20,
          }),
        )
      }
    >
      <State {...state} retry={state.refresh} />
      <Field label={t('manage.name')}>
        <input required className="manage-input" value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <Field label={t('manage.savedQuery')}>
        <select required className="manage-input" value={query} onChange={(e) => setQuery(e.target.value)}>
          <option value="">{t('manage.choose')}</option>
          {state.data?.map((q) => (
            <option key={q.id} value={q.id}>
              {q.name}
            </option>
          ))}
        </select>
      </Field>
      <Field label={t('manage.panelType')}>
        <select className="manage-input" value={type} onChange={(e) => setType(e.target.value as Panel['type'])}>
          {['count', 'timeseries', 'top'].map((key) => (
            <option key={key} value={key}>
              {t(`manage.panel_${key}`)}
            </option>
          ))}
        </select>
      </Field>
      {type === 'top' && (
        <Field label={t('manage.fieldPath')}>
          <input className="manage-input" required value={field} onChange={(e) => setField(e.target.value)} />
        </Field>
      )}
      {action.message}
    </ActionForm>
  );
}
function Usage({ project }: { project: Project }) {
  const { t, i18n } = useTranslation();
  const [range, setRange] = useState('24h');
  const load = useCallback(
    (signal: AbortSignal) =>
      request<UsageData>(
        'GET',
        `/usage?project_id=${project.id}&range=${range}&interval_seconds=3600`,
        undefined,
        signal,
      ),
    [project.id, range],
  );
  const state = useRemote(load);
  const usage = state.data;
  return (
    <>
      <Field label={t('explorer.range')}>
        <select className="manage-input max-w-48" value={range} onChange={(e) => setRange(e.target.value)}>
          {['1h', '24h', '7d', '30d'].map((key) => (
            <option key={key} value={key}>
              {t(`explorer.ranges.${key}`)}
            </option>
          ))}
        </select>
      </Field>
      <State {...state} retry={state.refresh} />
      {usage && (
        <>
          <div className="grid gap-4 sm:grid-cols-2">
            <section className="border-line rounded-card border p-5">
              <h2 className="text-ink2">{t('manage.events')}</h2>
              <p className="mt-2 font-mono text-3xl">{usage.events.toLocaleString()}</p>
            </section>
            <section className="border-line rounded-card border p-5">
              <h2 className="text-ink2">{t('manage.payloadBytes')}</h2>
              <p className="mt-2 font-mono text-3xl">{formatBytes(usage.bytes, i18n.language)}</p>
            </section>
          </div>
          <p className="text-ink3 text-sm">{t('manage.usageHint')}</p>
          <p className="text-ink2">
            {t('manage.dailyQuota')}: {usage.quota.daily_events || t('manage.unlimited')} {t('manage.events')} ·{' '}
            {usage.quota.daily_bytes ? formatBytes(usage.quota.daily_bytes, i18n.language) : t('manage.unlimited')}
          </p>
          {usage.partial && <p className="text-warn">{t('explorer.partial')}</p>}
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr>
                  <th className="py-3">{t('detail.time')}</th>
                  <th>{t('nav.streams')}</th>
                  <th>{t('manage.events')}</th>
                  <th>{t('manage.payloadBytes')}</th>
                </tr>
              </thead>
              <tbody>
                {usage.items.map((row) => (
                  <tr key={`${row.timestamp}:${row.stream}`} className="border-line border-t">
                    <td className="py-3">{new Date(row.timestamp).toLocaleString()}</td>
                    <td>{row.stream}</td>
                    <td className="font-mono">{row.events.toLocaleString()}</td>
                    <td className="font-mono">{formatBytes(row.bytes, i18n.language)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </>
  );
}
export function SystemAlerts() {
  const { t } = useTranslation();
  const { workspaces } = useTenancy();
  const allowed = workspaces.filter((w) => ['owner', 'admin', 'developer'].includes(w.role));
  const [id, setId] = useState(allowed[0]?.id ?? '');
  return (
    <>
      <h2 className="text-xl font-bold">{t('manage.system-alerts')}</h2>
      <p className="text-ink2">{t('manage.systemAlertsHint')}</p>
      <Field label={t('account.workspace')}>
        <select className="manage-input" value={id} onChange={(e) => setId(e.target.value)}>
          {allowed.map((w) => (
            <option key={w.id} value={w.id}>
              {w.name}
            </option>
          ))}
        </select>
      </Field>
      {id && <SystemAlertForm key={id} id={id} />}
    </>
  );
}
function SystemAlertForm({ id }: { id: string }) {
  const { t } = useTranslation();
  const load = useCallback(
    async (signal: AbortSignal) => ({
      channels: await api.channels(id, signal),
      subscriptions: await request<{ channel_ids: string[] }>(
        'GET',
        `/workspaces/${id}/system-alerts`,
        undefined,
        signal,
      ),
    }),
    [id],
  );
  const state = useRemote(load);
  return (
    <>
      <State {...state} retry={state.refresh} />
      {state.data && (
        <SubscriptionEditor
          id={id}
          channels={state.data.channels}
          initial={state.data.subscriptions.channel_ids}
          refresh={state.refresh}
        />
      )}
      {state.data?.channels.length === 0 && <p>{t('channels.none')}</p>}
    </>
  );
}
function SubscriptionEditor({
  id,
  channels,
  initial,
  refresh,
}: {
  id: string;
  channels: { id: string; name: string }[];
  initial: string[];
  refresh: () => void;
}) {
  const [selected, setSelected] = useState(initial ?? []);
  const action = useAction(refresh);
  return (
    <ActionForm
      busy={action.busy}
      submit={() => action.run(() => request('PUT', `/workspaces/${id}/system-alerts`, { channel_ids: selected }))}
    >
      {channels.map((ch) => (
        <label key={ch.id} className="flex min-h-11 items-center gap-3">
          <input
            type="checkbox"
            checked={selected.includes(ch.id)}
            onChange={(e) => setSelected(e.target.checked ? [...selected, ch.id] : selected.filter((v) => v !== ch.id))}
          />
          {ch.name}
        </label>
      ))}
      {action.message}
    </ActionForm>
  );
}
