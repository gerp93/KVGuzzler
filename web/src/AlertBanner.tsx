// What needs attention, across the top of every page: the alerts nobody has dismissed, the worst first. Dismissing one only takes it off the banner; it stays in
// the Alerts tab while its condition holds.
import { useState } from 'preact/hooks';
import { api } from './api';
import { ago, severityRank, type Alert } from './alerts';
import { routePath } from './route';

/** How many alerts the banner lists before "N more". */
const SHOWN = 3;

export function AlertBanner({ banner, onChanged }: { banner: Alert[]; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  if (banner.length === 0) return null;
  const sorted = [...banner].sort((a, b) => severityRank(b.severity) - severityRank(a.severity) || Date.parse(b.opened_at) - Date.parse(a.opened_at));
  const shown = sorted.slice(0, SHOWN);
  const first = sorted[0];
  const hidden = sorted.length - shown.length;
  const now = new Date();

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
    } catch {
      // A failed dismissal leaves the alert where it is; the next look shows it again.
    }
    setBusy(false);
    onChanged();
  };

  return (
    <section class={`alert-banner sev-${first?.severity ?? 'info'}`} role={first?.severity === 'urgent' ? 'alert' : 'status'} aria-label="Alerts">
      <ul>
        {shown.map((a) => (
          <li key={a.id} class={`sev-${a.severity}`}>
            <span class="alert-text">
              <strong>{a.title}</strong>
              {a.detail && <span class="alert-detail">{a.detail}</span>}
            </span>
            <span class="alert-when">{ago(a.opened_at, now)}</span>
            <span class="alert-actions">
              {a.link && <a href={a.link}>Open</a>}
              <button type="button" disabled={busy} onClick={() => void run(() => api.dismissAlert(a.id))}>
                Dismiss
              </button>
            </span>
          </li>
        ))}
      </ul>
      {sorted.length > 1 && (
        <p class="alert-more">
          {hidden > 0 && <a href={routePath('alerts')}>{`${hidden} more`}</a>}
          <button type="button" disabled={busy} onClick={() => void run(api.dismissAllAlerts)}>
            Dismiss all
          </button>
        </p>
      )}
    </section>
  );
}
