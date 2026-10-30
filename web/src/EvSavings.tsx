// What the car's miles would have cost in a gasoline car, set beside what charging cost: the savings (or the loss) of driving this electric car, with the
// gas price it rests on. The gas car's miles per gallon and the price area are the owner's choice, set in the card's settings. The sums are the server's.
import { useMemo } from 'preact/hooks';
import { api } from './api';
import { Chart, type Line } from './Chart';
import { cumulativeSavings, dayLabel, gasPriceSeries, type DistanceUnit, type EvSavings as Savings } from './ev';
import { usePoll } from './hooks';
import { formatMoney, formatPerMile } from './lib';

const DAY_S = 86_400;

export function EvSavings({ meterId, version, unit, onOpenSettings }: { meterId: string; version: number; unit: DistanceUnit; onOpenSettings: () => void }) {
  const { data } = usePoll<Savings>(() => api.evSavings(meterId), 300_000, [meterId, version]);
  const charts = useMemo(() => {
    if (!data?.configured) return null;
    const saved = cumulativeSavings(data.stretches);
    const price = gasPriceSeries(data.prices);
    const xs = [...saved.x, ...price.x];
    if (xs.length === 0) return null;
    const lines = {
      saved: [{ name: 'Saved so far', colorVar: '--color-accent-green', x: saved.x, y: saved.y }] as Line[],
      price: [{ name: 'Gas price', colorVar: '--color-accent-blue', x: price.x, y: price.y }] as Line[],
    };
    return { saved: saved.x.length, price: price.x.length, lines, domain: [Math.min(...xs) - DAY_S, Math.max(...xs) + DAY_S] as [number, number] };
  }, [data]);
  if (!data) return null;

  if (!data.configured) {
    return (
      <div class="ev-savings">
        <h3 class="ev-sub">Against a gas car</h3>
        <p class="caption">
          Set a gas car's miles per gallon and a gas price area to see what the same miles would have cost in gasoline.{' '}
          <button type="button" class="small" onClick={onOpenSettings}>
            Open settings
          </button>
        </p>
      </div>
    );
  }

  const t = data.totals;
  const latest = data.prices[data.prices.length - 1];
  return (
    <div class="ev-savings">
      <h3 class="ev-sub">
        Against a {data.mpg} mpg gas car{data.area_name ? `, ${data.area_name} prices` : ''}
      </h3>
      {t ? (
        <div class="ev-figures">
          <div class="stat">
            <span class="caption">{Number(t.savings) < 0 ? 'Extra cost' : 'Saved'}</span>
            <p class="big small ev-figure">{formatMoney(String(Math.abs(Number(t.savings))))}</p>
            <span class="caption">over {t.miles.toFixed(0)} miles</span>
          </div>
          <div class="stat">
            <span class="caption">Gas car</span>
            <p class="big small ev-figure">{formatMoney(t.gas_cost)}</p>
            <span class="caption">{t.gallons.toFixed(1)} gallons</span>
          </div>
          <div class="stat">
            <span class="caption">Charging</span>
            <p class="big small ev-figure">{formatMoney(t.ev_cost)}</p>
            <span class="caption">energy through the meter</span>
          </div>
          <div class="stat">
            <span class="caption">Per {unit === 'km' ? 'kilometer' : 'mile'}</span>
            <p class="big small ev-figure">{formatPerMile(t.ev_cost_per_mile, false, unit).replace(/\/(mi|km)$/, '')}</p>
            <span class="caption">gas car: {formatPerMile(t.gas_cost_per_mile, false, unit)}</span>
          </div>
        </div>
      ) : (
        <p class="caption">
          {data.prices.length === 0
            ? 'No gas prices yet: they come from EIA with the key saved under Carbon, and are read when the comparison is saved and twice a day.'
            : 'The savings appear once there is a stretch between two odometer readings that the gas prices cover.'}
        </p>
      )}
      {charts && (
        <div class="ev-trends">
          {charts.saved >= 2 && (
            <div>
              <h3 class="ev-sub">Saved so far</h3>
              <Chart lines={charts.lines.saved} domain={charts.domain} height={220} format={(v) => formatMoney(String(v)).replace(/\.00$/, '')} label="Savings against the gas car, added up over the odometer readings" />
            </div>
          )}
          {charts.price >= 2 && (
            <div>
              <h3 class="ev-sub">Gas price, per gallon</h3>
              <Chart lines={charts.lines.price} domain={charts.domain} height={220} format={(v) => `$${v.toFixed(2)}`} label="Weekly average price of regular gasoline" />
            </div>
          )}
        </div>
      )}
      <p class="caption ev-trend-note">
        {latest ? `Gas prices are EIA's weekly average for regular gasoline${data.area_name ? ` in ${data.area_name}` : ''} (${formatPrice(latest.dollars)} the week of ${dayLabel(latest.week)}). ` : ''}
        Only the energy through this meter counts as the electric car's cost, so charging elsewhere makes the savings look bigger; fixed electric fees and the gas car's upkeep are left out.
      </p>
    </div>
  );
}

const formatPrice = (dollars: string): string => `$${Number(dollars).toFixed(2)}/gal`;
