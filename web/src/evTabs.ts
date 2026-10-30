// The charts of the EV card as tabs of one panel: each is one or more series over time, with the words that say what it is and, while it has too few points to
// draw, what it is waiting for. Pure, so the choice of tabs and their points is tested apart from the drawing.
import { efficiencyTrend, gasPriceSeries, inUnit, KM_PER_MILE, TREND_DAYS, type DistanceUnit, type EvChargesAnswer, type EvSavings, type MeterEv } from './ev';
import { carName, compKey, cumulativeCosts, DIESEL_PRICE, gasLineStyle, visibleComparisons, type Picks } from './evGas';
import { formatMoney, formatPerMile } from './lib';

export type TabId = 'versus' | 'permile' | 'saved' | 'efficiency' | 'cost' | 'charge' | 'gas';

/** One line of a tab: its points are the tab's `x`, one value each. */
export interface TabLine {
  name: string;
  /** A theme custom property for the line. */
  colorVar: string;
  /** One value for each of the tab's points; null where the line has none. */
  y: (number | null)[];
  /** The colour at this strength (0 to 1). */
  alpha?: number;
  width?: number;
  dashed?: boolean;
  /** No fill under the line, which only the first line of a chart has by default. */
  fill?: boolean;
}

/** One bar of a tab that compares figures instead of drawing a line over time. */
export interface TabBar {
  label: string;
  /** The figure the bar's length stands for. */
  value: number;
  /** A theme custom property for the bar. */
  colorVar: string;
  alpha?: number;
  /** The figure in words, at the bar's end. */
  text: string;
}

export interface TrendTab {
  id: TabId;
  /** The tab's name. */
  label: string;
  /** What the chart shows, in a line over it. */
  title: string;
  /** Seconds since the epoch, one per value of every line. */
  x: number[];
  lines: TabLine[];
  /** The span the chart covers: a little more than its points. Null while there are none. */
  domain: [number, number] | null;
  /** Bars in place of a chart, for a tab that sets figures side by side (a cost per mile): the tab is ready with two of them. */
  bars?: TabBar[];
  /** Each line is named at its right end instead of in a key (for lines read against each other). */
  endLabels: boolean;
  /** What can be ticked under this chart: the cars to compare with, or the diesel line of the price chart. */
  picks: 'cars' | 'diesel-price' | null;
  /** Writes a value on the vertical axis. One function per tab, so the chart is not rebuilt on every render. */
  format: (v: number) => string;
  /** What the vertical axis measures, written up its side. */
  yLabel: string;
  /** What each point along the horizontal axis stands for, written under it. */
  xLabel: string;
  /** What the tab is waiting for, said in place of a chart that has too few points. */
  waiting: string;
}

/** A line needs two points. */
export const MIN_POINTS = 2;

export const tabReady = (t: TrendTab): boolean => (t.bars ? t.bars.length >= MIN_POINTS : t.x.length >= MIN_POINTS && t.lines.length > 0);

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

/**
 * The tabs this meter's vehicle can show, in the order they are offered: the ones that need the gas comparison only when it is set up. `hidden` are the gas cars
 * the viewer has switched off; the two tabs that draw a line for each gas car draw the others.
 */
export function trendTabs(input: { ev: MeterEv; savings: EvSavings | undefined; charges: EvChargesAnswer | undefined; unit: DistanceUnit; picks?: Picks }): TrendTab[] {
  const { ev, savings, charges, unit } = input;
  const tabs: TrendTab[] = [];
  const make = (t: Omit<TrendTab, 'domain' | 'endLabels' | 'picks'> & { endLabels?: boolean; picks?: TrendTab['picks'] }): void => {
    tabs.push({ endLabels: false, picks: null, ...t, domain: domainOf(t.x) });
  };

  if (savings?.configured && savings.comparisons.length > 0) {
    const costs = cumulativeCosts(savings);
    const picks = input.picks ?? { off: [], on: [] };
    const shown = visibleComparisons(savings.comparisons, picks);
    const none = shown.length === 0;
    const gasLines = (series: Map<string, number[]>): TabLine[] => shown.map((c) => ({ name: carName(c), ...gasLineStyle(c, savings.comparisons), y: series.get(compKey(c)) ?? [], fill: false }));
    const waiting = none ? 'Tick a car under the chart to compare with.' : 'Needs two odometer readings that the gas prices cover.';
    make({
      id: 'versus',
      label: 'Gas vs electric',
      title: 'What each car would have cost against what charging cost, added up: the gap between a car and the charging line is the savings',
      x: none ? [] : costs.x,
      lines: none ? [] : [...gasLines(costs.gas), { name: 'Charging', colorVar: '--color-accent-green', y: costs.ev, width: 3, fill: false }],
      endLabels: true,
      picks: 'cars',
      format: axisDollars,
      yLabel: 'Total cost, dollars',
      xLabel: 'Date of odometer reading',
      waiting,
    });
    // What a mile costs: charging against each gas car, which needs no history to be worth looking at.
    const mile = unit === 'km' ? 'kilometer' : 'mile';
    const perUnit = (perMile: number): number => (unit === 'km' ? perMile / KM_PER_MILE : perMile);
    const totals = savings.totals;
    const bars: TabBar[] = [];
    if (!none && totals?.ev_cost_per_mile != null) {
      bars.push({ label: 'Charging', value: perUnit(Number(totals.ev_cost_per_mile)), colorVar: '--color-accent-green', text: formatPerMile(totals.ev_cost_per_mile, false, unit) });
      for (const c of shown) {
        const g = totals.gas.find((e) => e.fuel === c.fuel && e.mpg === c.mpg);
        if (g?.gas_cost_per_mile == null) continue;
        const { colorVar, alpha } = gasLineStyle(c, savings.comparisons);
        bars.push({ label: carName(c), value: perUnit(Number(g.gas_cost_per_mile)), colorVar, alpha, text: formatPerMile(g.gas_cost_per_mile, false, unit) });
      }
    }
    make({
      id: 'permile',
      label: 'Cost per mile',
      title: `What a ${mile} costs: charging against each car`,
      x: [],
      lines: [],
      bars,
      picks: 'cars',
      format: axisCents,
      yLabel: `Cost per ${mile}, dollars`,
      xLabel: '',
      waiting,
    });
    make({
      id: 'saved',
      label: 'Saved',
      title: 'Saved against each car, added up over the odometer readings (below zero where the electric car cost more)',
      x: none ? [] : costs.x,
      lines: none ? [] : gasLines(costs.saved),
      endLabels: true,
      picks: 'cars',
      format: axisDollars,
      yLabel: 'Saved vs gas car, dollars',
      xLabel: 'Date of odometer reading',
      waiting,
    });
  }

  const trend = efficiencyTrend(ev.intervals);
  const eff = trend.filter((p) => p.milesPerKwh != null);
  const weekWaiting = `Needs a week of odometer readings (${Math.min(trend.length, TREND_DAYS)} of ${TREND_DAYS} days so far): a few days of a car that charges on other days than it drives say nothing.`;
  make({
    id: 'efficiency',
    label: 'Efficiency',
    title: `Efficiency in ${unit === 'km' ? 'kilometers' : 'miles'} per kWh at the wall, over a rolling week`,
    x: eff.map((p) => p.x),
    lines: [{ name: 'Efficiency', colorVar: '--color-primary-action', y: eff.map((p) => inUnit(p.milesPerKwh ?? 0, unit)) }],
    format: axisNumber,
    yLabel: `${unit === 'km' ? 'Kilometers' : 'Miles'} per kWh`,
    xLabel: 'Week ending',
    waiting: weekWaiting,
  });
  const cost = trend.filter((p) => p.windowCost != null);
  make({
    id: 'cost',
    label: 'Charging cost',
    title: 'What charging cost, over a rolling week',
    x: cost.map((p) => p.x),
    lines: [{ name: 'Charging cost', colorVar: '--color-accent-blue', y: cost.map((p) => p.windowCost ?? 0) }],
    format: axisWeekCost,
    yLabel: 'Charging cost per week, dollars',
    xLabel: 'Week ending',
    waiting: weekWaiting,
  });

  // Each settled charge, oldest first: its efficiency from wall to battery when the battery's size is known, else the energy it takes per percent.
  const byEfficiency = charges?.battery_kwh != null;
  const settled = [...(charges?.charges ?? [])].reverse().filter((c) => (byEfficiency ? c.efficiency != null : c.kwh_per_pct != null));
  make({
    id: 'charge',
    label: 'Per charge',
    title: byEfficiency ? 'Charging efficiency of each charge, from the wall to the battery' : 'Energy at the wall for each percent the battery gained, for each charge',
    x: settled.map((c) => seconds(c.ended_at ?? c.started_at)),
    lines: [{ name: 'Per charge', colorVar: '--color-accent-muted', y: settled.map((c) => (byEfficiency ? (c.efficiency ?? 0) * 100 : (c.kwh_per_pct ?? 0))) }],
    format: byEfficiency ? axisPercent : axisPerPct,
    yLabel: byEfficiency ? 'Efficiency, percent' : 'kWh per percent gained',
    xLabel: 'Date the charge ended',
    waiting: "Needs two charges that have the car's battery reading at both ends.",
  });

  if (savings?.configured) {
    const price = gasPriceSeries(savings.prices);
    // The diesel line shares the gas weeks (EIA publishes both on the same days) and is drawn only when the viewer has ticked it.
    const dieselByWeek = new Map(savings.diesel_prices.map((p) => [p.week, Number(p.dollars)]));
    const hasDiesel = dieselByWeek.size > 0;
    const showDiesel = hasDiesel && (input.picks ?? { off: [], on: [] }).on.includes(DIESEL_PRICE);
    const lines: TabLine[] = [{ name: 'Gas', colorVar: '--color-accent-blue', y: price.y }];
    if (showDiesel) lines.push({ name: 'Diesel', colorVar: '--color-accent-blue', dashed: true, y: savings.prices.map((p) => dieselByWeek.get(p.week) ?? null) });
    make({
      id: 'gas',
      label: 'Fuel price',
      title: priceTitle(savings, showDiesel),
      x: price.x,
      lines,
      picks: hasDiesel ? 'diesel-price' : null,
      format: axisCents,
      yLabel: 'Price per gallon, dollars',
      xLabel: 'Week of',
      waiting: 'No gas prices yet: they come from EIA with the key saved under Carbon, and are read when the comparison is saved and twice a day.',
    });
  }
  return tabs;
}

/** What the price chart shows, in words: gasoline for the area and, when ticked, diesel for the area it is read for (its region, if EIA has none for the area). */
function priceTitle(savings: Pick<EvSavings, 'area_name' | 'diesel_area_name'>, showDiesel: boolean): string {
  const where = savings.area_name ? ` in ${savings.area_name}` : '';
  if (!showDiesel) return `Weekly average price per gallon of regular gasoline${where}`;
  const dieselWhere = savings.diesel_area_name && savings.diesel_area_name !== savings.area_name ? ` in ${savings.diesel_area_name}` : where;
  return `Weekly average price per gallon of regular gasoline${where} and No. 2 diesel${dieselWhere}`;
}

/** The tab to open first: the first with enough points to draw, else the first. */
export function firstReady(tabs: TrendTab[]): TabId | undefined {
  return (tabs.find(tabReady) ?? tabs[0])?.id;
}
