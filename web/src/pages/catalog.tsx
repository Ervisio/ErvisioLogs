import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { Project } from '../lib/api';
import { management, type EventType, type FieldRule } from '../lib/manage-api';
import { ActionForm, Field, State, useAction, useRemote } from '../components/Management';

const BLANK: EventType = {
  name: '',
  title: '',
  template: '',
  icon: '',
  category: '',
  color: '',
  featured_fields: [],
  discovered: false,
};
export function Catalog({ project, writable }: { project: Project; writable: boolean }) {
  const { t } = useTranslation();
  const load = useCallback((signal: AbortSignal) => management.eventTypes(project.id, signal), [project.id]);
  const state = useRemote(load);
  const action = useAction(state.refresh);
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-bold">{t('manage.catalog')}</h2>
        {writable && (
          <button
            type="button"
            className="manage-button"
            disabled={action.busy}
            onClick={() => void action.run(() => management.preset(project.id))}
          >
            {t('manage.fivemPreset')}
          </button>
        )}
      </div>
      {action.message}
      <State {...state} retry={state.refresh} />
      {state.data?.map((item) => (
        <details key={`${project.id}:${JSON.stringify(item)}`} className="border-line rounded-row border p-4">
          <summary className="cursor-pointer font-semibold">
            {item.title || item.name}
            <code className="text-ink3 ml-3 text-xs">{item.name}</code>
          </summary>
          <EventEditor project={project.id} item={item} writable={writable} refresh={state.refresh} />
        </details>
      ))}
      {writable && (
        <details className="border-line rounded-row border p-4">
          <summary className="cursor-pointer font-semibold">{t('manage.createEvent')}</summary>
          <EventEditor key={project.id} project={project.id} item={BLANK} create writable refresh={state.refresh} />
        </details>
      )}
      <FieldRules project={project.id} writable={writable} />
    </>
  );
}
function EventEditor({
  project,
  item,
  create = false,
  writable,
  refresh,
}: {
  project: string;
  item: EventType;
  create?: boolean;
  writable: boolean;
  refresh: () => void;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState(item);
  const [featuredFields, setFeaturedFields] = useState(item.featured_fields.join(', '));
  const action = useAction(refresh);
  return (
    <ActionForm
      className="mt-5"
      busy={action.busy || !writable}
      submit={() =>
        action.run(async () => {
          await management.putEventType(
            project,
            {
              ...draft,
              featured_fields: featuredFields
                .split(',')
                .map((field) => field.trim())
                .filter(Boolean),
            },
            create,
          );
          if (create) {
            setDraft(BLANK);
            setFeaturedFields('');
          }
        })
      }
    >
      {(['name', 'title', 'template', 'icon', 'category', 'color'] as const).map((key) => (
        <Field
          key={key}
          label={t(`manage.event_${key}`)}
          hint={key === 'template' ? t('manage.templateHint') : undefined}
        >
          <input
            className="manage-input"
            type={key === 'color' ? 'color' : 'text'}
            required={key === 'name'}
            disabled={!writable || (key === 'name' && !create)}
            value={key === 'color' ? draft.color || '#7798ff' : draft[key]}
            maxLength={
              key === 'name'
                ? 128
                : key === 'title'
                  ? 150
                  : key === 'template'
                    ? 2000
                    : key === 'icon'
                      ? 64
                      : key === 'category'
                        ? 100
                        : undefined
            }
            pattern={key === 'name' ? String.raw`[A-Za-z0-9._:\/\-]{1,128}` : undefined}
            onChange={(e) => setDraft({ ...draft, [key]: e.target.value })}
          />
          {key === 'color' && writable && (
            <button
              type="button"
              className="manage-button"
              disabled={!draft.color || action.busy}
              onClick={() => setDraft({ ...draft, color: '' })}
            >
              {t('manage.inherit')}
            </button>
          )}
        </Field>
      ))}
      <Field label={t('manage.featuredFields')}>
        <input
          className="manage-input"
          disabled={!writable}
          value={featuredFields}
          onChange={(e) => setFeaturedFields(e.target.value)}
        />
      </Field>
      {!create && writable && (
        <button
          type="button"
          className="manage-button text-err justify-self-start"
          disabled={action.busy}
          onClick={() => {
            if (window.confirm(t('manage.deleteNamed', { name: item.name })))
              void action.run(() => management.deleteEventType(project, item.name));
          }}
        >
          {t('manage.delete')}
        </button>
      )}
      {action.message}
    </ActionForm>
  );
}
function FieldRules({ project, writable }: { project: string; writable: boolean }) {
  const { t } = useTranslation();
  const load = useCallback((signal: AbortSignal) => management.fieldRules(project, signal), [project]);
  const state = useRemote(load);
  return (
    <section className="grid gap-4 pt-6">
      <h2 className="text-lg font-bold">{t('manage.fieldRules')}</h2>
      <p className="text-ink2">{t('manage.redactionHint')}</p>
      <State {...state} retry={state.refresh} />
      {state.data && (
        <RulesEditor
          key={`${project}:${JSON.stringify(state.data)}`}
          project={project}
          initial={state.data}
          writable={writable}
          refresh={state.refresh}
        />
      )}
    </section>
  );
}
function RulesEditor({
  project,
  initial,
  writable,
  refresh,
}: {
  project: string;
  initial: FieldRule[];
  writable: boolean;
  refresh: () => void;
}) {
  const { t } = useTranslation();
  const [rules, setRules] = useState(initial);
  const action = useAction(refresh);
  const change = (index: number, patch: Partial<FieldRule>) =>
    setRules(rules.map((r, i) => (i === index ? { ...r, ...patch } : r)));
  return (
    <ActionForm
      busy={action.busy || !writable}
      submit={() => action.run(() => management.putFieldRules(project, { items: rules }))}
    >
      {rules.map((rule, index) => (
        <fieldset
          key={index}
          disabled={!writable || action.busy}
          className="border-line grid gap-3 rounded-card border p-4 sm:grid-cols-2"
        >
          <Field label={t('manage.fieldPath')}>
            <input
              required
              className="manage-input"
              value={rule.field}
              onChange={(e) => change(index, { field: e.target.value })}
            />
          </Field>
          <Field label={t('manage.displayName')}>
            <input
              className="manage-input"
              value={rule.label}
              onChange={(e) => change(index, { label: e.target.value })}
            />
          </Field>
          <Field label={t('manage.format')}>
            <select
              className="manage-input"
              value={rule.format}
              onChange={(e) => change(index, { format: e.target.value as FieldRule['format'] })}
            >
              {['text', 'number', 'currency', 'datetime', 'json'].map((format) => (
                <option key={format}>{format}</option>
              ))}
            </select>
          </Field>
          <div className="flex flex-wrap gap-4">
            <label className="flex min-h-11 items-center gap-2">
              <input
                type="checkbox"
                checked={rule.hidden}
                onChange={(e) => change(index, { hidden: e.target.checked })}
              />
              {t('manage.hidden')}
            </label>
            <label className="flex min-h-11 items-center gap-2">
              <input
                type="checkbox"
                checked={rule.redact}
                onChange={(e) => change(index, { redact: e.target.checked })}
              />
              {t('manage.redact')}
            </label>
          </div>
          {writable && (
            <button
              type="button"
              className="manage-button text-err justify-self-start"
              onClick={() => setRules(rules.filter((_, i) => i !== index))}
            >
              {t('manage.delete')}
            </button>
          )}
        </fieldset>
      ))}
      {writable && (
        <button
          type="button"
          className="manage-button justify-self-start"
          disabled={rules.length >= 100 || action.busy}
          onClick={() => setRules([...rules, { field: '', label: '', hidden: false, format: 'text', redact: false }])}
        >
          {t('manage.addField')}
        </button>
      )}
      {action.message}
    </ActionForm>
  );
}
