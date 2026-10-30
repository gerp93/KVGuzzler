// The charts of the EV card as tabs of one panel: each is one series over time, with the words that say what it is and, while it has too few points to draw,
// what it is waiting for. Pure, so the choice of tabs and their points is tested apart from the drawing.
import { cumulativeSavings, efficiencyTrend, gasPriceSeries, inUnit, TREND_DAYS, type DistanceUnit, type EvChargesAnswer, type EvSavings, type MeterEv } from './ev';
import { formatMoney } from './lib';

export type TabId = 'saved' | 'efficiency' | 'cost' | 'charge' | 'gas';

export interface TrendTab {
  id: TabId;
  /** The tab's name. */
  label: string;
  /** What the chart shows, in a line over it. */
  title: string;
  /** A theme custom property for the line. */
  colorVar: string;
  /** Seconds since the epoch, one per value. */
  x: number[];
  y: number[];
  /** The span the chart covers: a little more than its points. Null while there are none. */
  domain: [number, number] | null;
  /** Writes a value on the vertical axis. One function per tab, so the chart is not rebuilt on every render. */
  format: (v: number) => string;
  /** What the tab is waiting for, said in place of a chart that has too few points. */
  waiting: string;
}

/** A line needs two points. */
export const MIN_POINTS = 2;

export const tabReady = (t: TrendTab): boolean => t.x.length >= MIN_POINTS;

const DAY_S = 86_400;

/** The span of some points with a little room either side (half a day at least). */
export function domainOf(x: number[]): [number, number] | null {
  if (x.length === 0) return null;
  const lo = Math.min(...x);
  const hi = Math.max(...x);
  const pad = Math.max(DAY_S / 2, (hi - lo) * 0.03);
  return [lo - pad, hi + pad];
}

const axisNumber = (v: number): string => v.toFixed(1);
const axisPerPct = (v: number): string => v.toFixed(2);
const axisPercent = (v: number): string => `${v.toFixed(0)} %`;
const axisDollars = (v: number): string => formatMoney(String(v)).replace(/\.00$/, '');
const axisCents = (v: number): string => `$${v.toFixed(2)}`;
const axisWeekCost = (v: number): string => `$${v.toFixed(v < 10 ? 2 : 0)}`;

const seconds = (iso: string): number => Date.parse(iso) / 1000;

/** The tabs this meter's vehicle can show, in the order they are offered: the ones that need the gas comparison only when it is set up. */
export function trendTabs(input: { ev: MeterEv; savings: EvSavings | undefined; charges: EvChargesAnswer | undefined; unit: DistanceUnit }): TrendTab[] {
  const { ev, savings, charges, unit } = input;
  const tabs: TrendTab[] = [];
  const make = (t: Omit<TrendTab, 'domain'>): void => {
    tabs.push({ ...t, domain: domainOf(t.x) });
  };

  if (savings?.configured) {
    const saved = cumulativeSavings(savings.stretches);
    make({
      id: 'saved',
      label: 'Saved',
      title: `Saved against a ${savings.mpg} mpg gas car, added up over the odometer readings`,
      colorVar: '--color-accent-green',
      ...saved,
      format: axisDollars,
      waiting: 'The savings build up reading by reading: two odometer readings that the gas prices cover are needed.',
    });
  }

  const trend = efficiencyTrend(ev.intervals);
  const eff = trend.filter((p) => p.milesPerKwh != null);
  const weekWaiting = `Needs a week of odometer readings (${Math.min(trend.length, TREND_DAYS)} of ${TREND_DAYS} days so far): a few days of a car that charges on other days than it drives say nothing.`;
  make({
    id: 'efficiency',
    label: 'Efficiency',
    title: `Efficiency in ${unit === 'km' ? 'kilometers' : 'miles'} per kWh at the wall, over a rolling week`,
    colorVar: '--color-primary-action',
    x: eff.map((p) => p.x),
    y: eff.map((p) => inUnit(p.milesPerKwh ?? 0, unit)),
    format: axisNumber,
    waiting: weekWaiting,
  });
  const cost = trend.filter((p) => p.windowCost != null);
  make({
    id: 'cost',
    label: 'Charging cost',
    title: 'What charging cost, over a rolling week',
    colorVar: '--color-accent-blue',
    x: cost.map((p) => p.x),
    y: cost.map((p) => p.windowCost ?? 0),
    format: axisWeekCost,
    waiting: weekWaiting,
  });

  // Each settled charge, oldest first: its efficiency from wall to battery when the battery's size is known, else the energy it takes per percent.
  const settled = [...(charges?.charges ?? [])].reverse().filter((c) => (charges?.battery_kwh != null ? c.efficiency != null : c.kwh_per_pct != null));
  const byEfficiency = charges?.battery_kwh != null;
  make({
    id: 'charge',
    label: 'Per charge',
    title: byEfficiency ? 'Charging efficiency of each charge, from the wall to the battery' : 'Energy at the wall for each percent the battery gained, for each charge',
    colorVar: '--color-accent-muted',
    x: settled.map((c) => seconds(c.ended_at ?? c.started_at)),
    y: settled.map((c) => (byEfficiency ? (c.efficiency ?? 0) * 100 : (c.kwh_per_pct ?? 0))),
    format: byEfficiency ? axisPercent : axisPerPct,
    waiting: 'Needs two charges that have the car\'s battery reading at both ends.',
  });

  if (savings?.configured) {
    const price = gasPriceSeries(savings.prices);
    make({
      id: 'gas',
      label: 'Gas price',
      title: `Weekly average price of regular gasoline per gallon${savings.area_name ? `, ${savings.area_name}` : ''}`,
      colorVar: '--color-accent-blue',
      ...price,
      format: axisCents,
      waiting: 'No gas prices yet: they come from EIA with the key saved under Carbon, and are read when the comparison is saved and twice a day.',
    });
  }
  return tabs;
}

/** The tab to open first: the first with enough points to draw, else the first. */
export function firstReady(tabs: TrendTab[]): TabId | undefined {
  return (tabs.find(tabReady) ?? tabs[0])?.id;
}
