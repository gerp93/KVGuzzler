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

const savings = (over: Partial<EvSavings> = {}): EvSavings => ({
  meter_id: 'm',
  configured: true,
  mpg: 25,
  area_name: 'Minnesota',
  prices: [
    { week: '2026-10-05', dollars: '4.00' },
    { week: '2026-10-12', dollars: '4.10' },
  ],
  stretches: [
    { from_day: '2026-10-01', to_day: '2026-10-05', miles: 100, gallons: 4, gas_price: '4.00', gas_cost: '16.00', ev_cost: '2.00', savings: '14.00' },
    { from_day: '2026-10-05', to_day: '2026-10-09', miles: 100, gallons: 4, gas_price: '4.00', gas_cost: '16.00', ev_cost: '2.00', savings: '14.00' },
  ],
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

describe('trendTabs', () => {
  it('offers the efficiency, cost and per-charge tabs without the gas comparison, and the other two with it', () => {
    const ids = (t: ReturnType<typeof trendTabs>) => t.map((x) => x.id);
    expect(ids(trendTabs({ ev: ev([]), savings: undefined, charges: undefined, unit: 'miles' }))).toEqual(['efficiency', 'cost', 'charge']);
    expect(ids(trendTabs({ ev: ev([]), savings: savings({ configured: false }), charges: undefined, unit: 'miles' }))).toEqual(['efficiency', 'cost', 'charge']);
    expect(ids(trendTabs({ ev: ev([]), savings: savings(), charges: undefined, unit: 'miles' }))).toEqual(['saved', 'efficiency', 'cost', 'charge', 'gas']);
  });

  it('draws nothing from a week of readings that is not a week yet, and says how far along it is', () => {
    const [eff, cost] = trendTabs({ ev: ev(days(3)), savings: undefined, charges: undefined, unit: 'miles' });
    expect(tabReady(eff!)).toBe(false);
    expect(tabReady(cost!)).toBe(false);
    expect(eff!.waiting).toContain('3 of 7 days');
  });

  it('draws the efficiency and the cost once the window has filled', () => {
    const [eff, cost] = trendTabs({ ev: ev(days(9)), savings: undefined, charges: undefined, unit: 'miles' });
    expect(eff!.x.length).toBe(3); // the 7th, 8th and 9th days complete a week
    expect(eff!.y[0]).toBeCloseTo(3, 9);
    expect(cost!.y[0]).toBeCloseTo(10.5, 9);
  });

  it('shows the efficiency per kilometer with the kilometer unit', () => {
    const [eff] = trendTabs({ ev: ev(days(9)), savings: undefined, charges: undefined, unit: 'km' });
    expect(eff!.y[0]).toBeCloseTo(3 * 1.609344, 6);
    expect(eff!.title).toContain('kilometers');
  });

  it('adds the savings up and takes the gas prices as they are', () => {
    const tabs = trendTabs({ ev: ev([]), savings: savings(), charges: undefined, unit: 'miles' });
    const saved = tabs.find((t) => t.id === 'saved')!;
    const gas = tabs.find((t) => t.id === 'gas')!;
    expect(saved.y).toEqual([14, 28]);
    expect(gas.y).toEqual([4, 4.1]);
    expect(gas.title).toContain('Minnesota');
  });

  it('plots each settled charge oldest first: its efficiency with the battery size known, else the energy per percent', () => {
    const list = [charge(3, '2026-10-29T03:00:00Z', 1.02, 0.86), charge(2, '2026-10-27T03:00:00Z', null, null), charge(1, '2026-10-25T03:00:00Z', 0.99, 0.89)];
    const without = trendTabs({ ev: ev([]), savings: undefined, charges: chargesAnswer(list), unit: 'miles' }).find((t) => t.id === 'charge')!;
    expect(without.y).toEqual([0.99, 1.02]);
    expect(without.x[0]).toBeLessThan(without.x[1]!);
    const withSize = trendTabs({ ev: ev([]), savings: undefined, charges: chargesAnswer(list, 88), unit: 'miles' }).find((t) => t.id === 'charge')!;
    expect(withSize.y).toEqual([89, 86]);
    expect(withSize.format(89.4)).toBe('89 %');
  });
});

describe('firstReady and domainOf', () => {
  it('opens the first tab that has something to draw, else the first', () => {
    const tabs = trendTabs({ ev: ev(days(9)), savings: undefined, charges: undefined, unit: 'miles' });
    expect(firstReady(tabs)).toBe('efficiency');
    const none = trendTabs({ ev: ev([]), savings: savings({ configured: false }), charges: undefined, unit: 'miles' });
    expect(firstReady(none)).toBe('efficiency');
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
