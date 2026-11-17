import { describe, expect, it } from 'vitest';
import type { EvCharge, EvChargesAnswer, EvSavings } from './ev';
import { dayPosition, hasEndLevel, perMileSoFar, trueCost, trueSummary, trueTotals } from './evTrue';

/** A moment on this device's own clock, as the server would send it. */
const at = (month: number, day: number, hour = 0): string => new Date(2026, month - 1, day, hour, 0, 0).toISOString();

const charge = (c: { id: number; ended: string; kwh: number; cost: string | null; endPct: number | null }): EvCharge => ({
  id: c.id,
  started_at: c.ended,
  ended_at: c.ended,
  active: false,
  duration_seconds: 3600,
  kwh: c.kwh,
  cost: c.cost,
  cost_estimated: false,
  peak_watts: 7000,
  coverage: 1,
  start_pct: 40,
  start_pct_at: c.ended,
  end_pct: c.endPct,
  end_pct_at: c.ended,
  gain_pct: null,
  pct_status: c.endPct == null ? 'none' : 'ok',
  kwh_per_pct: null,
  battery_kwh: null,
  loss_kwh: null,
  efficiency: null,
  loss_cost: null,
});

const answer = (charges: EvCharge[], kwhPerPct: number | null = 1): EvChargesAnswer => ({
  meter_id: 'm',
  car_linked: true,
  charges,
  consistency: kwhPerPct == null ? null : { charges: 2, kwh_per_pct: kwhPerPct, lowest: kwhPerPct, highest: kwhPerPct },
  battery_kwh: null,
  losses: null,
  cost_estimated: false,
});

const gas = (cost: string) => [{ fuel: 'gas' as const, mpg: 20, gallons: 0, gas_cost: cost, savings: null }];
/** 120 miles over 4 days (30 a day) priced at 24 dollars of gas (6 a day), then 80 miles over 4 days (20 a day) at 16 dollars (4 a day). */
const stretches = (): EvSavings['stretches'] => [
  { from_day: '2026-10-27', to_day: '2026-10-31', miles: 120, gas_price: '4.00', diesel_price: null, ev_cost: null, gas: gas('24.00') },
  { from_day: '2026-10-31', to_day: '2026-11-04', miles: 80, gas_price: '4.00', diesel_price: null, ev_cost: null, gas: gas('16.00') },
];
const savings = (over: Partial<EvSavings> = {}): EvSavings => ({
  meter_id: 'm',
  configured: true,
  area_name: 'Minnesota',
  diesel_area_name: null,
  comparisons: [{ fuel: 'gas', mpg: 20, custom: false }],
  prices: [],
  diesel_prices: [],
  stretches: stretches(),
  totals: null,
  cost_estimated: false,
  ...over,
});

// The first charge ends the evening of 27 Oct, the second early on 1 Nov: 120 miles in between (5 + 3 days of 30 + 20 + 5), and 24 dollars of gas at 20 mpg.
const first = (): EvCharge => charge({ id: 1, ended: at(10, 27, 20), kwh: 20, cost: '3.00', endPct: 80 });

describe('trueCost', () => {
  it('takes the battery’s change out of what was bought, and prices the same miles for the gas car', () => {
    const t = trueCost({ charges: answer([first(), charge({ id: 2, ended: at(11, 1, 6), kwh: 30, cost: '4.50', endPct: 90 })]), savings: savings() });
    expect(t.reason).toBeNull();
    expect(t.cycles.length).toBe(1);
    const c = t.cycles[0]!;
    expect(c.miles).toBeCloseTo(120, 6);
    expect(c.boughtKwh).toBe(30);
    expect(c.usedKwh).toBeCloseTo(20, 9); // 30 from the wall, 10 percent gained at 1 kWh each
    expect(c.usedCost).toBeCloseTo(3, 9); // at the charge's own 15 cents
    expect(c.gas.get('gas:20')).toBeCloseTo(24, 6); // 120 miles at 20 per gallon is 6 gallons at 4 dollars
    expect(t.cumulative.miles).toEqual([0, c.miles]);
    expect(t.cumulative.ev[1]).toBeCloseTo(3, 9);
  });

  it('charges a stretch where the battery fell for the energy it drew', () => {
    // 5 kWh bought and the battery lost 10 percent over the cycle: the miles used 15.
    const t = trueCost({ charges: answer([first(), charge({ id: 2, ended: at(11, 1, 6), kwh: 5, cost: '1.00', endPct: 70 })]), savings: savings() });
    expect(t.cycles[0]!.usedKwh).toBeCloseTo(15, 9);
    expect(t.cycles[0]!.usedCost).toBeCloseTo(3, 9);
  });

  it('folds a charge with no end level into the cycle that follows it', () => {
    const t = trueCost({
      charges: answer([first(), charge({ id: 2, ended: at(10, 30, 6), kwh: 10, cost: '1.50', endPct: null }), charge({ id: 3, ended: at(11, 1, 6), kwh: 20, cost: '3.00', endPct: 80 })]),
      savings: savings(),
    });
    expect(t.cycles.length).toBe(1);
    expect(t.cycles[0]!.boughtKwh).toBe(30);
    expect(t.cycles[0]!.usedCost).toBeCloseTo(4.5, 9); // the battery ended where it began
  });

  it('adds cycles up, one point at each charge', () => {
    const t = trueCost({
      charges: answer([first(), charge({ id: 2, ended: at(11, 1, 6), kwh: 30, cost: '4.50', endPct: 90 }), charge({ id: 3, ended: at(11, 3, 6), kwh: 12, cost: '1.80', endPct: 90 })]),
      savings: savings(),
    });
    expect(t.cycles.length).toBe(2);
    expect(t.cumulative.at.length).toBe(3);
    expect(t.cumulative.miles[2]).toBeCloseTo(120 + 40, 4); // 1 Nov 06:00 to 3 Nov 06:00 is two days of 20 miles
  });

  it('does not count a cycle the odometer does not cover, or a price is missing for', () => {
    // The second charge ended after the last reading's last day.
    const late = trueCost({ charges: answer([first(), charge({ id: 2, ended: at(11, 8, 6), kwh: 30, cost: '4.50', endPct: 90 })]), savings: savings() });
    expect(late.cycles).toEqual([]);
    expect(late.reason).toMatch(/second charge/);
    const noGasPrice = trueCost({ charges: answer([first(), charge({ id: 2, ended: at(11, 1, 6), kwh: 30, cost: '4.50', endPct: 90 })]), savings: savings({ stretches: [{ ...stretches()[0]!, gas: [{ fuel: 'gas', mpg: 20, gallons: 0, gas_cost: null, savings: null }] }, stretches()[1]!] }) });
    expect(noGasPrice.cycles).toEqual([]);
    const noElectricPrice = trueCost({ charges: answer([first(), charge({ id: 2, ended: at(11, 1, 6), kwh: 30, cost: null, endPct: 90 })]), savings: savings() });
    expect(noElectricPrice.cycles).toEqual([]);
  });

  it('drops a cycle whose battery report says the miles used less than nothing', () => {
    // 5 kWh bought but the battery gained 20 percent: a report that cannot be right.
    const t = trueCost({ charges: answer([first(), charge({ id: 2, ended: at(11, 1, 6), kwh: 5, cost: '1.00', endPct: 100 })]), savings: savings() });
    expect(t.cycles).toEqual([]);
  });

  it('says what it is waiting for', () => {
    expect(trueCost({ charges: answer([first()]), savings: savings() }).reason).toMatch(/second charge|end/);
    expect(trueCost({ charges: answer([first()], null), savings: savings() }).reason).toMatch(/percent/);
    expect(trueCost({ charges: answer([]), savings: undefined }).reason).toMatch(/comparison/);
  });

  it('works out each car from the miles alone, so ticking never changes charging', () => {
    const two = savings({ comparisons: [{ fuel: 'gas', mpg: 20, custom: false }, { fuel: 'gas', mpg: 40, custom: false }], stretches: stretches().map((s) => ({ ...s, gas: [...s.gas, { fuel: 'gas' as const, mpg: 40, gallons: 0, gas_cost: String(Number(s.gas[0]!.gas_cost) / 2), savings: null }] })) });
    const t = trueCost({ charges: answer([first(), charge({ id: 2, ended: at(11, 1, 6), kwh: 30, cost: '4.50', endPct: 90 })]), savings: two });
    expect(t.cycles[0]!.gas.get('gas:40')).toBeCloseTo(12, 6);
    expect(t.cycles[0]!.usedCost).toBeCloseTo(3, 9);
  });
});

describe('hasEndLevel', () => {
  it('trusts an end report that came after the end, and not an early or missing one', () => {
    const base = { ended_at: at(11, 1), end_pct: 80 };
    expect(hasEndLevel({ ...base, pct_status: 'ok' })).toBe(true);
    expect(hasEndLevel({ ...base, pct_status: 'small_gain' })).toBe(true);
    expect(hasEndLevel({ ...base, pct_status: 'early' })).toBe(false);
    expect(hasEndLevel({ ...base, pct_status: 'waiting' })).toBe(false);
    expect(hasEndLevel({ ended_at: null, end_pct: 80, pct_status: 'active' })).toBe(false);
    expect(hasEndLevel({ ...base, end_pct: null, pct_status: 'none' })).toBe(false);
  });
});

describe('dayPosition', () => {
  it('is the day plus how far through it', () => {
    expect(dayPosition(at(11, 1, 6)) - dayPosition(at(11, 1, 0))).toBeCloseTo(0.25, 9);
    expect(dayPosition(at(11, 2, 0)) - dayPosition(at(11, 1, 0))).toBe(1);
  });
});

describe('perMileSoFar', () => {
  it('divides each running total by the miles so far, and has nothing before the first mile', () => {
    expect(perMileSoFar([0, 3, 10], [0, 100, 200])).toEqual([null, 0.03, 0.05]);
  });
});

describe('trueTotals and trueSummary', () => {
  const two = (): EvSavings =>
    savings({
      comparisons: [
        { fuel: 'gas', mpg: 20, custom: false },
        { fuel: 'gas', mpg: 40, custom: false },
      ],
      stretches: stretches().map((s) => ({ ...s, gas: [...s.gas, { fuel: 'gas' as const, mpg: 40, gallons: 0, gas_cost: String(Number(s.gas[0]!.gas_cost) / 2), savings: null }] })),
    });
  // 120 miles; 30 kWh bought and 10 percent gained at 1 kWh each: 20 kWh used, 3 dollars at the charge's 15 cents.
  const truth = () => trueCost({ charges: answer([first(), charge({ id: 2, ended: at(11, 1, 6), kwh: 30, cost: '4.50', endPct: 90 })]), savings: two() });

  it('adds the counted cycles up: miles, the energy the miles used, what it cost and what each car would have burned', () => {
    const t = trueTotals(truth())!;
    expect(t.miles).toBeCloseTo(120, 6);
    expect(t.usedKwh).toBeCloseTo(20, 9);
    expect(t.evCost).toBeCloseTo(3, 9);
    expect(t.gas.get('gas:20')).toBeCloseTo(24, 6);
    expect(t.gas.get('gas:40')).toBeCloseTo(12, 6);
  });

  it('has no totals without a counted cycle', () => {
    expect(trueTotals(trueCost({ charges: answer([first()]), savings: two() }))).toBeNull();
  });

  it('gives the headline tile its range over the cars drawn, in the same shape as the stretch summary', () => {
    const s = trueSummary(truth(), two().comparisons, ['gas:20', 'gas:40'])!;
    expect(s.count).toBe(2);
    expect(s.lowest).toBeCloseTo(9, 6); // 12 of gas less 3 of charging
    expect(s.highest).toBeCloseTo(21, 6);
    expect(s.perMileLow).toBeCloseTo(0.1, 6);
    expect(s.perMileHigh).toBeCloseTo(0.2, 6);
    expect(s.gasLow).toBeCloseTo(12, 6);
    expect(s.gasHigh).toBeCloseTo(24, 6);
    expect(s.mpgLow).toBe(20);
    expect(s.mpgHigh).toBe(40);
    expect(s.evCost).toBeCloseTo(3, 9);
    expect(s.miles).toBeCloseTo(120, 6);
  });

  it('follows the cars ticked, and says nothing when none is', () => {
    const one = trueSummary(truth(), two().comparisons, ['gas:40'])!;
    expect(one.count).toBe(1);
    expect(one.lowest).toBeCloseTo(one.highest, 9);
    expect(one.evCost).toBeCloseTo(3, 9); // charging is the same whichever cars are ticked
    expect(trueSummary(truth(), two().comparisons, [])).toBeNull();
  });

  it('can be negative where the electric car cost more', () => {
    const pricey = trueCost({ charges: answer([first(), charge({ id: 2, ended: at(11, 1, 6), kwh: 30, cost: '30.00', endPct: 90 })]), savings: two() });
    expect(trueSummary(pricey, two().comparisons, ['gas:40'])!.highest).toBeLessThan(0);
  });
});
