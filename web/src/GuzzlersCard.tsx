// The top guzzlers of the period picked on the Live page, in two views that switch at the top: the ones that WORK (they go up and down, and pull a lot while they
// run: an EV charger, a dryer) ranked by the energy they used above their idle level, and the ones that REST (always on at a steady draw: a fridge, a network
// closet) ranked by that always-on power, under the house's own always-on figures (what it never goes below, a year of it). Only meters that hold nothing are ranked (see guzzlers.ts), so no energy is listed twice and nothing is added to anything.
import { useState } from 'preact/hooks';
import { api } from './api';
import { BaseLoadStats } from './BaseLoadCard';
import { meterPath } from './Bars';
import { topGuzzlers, type GuzzlerBy, type GuzzlerMode } from './guzzlers';
import { usePoll, useStored } from './hooks';
import { formatMoney, periodRange, periodWords, type Meter, type PeriodKey, type Summary } from './lib';
import { Kwh, Watts } from './Watts';

const SHOWN = 5;

/** What an always-on level is learned from is at least this long, so a short period (an hour) still has one: a level needs an hour or more of minutes, and an hour is no sample of "hardly ever goes below". */
const MIN_LOOK_MS = 24 * 3600_000;

/** The start of the window the always-on levels are read over: the period's, or the 24 hours before its end when the period is shorter. */
function widen(from: string, to: string): string {
  return new Date(Math.min(Date.parse(from), Date.parse(to) - MIN_LOOK_MS)).toISOString();
}

export function GuzzlersCard({ meters, summary, houseKwh, period }: { meters: Meter[]; summary: Summary | undefined; houseKwh: number | null | undefined; period: PeriodKey }) {
  const [savedMode, setMode] = useStored('guzzlers-mode', 'active');
  const [saved, setBy] = useStored('guzzlers-by', 'kwh');
  const [all, setAll] = useState(false);
  // What each appliance hardly ever goes below over the period (the server lists the biggest few): the line between resting and working.
  const { data: base } = usePoll(
    async () => {
      const { from, to } = periodRange(period, new Date());
      return (await api.houseBaseLoad(widen(from, to), to)) ?? null;
    },
    60_000,
    [period],
  );
  const widened = periodRange(period, new Date());
  const lookedFurther = Date.parse(widened.to) - Date.parse(widened.from) < MIN_LOOK_MS;
  const rests = new Map((base?.parts ?? []).map((p) => [p.meter_id, p.watts]));
  const priced = (summary?.meters ?? []).some((f) => f.cost != null && f.kwh > 0);
  const mode: GuzzlerMode = savedMode === 'rest' ? 'rest' : 'active';
  const by: GuzzlerBy = saved === 'cost' && priced ? 'cost' : 'kwh';
  const working = topGuzzlers(meters, summary, by, houseKwh, 'active', rests);
  const resting = topGuzzlers(meters, summary, by, houseKwh, 'rest', rests);
  if (working.length === 0 && resting.length === 0) return null;
  const list = mode === 'rest' ? resting : working;
  const shown = all ? list : list.slice(0, SHOWN);
  // The bars are against the biggest, so the order reads at a glance: the always-on power at rest, else energy (or cost when that is the order).
  const value = (g: { kwh: number; cost: string | null; restWatts: number }): number => (mode === 'rest' ? g.restWatts : by === 'cost' ? Number(g.cost ?? 0) : g.kwh);
  const top = Math.max(...shown.map(value), 0);
  return (
    <section class="card guzzlers">
      <div class="row">
        <h2>Top guzzlers</h2>
        <div class="seg" role="group" aria-label="Which guzzlers">
          <button type="button" aria-pressed={mode === 'active'} onClick={() => setMode('active')} title="The ones that go up and down: what they pull while they run">
            Active
          </button>
          <button type="button" aria-pressed={mode === 'rest'} onClick={() => setMode('rest')} title="The ones that are always on: the steady draw they hardly ever go below">
            At rest
          </button>
        </div>
      </div>
      <p class="caption">
        {mode === 'rest'
          ? `Always on, over ${periodWords(period)}: the steady draw each one hardly ever goes below, day or night.${lookedFurther ? ' The level is learned from the last 24 hours, so a short period still has one.' : ''}`
          : `Working, over ${periodWords(period)}: the energy each one used above its idle level, with the most it pulled.`}
      </p>
      {mode === 'rest' && base && <BaseLoadStats data={base} what="this meter" span={lookedFurther ? 'the last 24 hours' : 'the period'} />}
      {mode === 'active' && priced && (
        <div class="seg small" role="group" aria-label="Rank by">
          <button type="button" aria-pressed={by === 'kwh'} onClick={() => setBy('kwh')}>
            Energy
          </button>
          <button type="button" aria-pressed={by === 'cost'} onClick={() => setBy('cost')}>
            Cost
          </button>
        </div>
      )}
      {list.length === 0 ? (
        <p class="note">
          {mode === 'rest' ? 'Nothing is known to rest yet: what each one hardly ever goes below needs at least an hour recorded.' : 'Nothing worked over this period.'}
        </p>
      ) : (
        <ol class={`guzzler-list${mode === 'rest' ? ' rest' : ''}`} aria-label={mode === 'rest' ? 'The biggest always-on draws, biggest first' : 'The hardest workers, biggest first'}>
          <li class="head" aria-hidden="true">
            <span />
            <span>Meter</span>
            <span class="bar-head peak" />
            <span class="num peak">{mode === 'rest' ? 'Always on' : 'Peak'}</span>
            <span class="num">Energy</span>
            <span class="num">Cost · share</span>
          </li>
          {shown.map((g, i) => (
            <li key={g.meter.id}>
              <span class="rank" aria-hidden="true">
                {i + 1}
              </span>
              <a class="name" href={meterPath(g.meter)}>
                {g.meter.label}
              </a>
              <span class="bar" aria-hidden="true">
                <i style={{ width: `${top > 0 ? Math.max(1, (value(g) / top) * 100) : 0}%` }} />
              </span>
              <span class="num peak" title={mode === 'rest' ? 'The power it hardly ever goes below' : 'The most it drew in the period'}>
                {mode === 'rest' ? (
                  <>
                    <Watts w={g.restWatts} /> on
                  </>
                ) : g.peakWatts != null ? (
                  <>
                    up to <Watts w={g.peakWatts} />
                  </>
                ) : (
                  ''
                )}
              </span>
              <span class="num"><Kwh k={g.kwh} /></span>
              <span class="num cost">
                {g.cost != null ? formatMoney(g.cost, summary?.cost_estimated) : '—'}
                {g.share != null ? ` · ${g.share}%` : ''}
              </span>
            </li>
          ))}
        </ol>
      )}
      {list.length > SHOWN && (
        <button type="button" class="link" onClick={() => setAll(!all)}>
          {all ? 'Show fewer' : `Show all ${list.length}`}
        </button>
      )}
      <p class="caption">
        {mode === 'rest'
          ? "A meter's always-on level is the power it hardly ever goes below, from its recorded minutes. The energy and cost are what that level adds up to over the period, at the meter's own average price."
          : "A meter's energy is split at its always-on level: what that level adds up to is its rest (the other view), the remainder is what it used while working."}{' '}
        Only appliances are ranked, never the feeds and panels that hold them, so nothing is counted twice. The percent is of the house's energy.
      </p>
    </section>
  );
}
