// The four answers the EV card exists to give, as tiles in the dashboard's own tile style: what driving this car saved against gas cars, what a mile costs
// (beside the gas cars'), how efficiently it uses the energy, and how much of the wall's energy reaches the battery. Each says what it is waiting for rather
// than showing nothing. The gas cars are the ones the viewer has ticked, so a range stands for them. The sums are the server's.
import type { ComponentChildren } from 'preact';
import { formatDistance, formatEfficiency, mpge, type DistanceUnit, type EvChargesAnswer, type EvSavings, type MeterEv } from './ev';
import { savingsSummary } from './evGas';
import { formatMoney, formatPerMile } from './lib';

function Tile({ tone, label, figure, unit, range, children }: { tone: string; label: string; figure: string; unit?: string; range?: boolean; children?: ComponentChildren }) {
  return (
    <div class={`kpi ev-tile ev-tile-${tone}`}>
      <div class="kpi-head">
        <span class="kpi-label">{label}</span>
      </div>
      <p class={`kpi-value${range ? ' range' : ''}`}>
        <span class="figure">{figure}</span>
        {unit && <span class="unit">{unit}</span>}
      </p>
      {children}
    </div>
  );
}

/** "$0.095/mi" as its figure and its unit. */
const splitAt = (text: string, sep: string): [string, string | undefined] => {
  const i = text.indexOf(sep);
  return i < 0 ? [text, undefined] : [text.slice(0, i), text.slice(i + (sep === ' ' ? 1 : 0))];
};

const money = (n: number): string => formatMoney(String(n));

export function EvHeadline({
  ev,
  savings,
  charges,
  unit,
  visible,
  onOpenSettings,
}: {
  ev: MeterEv;
  savings: EvSavings | undefined;
  charges: EvChargesAnswer | undefined;
  unit: DistanceUnit;
  /** The miles per gallon of the gas cars the viewer has ticked. */
  visible: number[];
  onOpenSettings: () => void;
}) {
  const totals = ev.totals;
  const sum = savingsSummary(savings?.totals ?? null, visible);
  const losses = charges?.losses ?? null;
  const open = (text: string) => (
    <button type="button" class="small" onClick={onOpenSettings}>
      {text}
    </button>
  );

  // Savings across the gas cars drawn: one figure for one car, the least to the most for several. Where even the thriftiest car cost less than the electric
  // one, the figures are what it cost extra, smallest first.
  const extra = sum != null && sum.highest < 0;
  const lo = sum ? (extra ? -sum.highest : sum.lowest) : 0;
  const hi = sum ? (extra ? -sum.lowest : sum.highest) : 0;
  const isRange = sum != null && Math.abs(hi - lo) >= 0.005;
  const savedFigure = !sum ? '—' : isRange ? `${money(lo)} – ${money(hi)}` : money(lo);
  const cars = sum ? (sum.count === 1 ? `a ${sum.mpgLow} mpg gas car` : `${sum.count} gas cars, ${sum.mpgLow} to ${sum.mpgHigh} mpg`) : '';

  const [perMile, perMileUnit] = totals?.cost_per_mile != null ? splitAt(formatPerMile(totals.cost_per_mile, ev.cost_estimated, unit), '/') : ['—', undefined];
  const [eff, effUnit] = totals?.miles_per_kwh != null ? splitAt(formatEfficiency(totals.miles_per_kwh, unit), ' ') : ['—', undefined];
  const perMileOf = (n: number): string => formatPerMile(String(n), false, unit);

  return (
    <div class="ev-tiles">
      <Tile tone="saved" label={extra ? 'Extra cost vs gas' : 'Saved vs gas'} figure={savedFigure} range={isRange}>
        {sum ? (
          <>
            <p class="kpi-hint">
              over {formatDistance(sum.miles, unit)} against {cars}
            </p>
            <p class="kpi-hint">
              charging {money(sum.evCost)} · gas {sum.gasLow === sum.gasHigh ? money(sum.gasLow) : `${money(sum.gasLow)} to ${money(sum.gasHigh)}`}
            </p>
          </>
        ) : savings?.configured && savings.totals ? (
          <p class="kpi-hint">tick a gas car under the chart to compare with</p>
        ) : savings?.configured ? (
          <p class="kpi-hint">needs two odometer readings that the gas prices cover</p>
        ) : (
          <p class="kpi-hint">{open('Compare with gas cars')}</p>
        )}
      </Tile>

      <Tile tone="mile" label="Cost per mile" figure={perMile} unit={perMileUnit}>
        <p class="kpi-hint">
          {sum
            ? sum.perMileLow === sum.perMileHigh
              ? `gas car ${perMileOf(sum.perMileLow)}`
              : `gas cars ${perMileOf(sum.perMileLow).replace(/\/(mi|km)$/, '')} to ${perMileOf(sum.perMileHigh)}`
            : totals
              ? `over ${formatDistance(totals.miles, unit)}`
              : 'needs two odometer readings'}
        </p>
      </Tile>

      <Tile tone="efficiency" label="Efficiency" figure={eff} unit={effUnit}>
        <p class="kpi-hint">{totals?.miles_per_kwh != null ? `${mpge(totals.miles_per_kwh).toFixed(0)} MPGe, miles per gallon equivalent` : 'needs two odometer readings'}</p>
      </Tile>

      <Tile tone="loss" label="Charging efficiency" figure={losses ? String(Math.round(losses.efficiency * 100)) : '—'} unit={losses ? '%' : undefined}>
        {losses ? (
          losses.efficiency > 1 ? (
            <p class="kpi-hint bad">more reached the battery than the wall gave: the battery's kWh is set too high</p>
          ) : (
            <p class="kpi-hint">
              {losses.loss_kwh.toFixed(1)} kWh lost{losses.loss_cost != null ? ` · ${formatMoney(losses.loss_cost)}` : ''}
            </p>
          )
        ) : charges && charges.battery_kwh == null && charges.consistency ? (
          <p class="kpi-hint">{open('Set the battery size')}</p>
        ) : (
          <p class="kpi-hint">of the wall's energy reaching the battery, after the first full charge</p>
        )}
      </Tile>
    </div>
  );
}
