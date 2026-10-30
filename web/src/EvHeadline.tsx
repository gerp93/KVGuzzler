// The four answers the EV card exists to give, as tiles in the dashboard's own tile style: what driving this car saved against a gas car, what a mile costs
// (beside the gas car's), how efficiently it uses the energy, and how much of the wall's energy reaches the battery. Each says what it is waiting for rather
// than showing nothing. The sums are the server's.
import type { ComponentChildren } from 'preact';
import { formatDistance, formatEfficiency, mpge, type DistanceUnit, type EvChargesAnswer, type EvSavings, type MeterEv } from './ev';
import { formatMoney, formatPerMile } from './lib';

function Tile({ tone, label, figure, unit, children }: { tone: string; label: string; figure: string; unit?: string; children?: ComponentChildren }) {
  return (
    <div class={`kpi ev-tile ev-tile-${tone}`}>
      <div class="kpi-head">
        <span class="kpi-label">{label}</span>
      </div>
      <p class="kpi-value">
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

export function EvHeadline({
  ev,
  savings,
  charges,
  unit,
  onOpenSettings,
}: {
  ev: MeterEv;
  savings: EvSavings | undefined;
  charges: EvChargesAnswer | undefined;
  unit: DistanceUnit;
  onOpenSettings: () => void;
}) {
  const totals = ev.totals;
  const gas = savings?.totals ?? null;
  const losses = charges?.losses ?? null;
  const saved = gas ? Number(gas.savings) : null;
  const open = (text: string) => (
    <button type="button" class="small" onClick={onOpenSettings}>
      {text}
    </button>
  );

  const [perMile, perMileUnit] = totals?.cost_per_mile != null ? splitAt(formatPerMile(totals.cost_per_mile, ev.cost_estimated, unit), '/') : ['—', undefined];
  const [eff, effUnit] = totals?.miles_per_kwh != null ? splitAt(formatEfficiency(totals.miles_per_kwh, unit), ' ') : ['—', undefined];

  return (
    <div class="ev-tiles">
      <Tile tone="saved" label={saved != null && saved < 0 ? 'Extra cost vs gas' : 'Saved vs gas'} figure={saved != null ? formatMoney(String(Math.abs(saved))) : '—'}>
        {gas ? (
          <>
            <p class="kpi-hint">
              over {formatDistance(gas.miles, unit)} against a {savings?.mpg} mpg gas car
            </p>
            <p class="kpi-hint">
              gas {formatMoney(gas.gas_cost)} · charging {formatMoney(gas.ev_cost)}
            </p>
          </>
        ) : savings?.configured ? (
          <p class="kpi-hint">needs two odometer readings that the gas prices cover</p>
        ) : (
          <p class="kpi-hint">{open('Compare with a gas car')}</p>
        )}
      </Tile>

      <Tile tone="mile" label="Cost per mile" figure={perMile} unit={perMileUnit}>
        <p class="kpi-hint">
          {gas?.gas_cost_per_mile != null
            ? `gas car ${formatPerMile(gas.gas_cost_per_mile, false, unit)}`
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
