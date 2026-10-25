// A meter's runs: each time it was working (a car charging, a dryer drying), with its length, energy and cost, and a line that sums them up.
// Hidden for a meter that barely changes. The server finds the runs from the recorded minutes; nothing here is added to any other figure.
import { useState } from 'preact/hooks';
import { formatKwh, formatMoney, formatWatts } from './lib';
import { runBars, runLength, runStats, runWhen, type MeterRuns } from './runs';

const SHOWN = 8;

export function RunsCard({ runs }: { runs: MeterRuns }) {
  const [all, setAll] = useState(false);
  const stats = runStats(runs);
  const list = all ? runs.runs : runs.runs.slice(0, SHOWN);
  const bars = runBars(list);
  const now = new Date();
  return (
    <section class="card">
      <h2>Runs</h2>
      {!stats && <p class="note">No run in this period (it works above about {formatWatts(runs.threshold_watts)}).</p>}
      {stats && (
        <>
          <p class="runs-summary">
            <strong>{stats.count}</strong> {stats.count === 1 ? 'run' : 'runs'} · about <strong>{formatKwh(stats.avgKwh)}</strong>
            {stats.avgCost != null && <> and <strong>{formatMoney(stats.avgCost.toFixed(2), runs.cost_estimated)}</strong></>} each · {runLength(stats.avgMinutes)} on average, the longest {runLength(stats.longestMinutes)}
          </p>
          <ol class="runs" aria-label="Recent runs, newest first">
            {list.map((r, i) => (
              <li key={r.start}>
                <span class="when">{runWhen(r.start, now)}</span>
                <span class="bar" aria-hidden="true">
                  <i style={{ width: `${bars[i]}%` }} />
                </span>
                <span class="num len">{runLength(r.minutes)}</span>
                <span class="num cost">
                  {formatKwh(r.kwh)}
                  {r.cost != null ? ` · ${formatMoney(r.cost, runs.cost_estimated)}` : ''}
                </span>
              </li>
            ))}
          </ol>
          {runs.runs.length > SHOWN && (
            <button type="button" class="link" onClick={() => setAll(!all)}>
              {all ? 'Show fewer' : `Show all ${runs.runs.length}`}
            </button>
          )}
          <p class="caption">
            {stats.shareOfMeter != null ? `Runs hold ${stats.shareOfMeter}% of this meter's energy in the period; the rest is what it draws at rest. ` : ''}
            A run is a stretch above about {formatWatts(runs.threshold_watts)}, found from the recorded minutes; a short dip does not end it.
            {runs.truncated ? ' Only the newest 500 are listed; the figures count them all.' : ''}
          </p>
        </>
      )}
    </section>
  );
}
