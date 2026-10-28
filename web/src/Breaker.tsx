// Current and breakers: the card on a meter's page (what it draws in amps now and at most, and how much of its breaker that is) and the field in the
// Hierarchy tab's wiring panel where the breaker's size is said. A 240 V pair's current is the larger leg's, never the two added (the server says so).
import { breakerOptions, breakerPercent, breakerVerdict, formatAmps, formatVolts, type RunCurrent } from './amps';
import type { Meter } from './lib';
import { routePath } from './route';

interface Now {
  volts?: number | null | undefined;
  amps?: number | null | undefined;
}

/** What the meter draws in amps right now and at most over the period, and how much of its breaker that is. Hidden for a meter that reports no current. */
export function CurrentCard({ meter, now, peakAmps, runs }: { meter: Meter; now: Now; peakAmps: number | null | undefined; runs: RunCurrent[] | null }) {
  if (now.amps == null && peakAmps == null) return null;
  const breaker = meter.breaker_amps ?? null;
  const nowPct = breakerPercent(now.amps, breaker);
  const peakPct = breakerPercent(peakAmps, breaker);
  const verdict = breakerVerdict(peakPct, runs, breaker);
  const level = verdict.level;
  const settable = meter.scope === 'circuit' || meter.scope === 'mains';
  return (
    <section class={`card${level === 'sustained' || level === 'over' ? ' warn' : ''}`}>
      <h2>Current</h2>
      <div class="stats three">
        <div class="stat">
          <span class="caption">Right now</span>
          <p class="big small">{formatAmps(now.amps)}</p>
          {now.volts != null && <span class="caption">at {formatVolts(now.volts)}</span>}
        </div>
        <div class="stat">
          <span class="caption">Highest in the period</span>
          <p class="big small">{formatAmps(peakAmps)}</p>
          {meter.pair_with && <span class="caption">larger leg</span>}
        </div>
        <div class="stat">
          <span class="caption">Of its breaker</span>
          <p class="big small">{peakPct != null ? `${peakPct}%` : '—'}</p>
          <span class="caption">{breaker != null ? `${breaker} A breaker${nowPct != null ? `, ${nowPct}% now` : ''}` : 'size not set'}</span>
        </div>
      </div>
      {peakPct != null && (
        <div class={`breaker-bar ${level === 'sustained' ? 'high' : level === 'over' ? 'over' : ''}`} role="img" aria-label={`The highest current was ${peakPct}% of the breaker`}>
          <i style={{ width: `${Math.min(100, peakPct)}%` }} />
          <b style={{ left: '80%' }} aria-hidden="true" />
        </div>
      )}
      {verdict.text && <p class={level === 'brief' ? 'caption' : 'breaker-warning'}>{verdict.text}</p>}
      {breaker == null && settable && (
        <p class="caption">
          <a href={routePath('hierarchy')}>Set the breaker size</a> in Hierarchy
        </p>
      )}
    </section>
  );
}

/** The breaker's size, chosen from the usual ones (or "not set"), with the live reading and its share beside it. */
export function BreakerField({ meter, now, onChange }: { meter: Meter; now: Now; onChange: (amps: number | null) => void }) {
  const breaker = meter.breaker_amps ?? null;
  const pct = breakerPercent(now.amps, breaker);
  return (
    <label class="field">
      <span>Size</span>
      <select
        value={breaker == null ? '' : String(breaker)}
        onChange={(e) => {
          const v = (e.target as HTMLSelectElement).value;
          onChange(v === '' ? null : Number(v));
        }}
      >
        <option value="">Not set</option>
        {breakerOptions(breaker, meter.scope === 'mains').map((a) => (
          <option key={a} value={a}>
            {a} A
          </option>
        ))}
      </select>
      {now.amps != null && (
        <span class="caption">
          Now {formatAmps(now.amps)}
          {now.volts != null ? ` · ${formatVolts(now.volts)}` : ''}
          {pct != null ? ` · ${pct}%` : ''}
        </span>
      )}
    </label>
  );
}
