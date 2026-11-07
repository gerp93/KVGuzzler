// Making or changing a rule of your own about one meter. The kinds are about what the meter does: how much it draws, how long it goes without working, how long it
// works, or its usual pattern, which is learned from its own readings and shown here before the rule is saved.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { patternLines, RULE_KINDS, SEVERITIES, WATCHES, type AlertParams, type AlertPattern, type AlertRule, type AlertWatch, type RuleKind } from './alerts';
import type { Meter } from './lib';

interface Fields {
  op: 'above' | 'below';
  watts: string;
  minutes: string;
  hours: string;
  days: string;
  margin: string;
  watch: AlertWatch;
}

const DEFAULTS: Record<RuleKind, Partial<Fields>> = {
  pattern: { watts: '', days: '30' },
  level: { op: 'above', watts: '5', minutes: '60' },
  quiet: { watts: '50', hours: '24' },
  long: { watts: '50', minutes: '60' },
};

const blank = (): Fields => ({ op: 'above', watts: '', minutes: '', hours: '', days: '30', margin: '', watch: { overdue: true, too_often: true, too_long: true, rest: true } });

function fieldsOf(kind: RuleKind, p: AlertParams): Fields {
  return {
    ...blank(),
    ...DEFAULTS[kind],
    op: p.op ?? 'above',
    watts: p.watts ? String(p.watts) : (DEFAULTS[kind].watts ?? ''),
    minutes: p.minutes ? String(p.minutes) : (DEFAULTS[kind].minutes ?? ''),
    hours: p.hours ? String(p.hours) : (DEFAULTS[kind].hours ?? ''),
    days: p.days ? String(p.days) : '30',
    margin: p.watch?.margin ? String(p.watch.margin) : '',
    watch: p.watch ?? blank().watch,
  };
}

const num = (s: string): number => (s.trim() === '' ? 0 : Number(s));

function paramsOf(kind: RuleKind, f: Fields): AlertParams {
  switch (kind) {
    case 'level':
      return { op: f.op, watts: num(f.watts), minutes: Math.round(num(f.minutes)) };
    case 'quiet':
      return { watts: num(f.watts), hours: num(f.hours) };
    case 'long':
      return { watts: num(f.watts), minutes: Math.round(num(f.minutes)) };
    case 'pattern':
      return { watts: num(f.watts), days: Math.round(num(f.days)), watch: { ...f.watch, ...(f.margin ? { margin: num(f.margin) } : {}) } };
  }
}

export function RuleEditor({ meters, rule, onSaved, onCancel }: { meters: Meter[]; rule?: AlertRule; onSaved: () => void; onCancel: () => void }) {
  const choices = meters.filter((m) => m.enabled).sort((a, b) => a.label.localeCompare(b.label));
  const [meterId, setMeterId] = useState(rule?.meter_id ?? '');
  const [kind, setKind] = useState<RuleKind>((rule?.kind as RuleKind | undefined) ?? 'pattern');
  const [name, setName] = useState(rule?.name ?? '');
  const [severity, setSeverity] = useState<string>(rule ? rule.severity : 'warn');
  const [f, setF] = useState<Fields>(rule ? fieldsOf(rule.kind as RuleKind, rule.params) : fieldsOf('pattern', {}));
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [pattern, setPattern] = useState<AlertPattern>();
  const [patternError, setPatternError] = useState('');

  const set = (patch: Partial<Fields>) => setF((x) => ({ ...x, ...patch }));
  const pick = (k: RuleKind) => {
    setKind(k);
    setF((x) => ({ ...x, ...DEFAULTS[k], watts: DEFAULTS[k].watts ?? '', minutes: DEFAULTS[k].minutes ?? '', hours: DEFAULTS[k].hours ?? '' }));
  };

  // The pattern is read again, a moment after the meter or the level or the days change.
  useEffect(() => {
    if (kind !== 'pattern' || !meterId) {
      setPattern(undefined);
      return;
    }
    let alive = true;
    const id = setTimeout(() => {
      api.alertPattern(meterId, num(f.watts), Math.min(90, Math.max(7, Math.round(num(f.days) || 30)))).then(
        (p) => {
          if (alive) {
            setPattern(p);
            setPatternError('');
          }
        },
        (e: Error) => alive && setPatternError(e.message),
      );
    }, 400);
    return () => {
      alive = false;
      clearTimeout(id);
    };
  }, [kind, meterId, f.watts, f.days]);

  const save = async () => {
    setBusy(true);
    setMessage('');
    const params = paramsOf(kind, f);
    try {
      if (rule) await api.updateAlertRule(rule.id, { meter_id: meterId, name: name.trim() || undefined, severity: severity as 'info', params });
      else await api.addAlertRule({ kind, meter_id: meterId, ...(name.trim() ? { name: name.trim() } : {}), severity: severity as 'info', params });
      onSaved();
    } catch (e) {
      setMessage((e as Error).message);
    }
    setBusy(false);
  };

  const input = (key: 'watts' | 'minutes' | 'hours' | 'days' | 'margin', label: string, placeholder = '') => (
    <label class="field">
      <span>{label}</span>
      <input inputMode="decimal" placeholder={placeholder} value={f[key]} onInput={(e) => set({ [key]: (e.target as HTMLInputElement).value })} />
    </label>
  );

  return (
    <fieldset class="rule-editor">
      <legend>{rule ? 'Change rule' : 'Add a rule'}</legend>
      <label class="field">
        <span>Meter</span>
        <select value={meterId} onChange={(e) => setMeterId((e.target as HTMLSelectElement).value)}>
          <option value="">Choose a meter</option>
          {choices.map((m) => (
            <option key={m.id} value={m.id}>
              {m.label}
            </option>
          ))}
        </select>
      </label>
      {!rule && (
        <label class="field">
          <span>Alert when it</span>
          <select value={kind} onChange={(e) => pick((e.target as HTMLSelectElement).value as RuleKind)}>
            {RULE_KINDS.map((k) => (
              <option key={k.value} value={k.value}>
                {k.label}
              </option>
            ))}
          </select>
        </label>
      )}
      <p class="caption">{RULE_KINDS.find((k) => k.value === kind)?.help}</p>

      {kind === 'level' && (
        <>
          <label class="field">
            <span>Draws</span>
            <select value={f.op} onChange={(e) => set({ op: (e.target as HTMLSelectElement).value as 'above' | 'below' })}>
              <option value="above">Above</option>
              <option value="below">Below</option>
            </select>
          </label>
          {input('watts', 'Level (W)')}
          {input('minutes', 'For at least (min)')}
        </>
      )}
      {kind === 'quiet' && (
        <>
          {input('watts', 'Working means above (W)')}
          {input('hours', 'Not working for (hours)')}
        </>
      )}
      {kind === 'long' && (
        <>
          {input('watts', 'Working means above (W)')}
          {input('minutes', 'Working for more than (min)')}
        </>
      )}
      {kind === 'pattern' && (
        <>
          <div class="toggles" role="group" aria-label="What to watch for">
            {WATCHES.map((w) => (
              <label key={w.key}>
                <input type="checkbox" checked={!!f.watch[w.key]} onChange={(e) => set({ watch: { ...f.watch, [w.key]: (e.target as HTMLInputElement).checked } })} /> {w.label}
              </label>
            ))}
          </div>
          {meterId && (
            <div class="pattern-preview" aria-live="polite">
              {patternError ? (
                <p class="note error">{patternError}</p>
              ) : !pattern ? (
                <p class="note">Reading its history…</p>
              ) : (
                patternLines(pattern).map((l) => (
                  <p class={pattern.ok ? 'note' : 'note error'} key={l}>
                    {l}
                  </p>
                ))
              )}
            </div>
          )}
          <details>
            <summary>Adjust</summary>
            {input('watts', 'Working means above (W)', 'from its own readings')}
            {input('days', 'Learn from the last (days)', '30')}
            {input('margin', 'How far past usual counts', '1.25')}
          </details>
        </>
      )}

      <label class="field">
        <span>Name</span>
        <input value={name} maxLength={80} placeholder="Optional" onInput={(e) => setName((e.target as HTMLInputElement).value)} />
      </label>
      <label class="field">
        <span>Severity</span>
        <select value={severity} onChange={(e) => setSeverity((e.target as HTMLSelectElement).value)}>
          {SEVERITIES.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </label>
      {message && <p class="note error" role="alert">{message}</p>}
      <div class="chips">
        <button type="button" disabled={busy || !meterId} onClick={() => void save()}>
          Save
        </button>
        <button type="button" disabled={busy} onClick={onCancel}>
          Cancel
        </button>
      </div>
    </fieldset>
  );
}
