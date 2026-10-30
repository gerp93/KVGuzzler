import { describe, expect, it } from 'vitest';
import type { EvChargesAnswer, EvInterval, EvSavings, MeterEv } from './ev';
import { domainOf, firstReady, tabReady, trendTabs } from './evTabs';

const interval = (from: string, to: string, miles: number, kwh: number, cost: string): EvInterval => ({
  from_day: from,
  to_day: to,
  miles,
  kwh,
  cost,
  cost_per_mile: null,
  miles_per_kwh: null,
  coverage: 1,
  cost_estimated: false,
});

const days = (n: number): EvInterval[] =>
  Array.from({ length: n }, (_, i) => {
    const d = (k: number) => `2026-10-${String(k).padStart(2, '0')}`;
    return interval(d(i + 1), d(i + 2), 30, 10, '1.50');
  });

const ev = (intervals: EvInterval[]): MeterEv => ({ meter_id: 'm', readings: [], intervals, totals: null, since_last: null, cost_estimated: false });

/** Two stretches of 100 miles that each cost 2 dollars to charge, against gas cars of 20 and 40 miles per gallon at 4 dollars a gallon and a diesel car of 25 at 5. */
const gasCars = (savings20: string, savings40: string, dieselSavings: string) => [
  { fuel: 'gas' as const, mpg: 20, gallons: 5, gas_cost: '20.00', savings: savings20 },
  { fuel: 'gas' as const, mpg: 40, gallons: 2.5, gas_cost: '10.00', savings: savings40 },
  { fuel: 'diesel' as const, mpg: 25, gallons: 4, gas_cost: '20.00', savings: dieselSavings },
];
const stretches: EvSavings['stretches'] = [
  { from_day: '2026-10-01', to_day: '2026-10-05', miles: 100, gas_price: '4.00', diesel_price: '5.00', ev_cost: '2.00', gas: gasCars('18.00', '8.00', '18.00') },
  { from_day: '2026-10-05', to_day: '2026-10-09', miles: 100, gas_price: '4.00', diesel_price: '5.00', ev_cost: '2.00', gas: gasCars('18.00', '8.00', '18.00') },
];

const savings = (over: Partial<EvSavings> = {}): EvSavings => ({
  meter_id: 'm',
  configured: true,
  area_name: 'Minnesota',
  diesel_area_name: 'Midwest (PADD 2)',
  comparisons: [
    { fuel: 'gas', mpg: 20, custom: false },
    { fuel: 'gas', mpg: 40, custom: false },
    { fuel: 'diesel', mpg: 25, custom: false },
  ],
  prices: [
    { week: '2026-10-05', dollars: '4.00' },
    { week: '2026-10-12', dollars: '4.10' },
  ],
  diesel_prices: [
    { week: '2026-10-05', dollars: '4.80' },
    { week: '2026-10-12', dollars: '4.90' },
  ],
  stretches,
  totals: null,
  cost_estimated: false,
  ...over,
});

const charge = (id: number, endedAt: string, kwhPerPct: number | null, efficiency: number | null): EvChargesAnswer['charges'][number] => ({
  id,
  started_at: endedAt,
  ended_at: endedAt,
  active: false,
  duration_seconds: 3600,
  kwh: 10,
  cost: '1.50',
  cost_estimated: false,
  peak_watts: 7000,
  coverage: 1,
  start_pct: 40,
  start_pct_at: endedAt,
  end_pct: 60,
  end_pct_at: endedAt,
  gain_pct: 20,
  pct_status: kwhPerPct == null ? 'small_gain' : 'ok',
  kwh_per_pct: kwhPerPct,
  battery_kwh: null,
  loss_kwh: null,
  efficiency,
  loss_cost: null,
});

const chargesAnswer = (list: EvChargesAnswer['charges'], battery: number | null = null): EvChargesAnswer => ({ meter_id: 'm', car_linked: true, charges: list, consistency: null, battery_kwh: battery, losses: null, cost_estimated: false });

const tabsOf = (over: Parameters<typeof trendTabs>[0]) => trendTabs(over);
const base = { ev: ev([]), charges: undefined, unit: 'miles' as const };

describe('trendTabs', () => {
  it('offers the efficiency, cost and per-charge tabs without the gas comparison, and the fuel ones once it is set up', () => {
    const ids = (t: ReturnType<typeof trendTabs>) => t.map((x) => x.id);
    expect(ids(tabsOf({ ...base, savings: undefined }))).toEqual(['efficiency', 'cost', 'charge']);
    expect(ids(tabsOf({ ...base, savings: savings({ configured: false }) }))).toEqual(['efficiency', 'cost', 'charge']);
    expect(ids(tabsOf({ ...base, savings: savings() }))).toEqual(['versus', 'permile', 'saved', 'efficiency', 'cost', 'charge', 'gas']);
  });

  it('draws a line for each gas car beside charging, each added up from zero, and no diesel line until it is ticked', () => {
    const versus = tabsOf({ ...base, savings: savings() }).find((t) => t.id === 'versus')!;
    expect(versus.lines.map((l) => l.name)).toEqual(['20 mpg', '40 mpg', 'Charging']);
    expect(versus.lines[0]!.y).toEqual([0, 20, 40]);
    expect(versus.lines[1]!.y).toEqual([0, 10, 20]);
    expect(versus.lines[2]!.y).toEqual([0, 2, 4]);
    expect(versus.x.map((s) => new Date(s * 1000).toISOString().slice(0, 10))).toEqual(['2026-10-01', '2026-10-05', '2026-10-09']);
    expect(versus.endLabels).toBe(true);
    expect(versus.picks).toBe('cars');
    expect(versus.lines.every((l) => l.fill === false)).toBe(true);
    expect(tabReady(versus)).toBe(true);
  });

  it('draws a diesel car dashed and named as diesel once it is ticked', () => {
    const versus = tabsOf({ ...base, savings: savings(), picks: { off: [], on: ['diesel:25'] } }).find((t) => t.id === 'versus')!;
    expect(versus.lines.map((l) => l.name)).toEqual(['20 mpg', '40 mpg', '25 mpg diesel', 'Charging']);
    const diesel = versus.lines[2]!;
    expect(diesel.dashed).toBe(true);
    expect(diesel.y).toEqual([0, 20, 40]);
    expect(versus.lines[0]!.dashed).toBe(false);
  });

  it('makes the charging line the heavy one, and the cars of a fuel one colour at falling strength', () => {
    const lines = tabsOf({ ...base, savings: savings() }).find((t) => t.id === 'versus')!.lines;
    expect(lines[2]!.width).toBe(3);
    expect(lines[0]!.colorVar).toBe(lines[1]!.colorVar);
    expect(lines[0]!.alpha!).toBeGreaterThan(lines[1]!.alpha!);
  });

  it('leaves out the gas cars switched off, and waits for one to be ticked when none is left', () => {
    const one = tabsOf({ ...base, savings: savings(), picks: { off: ['gas:20'], on: [] } }).find((t) => t.id === 'versus')!;
    expect(one.lines.map((l) => l.name)).toEqual(['40 mpg', 'Charging']);
    const none = tabsOf({ ...base, savings: savings(), picks: { off: ['gas:20', 'gas:40'], on: [] } });
    for (const id of ['versus', 'saved'] as const) {
      const tab = none.find((t) => t.id === id)!;
      expect(tabReady(tab)).toBe(false);
      expect(tab.waiting).toContain('Tick a car');
    }
  });

  it('draws what was saved against each car, which can be below zero', () => {
    const saved = tabsOf({ ...base, savings: savings(), picks: { off: [], on: ['diesel:25'] } }).find((t) => t.id === 'saved')!;
    expect(saved.lines.map((l) => l.y)).toEqual([
      [0, 18, 36],
      [0, 8, 16],
      [0, 18, 36],
    ]);
    const below = tabsOf({
      ...base,
      savings: savings({ stretches: [{ ...stretches[0]!, gas: gasCars('-3.00', '-5.00', '-1.00') }, stretches[1]!] }),
    }).find((t) => t.id === 'saved')!;
    expect(below.lines[1]!.y[1]).toBe(-5);
  });

  it('draws nothing from a week of readings that is not a week yet, and says how far along it is', () => {
    const [eff, cost] = tabsOf({ ev: ev(days(3)), savings: undefined, charges: undefined, unit: 'miles' });
    expect(tabReady(eff!)).toBe(false);
    expect(tabReady(cost!)).toBe(false);
    expect(eff!.waiting).toContain('3 of 7 days');
  });

  it('draws the efficiency and the cost once the window has filled', () => {
    const [eff, cost] = tabsOf({ ev: ev(days(9)), savings: undefined, charges: undefined, unit: 'miles' });
    expect(eff!.x.length).toBe(3); // the 7th, 8th and 9th days complete a week
    expect(eff!.lines[0]!.y[0]).toBeCloseTo(3, 9);
    expect(cost!.lines[0]!.y[0]).toBeCloseTo(10.5, 9);
  });

  it('shows the efficiency per kilometer with the kilometer unit', () => {
    const [eff] = tabsOf({ ev: ev(days(9)), savings: undefined, charges: undefined, unit: 'km' });
    expect(eff!.lines[0]!.y[0]).toBeCloseTo(3 * 1.609344, 6);
    expect(eff!.title).toContain('kilometers');
  });

  it('plots each settled charge oldest first: its efficiency with the battery size known, else the energy per percent', () => {
    const list = [charge(3, '2026-10-29T03:00:00Z', 1.02, 0.86), charge(2, '2026-10-27T03:00:00Z', null, null), charge(1, '2026-10-25T03:00:00Z', 0.99, 0.89)];
    const without = tabsOf({ ...base, savings: undefined, charges: chargesAnswer(list) }).find((t) => t.id === 'charge')!;
    expect(without.lines[0]!.y).toEqual([0.99, 1.02]);
    expect(without.x[0]).toBeLessThan(without.x[1]!);
    const withSize = tabsOf({ ...base, savings: undefined, charges: chargesAnswer(list, 88) }).find((t) => t.id === 'charge')!;
    expect(withSize.lines[0]!.y).toEqual([89, 86]);
    expect(withSize.format(89.4)).toBe('89 %');
  });
});

describe('the fuel price tab', () => {
  it('draws the gas price, and the diesel price too once it is ticked', () => {
    const off = tabsOf({ ...base, savings: savings() }).find((t) => t.id === 'gas')!;
    expect(off.label).toBe('Fuel price');
    expect(off.lines.map((l) => l.name)).toEqual(['Gas']);
    expect(off.lines[0]!.y).toEqual([4, 4.1]);
    expect(off.picks).toBe('diesel-price');
    const on = tabsOf({ ...base, savings: savings(), picks: { off: [], on: ['diesel:price'] } }).find((t) => t.id === 'gas')!;
    expect(on.lines.map((l) => l.name)).toEqual(['Gas', 'Diesel']);
    expect(on.lines[1]!.y).toEqual([4.8, 4.9]);
    expect(on.lines[1]!.dashed).toBe(true);
    expect(on.title).toBe('Weekly average price per gallon of regular gasoline in Minnesota and No. 2 diesel in Midwest (PADD 2)');
    expect(off.title).toBe('Weekly average price per gallon of regular gasoline in Minnesota');
  });

  it('has no diesel to offer when no diesel prices have been read', () => {
    const tab = tabsOf({ ...base, savings: savings({ diesel_prices: [] }), picks: { off: [], on: ['diesel:price'] } }).find((t) => t.id === 'gas')!;
    expect(tab.picks).toBeNull();
    expect(tab.lines.map((l) => l.name)).toEqual(['Gas']);
  });

  it('leaves a gap where the diesel price has no week of its own', () => {
    const tab = tabsOf({ ...base, savings: savings({ diesel_prices: [{ week: '2026-10-12', dollars: '4.90' }] }), picks: { off: [], on: ['diesel:price'] } }).find((t) => t.id === 'gas')!;
    expect(tab.lines[1]!.y).toEqual([null, 4.9]);
  });
});

describe('the cost per mile tab', () => {
  const totals: EvSavings['totals'] = {
    stretches: 2,
    miles: 200,
    ev_cost: '4.00',
    ev_cost_per_mile: '0.020000',
    gas: [
      { fuel: 'gas', mpg: 20, gallons: 10, gas_cost: '40.00', savings: '36.00', gas_cost_per_mile: '0.200000' },
      { fuel: 'gas', mpg: 40, gallons: 5, gas_cost: '20.00', savings: '16.00', gas_cost_per_mile: '0.100000' },
      { fuel: 'diesel', mpg: 25, gallons: 8, gas_cost: '40.00', savings: '36.00', gas_cost_per_mile: '0.200000' },
    ],
  };
  const tab = (picks?: { off: string[]; on: string[] }, unit: 'miles' | 'km' = 'miles') => tabsOf({ ...base, unit, savings: savings({ totals }), picks }).find((t) => t.id === 'permile')!;

  it('sets charging against each car as the cost of a mile, charging first', () => {
    expect(tab().bars!.map((b) => [b.label, b.value])).toEqual([['Charging', 0.02], ['20 mpg', 0.2], ['40 mpg', 0.1]]);
    expect(tab().bars![0]!.text).toBe('$0.020/mi');
    expect(tabReady(tab())).toBe(true);
    expect(tab().picks).toBe('cars');
  });

  it('adds a diesel car by its name when it is ticked', () => {
    expect(tab({ off: [], on: ['diesel:25'] }).bars!.map((b) => b.label)).toEqual(['Charging', '20 mpg', '40 mpg', '25 mpg diesel']);
  });

  it('follows the cars ticked, and the unit', () => {
    expect(tab({ off: ['gas:20'], on: [] }).bars!.map((b) => b.label)).toEqual(['Charging', '40 mpg']);
    expect(tab({ off: [], on: [] }, 'km').bars![1]!.value).toBeCloseTo(0.2 / 1.609344, 9);
    expect(tab({ off: [], on: [] }, 'km').title).toContain('kilometer');
  });

  it('waits when no car is ticked or there is no stretch to count', () => {
    expect(tabReady(tab({ off: ['gas:20', 'gas:40'], on: [] }))).toBe(false);
    expect(tab({ off: ['gas:20', 'gas:40'], on: [] }).waiting).toContain('Tick a car');
    const none = tabsOf({ ...base, savings: savings() }).find((t) => t.id === 'permile')!;
    expect(tabReady(none)).toBe(false);
    expect(none.waiting).toContain('two odometer readings');
  });
});

describe('the axes of each tab', () => {
  it('say what the numbers are and what each point stands for', () => {
    const tabs = tabsOf({ ...base, savings: savings() });
    const y = (id: string) => tabs.find((t) => t.id === id)!.yLabel;
    const x = (id: string) => tabs.find((t) => t.id === id)!.xLabel;
    expect(y('versus')).toBe('Total cost, dollars');
    expect(y('saved')).toBe('Saved vs gas car, dollars');
    expect(y('permile')).toBe('Cost per mile, dollars');
    expect(y('efficiency')).toBe('Miles per kWh');
    expect(y('cost')).toBe('Charging cost per week, dollars');
    expect(y('gas')).toBe('Price per gallon, dollars');
    expect(y('charge')).toBe('kWh per percent gained');
    expect(x('versus')).toBe('Date of odometer reading');
    expect(x('efficiency')).toBe('Week ending');
    expect(x('charge')).toBe('Date the charge ended');
    expect(x('gas')).toBe('Week of');
    for (const t of tabs.filter((t) => !t.bars)) expect(t.xLabel, t.id).not.toBe('');
  });

  it('follow the unit and the battery size', () => {
    const tabs = tabsOf({ ev: ev([]), savings: savings(), charges: chargesAnswer([], 88), unit: 'km' });
    expect(tabs.find((t) => t.id === 'efficiency')!.yLabel).toBe('Kilometers per kWh');
    expect(tabs.find((t) => t.id === 'permile')!.yLabel).toBe('Cost per kilometer, dollars');
    expect(tabs.find((t) => t.id === 'charge')!.yLabel).toBe('Efficiency, percent');
  });
});

describe('firstReady and domainOf', () => {
  it('opens the first tab that has something to draw, else the first', () => {
    expect(firstReady(tabsOf({ ev: ev(days(9)), savings: undefined, charges: undefined, unit: 'miles' }))).toBe('efficiency');
    expect(firstReady(tabsOf({ ...base, savings: savings() }))).toBe('versus');
    expect(firstReady(tabsOf({ ...base, savings: savings({ configured: false }) }))).toBe('efficiency');
    expect(firstReady([])).toBeUndefined();
  });

  it('gives the points a little room either side, at least half a day', () => {
    expect(domainOf([])).toBeNull();
    expect(domainOf([1_000_000])).toEqual([1_000_000 - 43_200, 1_000_000 + 43_200]);
    const [lo, hi] = domainOf([0, 100 * 86_400])!;
    expect(lo).toBeCloseTo(-3 * 86_400, 6);
    expect(hi).toBeCloseTo(103 * 86_400, 6);
  });
});
