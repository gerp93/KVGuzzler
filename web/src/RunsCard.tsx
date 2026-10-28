// A meter's runs: each time it was working (a car charging, a dryer drying), with its length, energy and cost, and a line that sums them up.
// Hidden for a meter that barely changes. The server finds the runs from the recorded minutes; nothing here is added to any other figure.
import { useState } from 'preact/hooks';
import { api } from './api';
import { breakerLevel, breakerPercent, formatAmps, typicalAmps } from './amps';
import { formatMoney } from './lib';
import { runBars, runLength, runStats, runWhen, type MeterRuns } from './runs';
import { Facts } from './Facts';
import { Kwh } from './Watts';

const SHOWN = 8;

export function RunsCard({ runs, breaker }: { runs: MeterRuns; breaker?: number | null }) {
  const [all, setAll] = useState(false);
  const stats = runStats(runs);
  const list = all ? runs.runs : runs.runs.slice(0, SHOWN);
  const bars = runBars(list);
  const usualAmps = typicalAmps(runs.runs);
  const now = new Date();
  return (
    <section class="card">
      <h2>Runs</h2>
      {!stats && <p class="note">No runs in this period.</p>}
      {stats && (
        <>
          <div class="stats compact">
            <div class="stat">
              <span class="caption">Runs</span>
              <p class="big small">{stats.count}</p>
            </div>
            <div class="stat">
              <span class="caption">Average run</span>
              <p class="big small"><Kwh k={stats.avgKwh} /></p>
              {stats.avgCost != null && <span class="caption">{formatMoney(stats.avgCost.toFixed(2), runs.cost_estimated)}</span>}
            </div>
            <div class="stat">
              <span class="caption">Average length</span>
              <p class="big small">{runLength(stats.avgMinutes)}</p>
              <span class="caption">longest {runLength(stats.longestMinutes)}</span>
            </div>
            {usualAmps != null && (
              <div class="stat">
                <span class="caption">Typical current</span>
                <p class="big small">{formatAmps(usualAmps)}</p>
                {breaker ? <span class="caption">{breakerPercent(usualAmps, breaker)}% of {breaker} A</span> : null}
              </div>
            )}
          </div>
          <ol class="runs" aria-label="Recent runs, newest first">
            {list.map((r, i) => (
              <li key={r.start}>
                <span class="when">{runWhen(r.start, now)}</span>
                <span class="bar" aria-hidden="true">
                  <i style={{ width: `${bars[i]}%` }} />
                </span>
                <span class={`num len${r.minutes >= 180 && breakerLevel(breakerPercent(r.avg_amps, breaker)) !== null && breakerLevel(breakerPercent(r.avg_amps, breaker)) !== 'ok' ? ' hot' : ''}`} title={r.avg_amps != null ? `${formatAmps(r.avg_amps)} on average, ${formatAmps(r.peak_amps)} at most` : undefined}>
                  {runLength(r.minutes)}
                  {r.avg_amps != null ? ` · ${formatAmps(r.avg_amps)}` : ''}
                </span>
                <span class="num cost">
                  <Kwh k={r.kwh} />
                  {r.cost != null ? ` · ${formatMoney(r.cost, runs.cost_estimated)}` : ''}
                </span>
              </li>
            ))}
          </ol>
          <a class="button" href={api.runsExportUrl(runs.meter_id, runs.from, runs.to)} download>
            Download CSV
          </a>
          {runs.runs.length > SHOWN && (
            <button type="button" class="link" onClick={() => setAll(!all)}>
              {all ? 'Show fewer' : `Show all ${runs.runs.length}`}
            </button>
          )}
          <Facts
            items={[
              { label: 'Working level', value: `above ${Math.round(runs.threshold_watts ?? 0)} W` },
              ...(stats.shareOfMeter != null ? [{ label: 'Share of energy', value: `${stats.shareOfMeter}%` }] : []),
              ...(runs.truncated ? [{ label: 'Listed', value: 'newest 500' }] : []),
            ]}
          />
        </>
      )}
    </section>
  );
}
