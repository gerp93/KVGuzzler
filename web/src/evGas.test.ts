import { describe, expect, it } from 'vitest';
import type { EvSavings } from './ev';
import { countedStretches, cumulativeCosts, gasLineStyle, parseHidden, savingsSummary, visibleComparisons, type GasComparison } from './evGas';

const comps: GasComparison[] = [
  { mpg: 15, custom: false },
  { mpg: 20, custom: false },
  { mpg: 25, custom: false },
  { mpg: 27.5, custom: true },
  { mpg: 40, custom: false },
];

type Stretch = EvSavings['stretches'][number];

/** A stretch whose gas cost for each car is its miles over the car's miles per gallon at 4 dollars a gallon. */
const stretch = (from: string, to: string, miles: number, ev: string | null, priced = true): Stretch => ({
  from_day: from,
  to_day: to,
  miles,
  gas_price: priced ? '4.00' : null,
  ev_cost: ev,
  gas: comps.map((c) => {
    const cost = (miles / c.mpg) * 4;
    return {
      mpg: c.mpg,
      gallons: miles / c.mpg,
      gas_cost: priced ? cost.toFixed(2) : null,
      savings: priced && ev != null ? (cost - Number(ev)).toFixed(2) : null,
    };
  }),
});

describe('parseHidden', () => {
  it('reads the gas cars switched off', () => {
    expect(parseHidden('[15, 27.5]')).toEqual([15, 27.5]);
    expect(parseHidden('[]')).toEqual([]);
  });
  it('takes anything else saved for none', () => {
    for (const bad of ['', 'nope', '{"a":1}', '5', 'null']) expect(parseHidden(bad)).toEqual([]);
    expect(parseHidden('[15, "x", null, 20]')).toEqual([15, 20]);
  });
});

describe('visibleComparisons', () => {
  it('draws every gas car but the ones switched off, including one just named', () => {
    expect(visibleComparisons(comps, [15, 40]).map((c) => c.mpg)).toEqual([20, 25, 27.5]);
    expect(visibleComparisons(comps, []).length).toBe(5);
    expect(visibleComparisons(comps, [15, 20, 25, 27.5, 40])).toEqual([]);
  });
});

describe('gasLineStyle', () => {
  it('draws the presets as one colour at falling strength, the thirstiest car the strongest', () => {
    const styles = comps.filter((c) => !c.custom).map((c) => gasLineStyle(c, comps));
    expect(styles.every((s) => s.colorVar === '--color-accent-red')).toBe(true);
    expect(styles[0]!.alpha).toBe(1);
    expect(styles[styles.length - 1]!.alpha).toBeCloseTo(0.45, 9);
    for (let i = 1; i < styles.length; i++) expect(styles[i]!.alpha).toBeLessThan(styles[i - 1]!.alpha);
  });

  it('gives the owners own car a colour of its own at full strength', () => {
    expect(gasLineStyle(comps[3]!, comps)).toEqual({ colorVar: '--color-accent-blue', alpha: 1, width: 2 });
  });

  it('keeps a car looking the same whichever others are switched off', () => {
    // The look depends on the cars compared with, not on the ones drawn, so ticking a box never recolours the others.
    expect(gasLineStyle(comps[2]!, comps)).toEqual(gasLineStyle(comps[2]!, [...comps]));
  });
});

describe('countedStretches', () => {
  it('counts a stretch that has the gas price and the electric cost', () => {
    const list = [stretch('2026-10-01', '2026-10-05', 100, '2.00'), stretch('2026-10-05', '2026-10-09', 100, null), stretch('2026-10-09', '2026-10-12', 100, '2.00', false)];
    expect(countedStretches(list).map((s) => s.from_day)).toEqual(['2026-10-01']);
  });
});

describe('cumulativeCosts', () => {
  it('adds each gas car, charging and the savings up over the counted stretches, from zero on the first day', () => {
    const c = cumulativeCosts({ comparisons: comps, stretches: [stretch('2026-10-01', '2026-10-05', 100, '2.00'), stretch('2026-10-05', '2026-10-09', 100, '2.00')] });
    expect(c.x.map((s) => new Date(s * 1000).toISOString().slice(0, 10))).toEqual(['2026-10-01', '2026-10-05', '2026-10-09']);
    expect(c.ev).toEqual([0, 2, 4]);
    expect(c.gas.get(20)).toEqual([0, 20, 40]); // 100 miles at 20 per gallon is 5 gallons at 4 dollars
    expect(c.gas.get(40)).toEqual([0, 10, 20]);
    expect(c.saved.get(20)).toEqual([0, 18, 36]);
    // The gap between a gas line and charging is the savings.
    for (const mpg of [15, 20, 25, 27.5, 40]) {
      const gas = c.gas.get(mpg)!;
      const saved = c.saved.get(mpg)!;
      gas.forEach((g, i) => expect(g - c.ev[i]!).toBeCloseTo(saved[i]!, 2));
    }
  });

  it('counts only the stretches that have both costs, so every line covers the same days', () => {
    const c = cumulativeCosts({
      comparisons: comps,
      stretches: [stretch('2026-10-01', '2026-10-05', 100, null), stretch('2026-10-05', '2026-10-09', 100, '2.00'), stretch('2026-10-09', '2026-10-12', 100, '2.00', false)],
    });
    expect(c.ev).toEqual([0, 2]);
    expect(c.gas.get(20)).toEqual([0, 20]);
    expect(new Date(c.x[0]! * 1000).toISOString().slice(0, 10)).toBe('2026-10-05');
  });

  it('can go below zero when a gas car cost less than charging', () => {
    const c = cumulativeCosts({ comparisons: comps, stretches: [stretch('2026-10-01', '2026-10-05', 100, '12.00')] });
    expect(c.saved.get(40)).toEqual([0, -2]); // 100 miles at 40 per gallon is 10 dollars of gas against 12 of charging
  });

  it('has nothing without a stretch to count', () => {
    const c = cumulativeCosts({ comparisons: comps, stretches: [] });
    expect(c.x).toEqual([]);
    expect(c.gas.size).toBe(0);
  });
});

describe('savingsSummary', () => {
  const totals: NonNullable<EvSavings['totals']> = {
    stretches: 2,
    miles: 190,
    ev_cost: '3.44',
    ev_cost_per_mile: '0.018105',
    gas: [
      { mpg: 15, gallons: 12.67, gas_cost: '55.00', savings: '51.56', gas_cost_per_mile: '0.289474' },
      { mpg: 25, gallons: 7.6, gas_cost: '33.00', savings: '29.56', gas_cost_per_mile: '0.173684' },
      { mpg: 40, gallons: 4.75, gas_cost: '20.625', savings: '17.185', gas_cost_per_mile: '0.108553' },
    ],
  };

  it('gives the least and the most saved over the gas cars drawn', () => {
    const s = savingsSummary(totals, [15, 25, 40])!;
    expect(s.count).toBe(3);
    expect(s.lowest).toBeCloseTo(17.185, 9);
    expect(s.highest).toBeCloseTo(51.56, 9);
    expect(s.mpgLow).toBe(15);
    expect(s.mpgHigh).toBe(40);
    expect(s.gasLow).toBeCloseTo(20.625, 9);
    expect(s.gasHigh).toBeCloseTo(55, 9);
    expect(s.perMileLow).toBeCloseTo(0.108553, 9);
    expect(s.evCost).toBeCloseTo(3.44, 9);
    expect(s.miles).toBe(190);
  });

  it('follows the gas cars ticked, down to one', () => {
    const one = savingsSummary(totals, [25])!;
    expect(one.count).toBe(1);
    expect(one.lowest).toBe(one.highest);
    expect(savingsSummary(totals, [15, 40])!.lowest).toBeCloseTo(17.185, 9);
  });

  it('has nothing without totals or without a gas car drawn', () => {
    expect(savingsSummary(null, [15])).toBeNull();
    expect(savingsSummary(totals, [])).toBeNull();
    expect(savingsSummary(totals, [99])).toBeNull();
  });
});
