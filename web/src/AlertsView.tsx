// The Alerts tab: what holds now, what was raised lately, and the summaries of days and weeks. What raises an alert is chosen in Settings.
import { useState } from 'preact/hooks';
import { api } from './api';
import { ago, changeKwh, changeText, costText, digestNotes, digestTitle, SEVERITIES, type Alert, type AlertsAnswer, type Digest } from './alerts';
import { Facts } from './Facts';
import { usePoll } from './hooks';
import { formatKwh, formatWatts } from './lib';
import { routePath } from './route';

const severityLabel = (s: string) => SEVERITIES.find((x) => x.value === s)?.label ?? s;

function AlertRow({ a, now, busy, onDismiss }: { a: Alert; now: Date; busy: boolean; onDismiss: (id: number) => void }) {
  return (
    <li class={`alert-row sev-${a.severity}${a.acked_at ? ' dismissed' : ''}`}>
      <span class="alert-text">
        <strong>{a.title}</strong>
        {a.detail && <span class="alert-detail">{a.detail}</span>}
        <span class="caption">{`${severityLabel(a.severity)} · ${ago(a.opened_at, now)}${a.cleared_at && !a.active && a.updated_at !== a.opened_at ? ` · over ${ago(a.cleared_at, now)}` : ''}`}</span>
      </span>
      <span class="alert-actions">
        {a.link && <a href={a.link}>Open</a>}
        {!a.acked_at && (
          <button type="button" disabled={busy} onClick={() => onDismiss(a.id)}>
            Dismiss
          </button>
        )}
      </span>
    </li>
  );
}

/** One summary: the energy and cost against the period before, what used it, and what else is worth knowing. */
export function DigestCard({ d }: { d: Digest }) {
  const b = d.body;
  const change = changeText(b);
  const notes = digestNotes(b);
  const top = b.top.filter((m) => m.kwh > 0);
  const biggest = Math.max(0.0001, ...top.map((m) => m.kwh));
  return (
    <div class="digest">
      <Facts
        items={[
          { label: 'Energy', value: formatKwh(b.kwh) },
          ...(b.cost != null ? [{ label: 'Cost', value: costText(b.cost, b.cost_estimated) }] : []),
          ...(b.base_watts != null ? [{ label: 'Always on', value: formatWatts(b.base_watts) }] : []),
        ]}
      />
      {change && <p class="note">{change}</p>}
      {top.length > 0 && (
        <ul class="digest-top">
          {top.map((m) => (
            <li key={m.meter_id}>
              <span class="digest-name">
                <a href={routePath('hierarchy', 'meter', m.meter_id)}>{m.label}</a>
              </span>
              <span class="digest-bar" aria-hidden="true">
                <i style={{ width: `${Math.max(2, (m.kwh / biggest) * 100)}%` }} />
              </span>
              <span class="digest-figs">{`${formatKwh(m.kwh)}${m.cost != null ? ` · ${costText(m.cost)}` : ''}`}</span>
              <span class="digest-change">{Math.abs(m.change_kwh) >= 0.05 ? changeKwh(m) : ''}</span>
            </li>
          ))}
        </ul>
      )}
      {b.biggest_rise && <p class="note">{`Biggest rise: ${b.biggest_rise.label}, ${changeKwh(b.biggest_rise)}.`}</p>}
      {notes.map((n) => (
        <p class="caption" key={n}>
          {n}
        </p>
      ))}
    </div>
  );
}

export function AlertsView({ answer, onChanged }: { answer: AlertsAnswer | undefined; onChanged: () => void }) {
  const [version, setVersion] = useState(0);
  const { data: digests } = usePoll(api.digests, 0, [version]);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const now = new Date();

  const dismiss = async (id: number) => {
    setBusy(true);
    try {
      await api.dismissAlert(id);
    } catch (e) {
      setMessage((e as Error).message);
    }
    setBusy(false);
    onChanged();
  };
  const make = async (kind: 'daily' | 'weekly') => {
    setBusy(true);
    setMessage('');
    try {
      await api.makeDigest(kind);
      setVersion((v) => v + 1);
    } catch (e) {
      setMessage((e as Error).message);
    }
    setBusy(false);
  };

  const active = answer?.alerts.filter((a) => a.active) ?? [];
  const recent = answer?.alerts.filter((a) => !a.active) ?? [];
  const list = digests?.digests ?? [];

  return (
    <>
      <header class="page-head">
        <h2>Alerts</h2>
        <p class="note">
          <a href={routePath('settings')}>Choose what raises an alert in Settings</a>
        </p>
      </header>

      <section class="card page-narrow">
        <h2>Needs attention</h2>
        {!answer ? (
          <p class="note">Loading…</p>
        ) : active.length === 0 ? (
          <p class="note">Nothing needs attention.</p>
        ) : (
          <ul class="alert-list">
            {active.map((a) => (
              <AlertRow key={a.id} a={a} now={now} busy={busy} onDismiss={(id) => void dismiss(id)} />
            ))}
          </ul>
        )}
        {message && <p class="note error" role="alert">{message}</p>}
      </section>

      <section class="card page-narrow">
        <h2>Summaries</h2>
        <div class="chips">
          <button type="button" disabled={busy} onClick={() => void make('weekly')}>
            Make last week's summary
          </button>
          <button type="button" disabled={busy} onClick={() => void make('daily')}>
            Make yesterday's summary
          </button>
        </div>
        {list.length === 0 ? (
          <p class="note">No summaries yet. One is made on Monday morning.</p>
        ) : (
          list.map((d, i) => (
            <details key={d.id} class="digest-item" open={i === 0}>
              <summary>
                <strong>{digestTitle(d)}</strong>
                <span class="caption">{d.kind === 'weekly' ? 'Weekly' : 'Daily'}</span>
              </summary>
              <DigestCard d={d} />
            </details>
          ))
        )}
      </section>

      <section class="card page-narrow">
        <h2>Recent</h2>
        {recent.length === 0 ? (
          <p class="note">Nothing in the last month.</p>
        ) : (
          <ul class="alert-list">
            {recent.map((a) => (
              <AlertRow key={a.id} a={a} now={now} busy={busy} onDismiss={(id) => void dismiss(id)} />
            ))}
          </ul>
        )}
      </section>
    </>
  );
}
