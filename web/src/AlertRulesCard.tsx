// What raises an alert: the built-in rules, each switched on or off with its own settings, and rules of your own about one meter. A change is saved as it is made.
import { useState } from 'preact/hooks';
import { api } from './api';
import { ruleSummary, SEVERITIES, type AlertRule } from './alerts';
import { usePoll } from './hooks';
import type { Meter } from './lib';
import { RuleEditor } from './RuleEditor';

type Editing = { kind: 'new' } | { kind: 'edit'; rule: AlertRule } | undefined;

function BuiltinRow({ rule, onChange }: { rule: AlertRule; onChange: (patch: Parameters<typeof api.updateAlertRule>[1]) => Promise<void> }) {
  const fields = rule.fields ?? [];
  const [draft, setDraft] = useState<Record<string, string>>({});
  const value = (key: string): string => {
    if (draft[key] !== undefined) return draft[key];
    const p = rule.params as Record<string, number | undefined>;
    return p[key] != null ? String(p[key]) : '';
  };
  const commit = (key: string) => {
    const text = draft[key];
    if (text === undefined) return;
    void onChange({ params: { ...rule.params, [key]: Number(text) } }).then(() => setDraft((d) => ({ ...d, [key]: undefined as unknown as string })));
  };
  return (
    <li class="rule-row">
      <label class="rule-switch">
        <input type="checkbox" checked={rule.enabled} onChange={(e) => void onChange({ enabled: (e.target as HTMLInputElement).checked })} />
        <span class="rule-name">{rule.name}</span>
        {rule.open > 0 && <span class="rule-open">{`${rule.open} open`}</span>}
      </label>
      {rule.help && <p class="caption">{rule.help}</p>}
      {rule.enabled && (
        <div class="rule-fields">
          {fields.map((f) => (
            <label key={f.key} class="rule-field">
              <span>{f.label}</span>
              <input
                type="number"
                inputMode="decimal"
                min={f.min}
                max={f.max}
                step={f.step}
                value={value(f.key)}
                onInput={(e) => setDraft((d) => ({ ...d, [f.key]: (e.target as HTMLInputElement).value }))}
                onBlur={() => commit(f.key)}
              />
              <span class="unit">{f.unit}</span>
            </label>
          ))}
          <label class="rule-field">
            <span>Severity</span>
            <select value={rule.severity} onChange={(e) => void onChange({ severity: (e.target as HTMLSelectElement).value as 'info' })}>
              {SEVERITIES.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
    </li>
  );
}

export function AlertRulesCard({ meters }: { meters: Meter[] }) {
  const [version, setVersion] = useState(0);
  const { data, error } = usePoll(api.alertRules, 0, [version]);
  const [editing, setEditing] = useState<Editing>();
  const [removing, setRemoving] = useState<string>();
  const [message, setMessage] = useState('');

  const change = async (id: string, patch: Parameters<typeof api.updateAlertRule>[1]) => {
    setMessage('');
    try {
      await api.updateAlertRule(id, patch);
    } catch (e) {
      setMessage((e as Error).message);
    }
    setVersion((v) => v + 1);
  };
  const remove = async (id: string) => {
    try {
      await api.removeAlertRule(id);
    } catch (e) {
      setMessage((e as Error).message);
    }
    setRemoving(undefined);
    setVersion((v) => v + 1);
  };

  if (!data) {
    return (
      <section class="card page-narrow">
        <h2>Alerts</h2>
        <p class="note">{error?.message || 'Loading…'}</p>
      </section>
    );
  }
  const builtin = data.rules.filter((r) => r.builtin);
  const custom = data.rules.filter((r) => !r.builtin);

  return (
    <section class="card page-narrow alert-rules">
      <h2>Alerts</h2>
      <ul class="rule-list">
        {builtin.map((r) => (
          <BuiltinRow key={r.id} rule={r} onChange={(patch) => change(r.id, patch)} />
        ))}
      </ul>

      <h3>Your rules</h3>
      {custom.length === 0 && !editing && <p class="note">None yet.</p>}
      <ul class="rule-list">
        {custom.map((r) => (
          <li key={r.id} class="rule-row">
            <label class="rule-switch">
              <input type="checkbox" checked={r.enabled} onChange={(e) => void change(r.id, { enabled: (e.target as HTMLInputElement).checked })} />
              <span class="rule-name">{r.name}</span>
              {r.open > 0 && <span class="rule-open">{`${r.open} open`}</span>}
            </label>
            <p class="caption">{ruleSummary(r)}</p>
            <div class="chips">
              <button type="button" onClick={() => setEditing({ kind: 'edit', rule: r })}>
                Edit
              </button>
              <button type="button" onClick={() => (removing === r.id ? void remove(r.id) : setRemoving(r.id))} onBlur={() => setRemoving(undefined)}>
                {removing === r.id ? 'Remove?' : 'Remove'}
              </button>
            </div>
          </li>
        ))}
      </ul>
      {editing ? (
        <RuleEditor
          key={editing.kind === 'edit' ? editing.rule.id : 'new'}
          meters={meters}
          rule={editing.kind === 'edit' ? editing.rule : undefined}
          onSaved={() => {
            setEditing(undefined);
            setVersion((v) => v + 1);
          }}
          onCancel={() => setEditing(undefined)}
        />
      ) : (
        <div class="chips">
          <button type="button" onClick={() => setEditing({ kind: 'new' })}>
            Add a rule
          </button>
        </div>
      )}
      {message && <p class="note error" role="alert">{message}</p>}
    </section>
  );
}
