// What running the computer would cost at the saved price: at a load typed in, at what it draws right now, or at what it really used per day.
import { api } from './api';
import { usePoll, useStored } from './hooks';
import {
  ESTIMATE_WINDOWS, estimateLines, formatKwh, formatMoney, formatWatts, usageBasis,
  type EstimateMode,
} from './lib';

const MODES: { key: EstimateMode; label: string }[] = [
  { key: 'constant', label: 'Constant load' },
  { key: 'now', label: 'Right now' },
  { key: 'usage', label: 'My usage' },
];

export function Estimator({ id, plugIds, includePc, nowWatts, nowWhat }: {
  id: string;
  /** The ids of the plugs counted next to the PC. */
  plugIds: string[];
  includePc: boolean;
  /** What the counted parts draw right now, or null when unknown. */
  nowWatts: number | null;
  nowWhat: string;
}) {
  const [mode, setMode] = useStored('estimate-mode', 'constant');
  const [typed, setTyped] = useStored('estimate-watts', '400');
  const [window, setWindow] = useStored('estimate-window', '7d');

  // The price and the real usage come from the server (it knows the bills); the other two modes only need the price.
  const { data: est, error } = usePoll(() => api.computerEstimate(id, window, plugIds, includePc), 60_000, [id, window, plugIds.join(','), includePc]);

  let kwhPerDay: number | null = null;
  let perHour = true;
  let basis = '';
  let message = '';
  if (mode === 'constant') {
    const watts = Number(typed);
    if (typed.trim() === '' || !Number.isFinite(watts) || watts < 0) message = 'Enter a number of watts, for example 400.';
    else {
      kwhPerDay = (watts * 24) / 1000;
      basis = `If the PC drew ${watts} W all day, every day.`;
    }
  } else if (mode === 'now') {
    if (nowWatts == null) message = 'Waiting for a power reading. Live readings need a plug on the PC or its collector running.';
    else {
      kwhPerDay = (nowWatts * 24) / 1000;
      basis = `If it kept drawing what it draws right now (${formatWatts(nowWatts)}, ${nowWhat}) all day, every day.`;
    }
  } else {
    perHour = false;
    if (!est) message = error ? error.message : 'Loading…';
    else if (!est.usage) message = 'Not enough history yet: this needs at least an hour recorded, and something counted.';
    else {
      kwhPerDay = est.usage.kwh_per_day;
      basis = usageBasis(est.usage, window);
    }
  }

  const rate = est?.rate ?? null;
  const lines = kwhPerDay != null ? estimateLines(kwhPerDay, rate, perHour) : [];

  return (
    <section class="card">
      <h2>Cost estimator</h2>
      <p class="caption">What running it would cost at the rate saved in Settings.</p>
      <div class="seg" role="group" aria-label="Estimate from">
        {MODES.map((m) => (
          <button key={m.key} type="button" aria-pressed={m.key === mode} onClick={() => setMode(m.key)}>
            {m.label}
          </button>
        ))}
      </div>

      {mode === 'constant' && (
        <p>
          <label>
            Load:{' '}
            <input type="number" min="0" step="any" value={typed} onInput={(e) => setTyped((e.target as HTMLInputElement).value)} style="width: 6rem" />{' '}
            watts, all the time
          </label>
        </p>
      )}
      {mode === 'usage' && (
        <p>
          <label>
            Average over:{' '}
            <select value={window} onChange={(e) => setWindow((e.target as HTMLSelectElement).value)}>
              {ESTIMATE_WINDOWS.map((w) => (
                <option key={w.key} value={w.key}>
                  {w.label}
                </option>
              ))}
            </select>
          </label>
        </p>
      )}

      {basis && <p class="caption">{basis}</p>}
      {message && <p class="note">{message}</p>}
      {lines.length > 0 && (
        <ul class="settings">
          {lines.map((l) => (
            <li key={l.label}>
              <span class="grow">{l.label}</span>
              <span class="num">{formatKwh(l.kwh)}</span>
              <span class="num cost">{l.cost != null ? formatMoney(l.cost, est?.rate_estimated ?? false) : ''}</span>
            </li>
          ))}
        </ul>
      )}
      {lines.length > 0 && rate == null && <p class="caption">Save your electricity rate in Settings to see the cost.</p>}
    </section>
  );
}
