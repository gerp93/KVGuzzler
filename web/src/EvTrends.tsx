// Two charts in the EV card: how efficiently the car has used the energy the meter measured, and what charging has cost, each over a rolling week. The sums
// are the odometer stretches' (see `efficiencyTrend`): the meter sees only the energy that went through it, so charging somewhere else is not in them.
import { useMemo } from 'preact/hooks';
import { Chart, type Line } from './Chart';
import { efficiencyTrend, inUnit, TREND_DAYS, unitAbbreviation, type DistanceUnit, type EvInterval } from './ev';

const DAY_S = 86_400;

export function EvTrends({ intervals, unit }: { intervals: EvInterval[]; unit: DistanceUnit }) {
  const trend = useMemo(() => efficiencyTrend(intervals), [intervals]);
  const shown = useMemo(() => {
    const eff = trend.filter((p) => p.milesPerKwh != null);
    const cost = trend.filter((p) => p.windowCost != null);
    const first = trend[0]?.x;
    const last = trend[trend.length - 1]?.x;
    if (first == null || last == null) return null;
    const lines = {
      efficiency: [{ name: 'Efficiency', colorVar: '--color-accent-green', x: eff.map((p) => p.x), y: eff.map((p) => inUnit(p.milesPerKwh ?? 0, unit)) }] as Line[],
      cost: [{ name: 'Charging cost', colorVar: '--color-accent-blue', x: cost.map((p) => p.x), y: cost.map((p) => p.windowCost ?? 0) }] as Line[],
    };
    return { eff: eff.length, cost: cost.length, lines, domain: [first - DAY_S / 2, last + DAY_S / 2] as [number, number] };
  }, [trend, unit]);

  if (!shown) return null;
  // A line needs two points, and a point is a full week: until then the card says what it is waiting for.
  if (shown.eff < 2 && shown.cost < 2) {
    return (
      <p class="caption ev-trend-note">
        The efficiency and cost charts appear once there is a week of odometer readings ({trend.length} of {TREND_DAYS} days so far).
      </p>
    );
  }
  const perKwh = `${unitAbbreviation(unit) === 'km' ? 'kilometers' : 'miles'} per kWh`;
  return (
    <div class="ev-trends">
      {shown.eff >= 2 && (
        <div>
          <h3 class="ev-sub">Efficiency, {perKwh}</h3>
          <Chart lines={shown.lines.efficiency} domain={shown.domain} height={220} format={(v) => v.toFixed(1)} label={`Efficiency in ${perKwh}, over a rolling week`} />
        </div>
      )}
      {shown.cost >= 2 && (
        <div>
          <h3 class="ev-sub">Charging cost, per week</h3>
          <Chart lines={shown.lines.cost} domain={shown.domain} height={220} format={(v) => `$${v.toFixed(v < 10 ? 2 : 0)}`} label="Charging cost over a rolling week" />
        </div>
      )}
      <p class="caption ev-trend-note">Each point is the week ending that day. Only the energy through this meter is counted, so charging elsewhere makes the car look more efficient.</p>
    </div>
  );
}
