// Current and breakers: the card on a meter's page (what it draws in amps now and at most, and how much of its breaker that is) and the field in the
// Hierarchy tab's wiring panel where the breaker's size is said. A 240 V pair's current is the larger leg's, never the two added (the server says so).
import { breakerAdvice, breakerLevel, breakerOptions, breakerPercent, formatAmps, formatVolts } from './amps';
import type { Meter } from './lib';
import { routePath } from './route';

interface Now {
  volts?: number | null | undefined;
  amps?: number | null | undefined;
}

/** What the meter draws in amps right now and at most over the period, and how much of its breaker that is. Hidden for a meter that reports no current. */
export function CurrentCard({ meter, now, peakAmps }: { meter: Meter; now: Now; peakAmps: number | null | undefined }) {
  if (now.amps == null && peakAmps == null) return null;
  const breaker = meter.breaker_amps ?? null;
  const nowPct = breakerPercent(now.amps, breaker);
  const peakPct = breakerPercent(peakAmps, breaker);
  const level = breakerLevel(peakPct);
  const advice = breakerAdvice(peakPct, breaker);
  const settable = meter.scope === 'circuit';
  return (
    <section class={`card${level && level !== 'ok' ? ' warn' : ''}`}>
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
        </div>
        <div class="stat">
          <span class="caption">Of its breaker</span>
          <p class="big small">{peakPct != null ? `${peakPct}%` : '—'}</p>
          <span class="caption">{breaker != null ? `${breaker} A breaker${nowPct != null ? `, ${nowPct}% now` : ''}` : 'size not set'}</span>
        </div>
      </div>
      {peakPct != null && (
        <div class={`breaker-bar ${level ?? ''}`} role="img" aria-label={`The highest current was ${peakPct}% of the breaker`}>
          <i style={{ width: `${Math.min(100, peakPct)}%` }} />
          <b style={{ left: '80%' }} aria-hidden="true" />
        </div>
      )}
      {advice && <p class="breaker-warning">{advice}</p>}
      {breaker == null && settable && (
        <p class="caption">
          Say how big its breaker is (open its row in the <a href={routePath('hierarchy')}>Hierarchy tab</a>, then Breaker size) to see how much of it is used.
        </p>
      )}
      <p class="caption">
        The highest of each minute's readings.{meter.pair_with ? ' A 240 V breaker carries the same current in both of its legs, so this is the larger leg, not the two added.' : ''}
      </p>
    </section>
  );
}

/** The breaker's size, chosen from the usual ones (or "not set"), with the live reading and its share beside it. */
export function BreakerField({ meter, now, onChange }: { meter: Meter; now: Now; onChange: (amps: number | null) => void }) {
  const breaker = meter.breaker_amps ?? null;
  const pct = breakerPercent(now.amps, breaker);
  return (
    <label class="field">
      <span>Breaker size</span>
      <select
        value={breaker == null ? '' : String(breaker)}
        onChange={(e) => {
          const v = (e.target as HTMLSelectElement).value;
          onChange(v === '' ? null : Number(v));
        }}
      >
        <option value="">Not set</option>
        {breakerOptions(breaker).map((a) => (
          <option key={a} value={a}>
            {a} A
          </option>
        ))}
      </select>
      <span class="caption">
        {now.amps != null ? `Now ${formatAmps(now.amps)}${now.volts != null ? ` at ${formatVolts(now.volts)}` : ''}${pct != null ? `, ${pct}% of it` : ''}. ` : ''}
        Only used to say how much of it a current is.
      </span>
    </label>
  );
}
