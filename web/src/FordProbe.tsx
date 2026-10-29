// Asks every read-only Ford endpoint once and shows what each answered, so what this link can read is seen rather than guessed. Only the names of the
// fields are shown, and the values of those about the vehicle's kind, the battery, charging, schedules and alerts: never who the vehicle is or where it is.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import type { FordProbe as Probe } from './lib';

type Result = Probe['results'][number];

/** One endpoint's outcome in a few words. */
function outcome(r: Result): { word: string; tone: 'ok' | 'warn' | 'off' } {
  if (r.ok) return { word: 'Answered', tone: 'ok' };
  if (r.status === 0) return { word: 'Not asked', tone: 'off' };
  if (r.status === 403 || r.status === 401) return { word: `Refused (${r.status})`, tone: 'warn' };
  if (r.status === 404) return { word: 'Nothing there (404)', tone: 'off' };
  return { word: `Error (${r.status})`, tone: 'warn' };
}

export function FordProbe({ waitLeft, wait, minGap, reload }: { waitLeft: number; wait: (seconds: number) => void; minGap: number; reload: () => void }) {
  const [probe, setProbe] = useState<Probe>();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  useEffect(() => {
    api.fordProbe().then(setProbe, () => undefined);
  }, []);

  const ask = () => {
    wait(minGap);
    setBusy(true);
    setMessage('');
    api.probeFord().then(
      (p) => {
        setBusy(false);
        setProbe(p);
        reload();
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
        reload();
      },
    );
  };

  const results = probe?.results ?? [];
  return (
    <details class="ford-probe">
      <summary>Check all Ford data</summary>
      <p class="caption">Asks each read-only Ford endpoint once, to see what this link can read: telemetry, garage, alerts, charge schedules, departure times and the wallbox.</p>
      <div class="chips save">
        <button type="button" disabled={busy || waitLeft > 0} onClick={ask}>
          {waitLeft > 0 ? `Check · wait ${waitLeft} s` : 'Check all Ford data'}
        </button>
      </div>
      {message && <p class="note error">{message}</p>}
      {probe?.checked_at && <p class="caption">As of {new Date(probe.checked_at).toLocaleString()}</p>}
      {results.map((r) => {
        const o = outcome(r);
        return (
          <div class="probe-row" key={r.endpoint}>
            <p class={`ford-state ${o.tone}`}>
              <span class="dot" aria-hidden="true" />
              <strong>{r.endpoint}</strong>
              <span class="caption"> · {o.word}</span>
            </p>
            {r.message && <p class="caption">{r.message}</p>}
            {r.signals && r.signals.length > 0 && (
              <details>
                <summary class="caption">{r.signals.length} fields</summary>
                <table class="probe-table">
                  <tbody>
                    {r.signals.map((s) => (
                      <tr key={s.path}>
                        <td>
                          <code>{s.path}</code>
                        </td>
                        <td>{s.kind}</td>
                        <td>{s.value ?? ''}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </details>
            )}
          </div>
        );
      })}
    </details>
  );
}
