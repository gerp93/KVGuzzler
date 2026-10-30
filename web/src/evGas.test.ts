import { describe, expect, it } from 'vitest';
import type { EvSavings } from './ev';
import { carName, compKey, countedStretches, cumulativeCosts, fuelWords, gasLineStyle, isShown, parsePicks, picksFromOldHidden, savingsSummary, togglePick, visibleComparisons, type GasComparison } from './evGas';

const comps: GasComparison[] = [
  { fuel: 'gas', mpg: 15, custom: false },
  { fuel: 'gas', mpg: 20, custom: false },
  { fuel: 'gas', mpg: 25, custom: false },
  { fuel: 'gas', mpg: 27.5, custom: true },
  { fuel: 'gas', mpg: 40, custom: false },
  { fuel: 'diesel', mpg: 15, custom: false },
  { fuel: 'diesel', mpg: 25, custom: false },
  { fuel: 'diesel', mpg: 40, custom: false },
];

type Stretch = EvSavings['stretches'][number];

/** A stretch whose cost for each car is its miles over the car's miles per gallon at 4 dollars a gallon of gas and 5 of diesel. */
const stretch = (from: string, to: string, miles: number, ev: string | null, priced = true): Stretch => ({
  from_day: from,
  to_day: to,
  miles,
  gas_price: priced ? '4.00' : null,
  diesel_price: priced ? '5.00' : null,
  ev_cost: ev,
  gas: comps.map((c) => {
    const cost = (miles / c.mpg) * (c.fuel === 'diesel' ? 5 : 4);
    return { fuel: c.fuel, mpg: c.mpg, gallons: miles / c.mpg, gas_cost: priced ? cost.toFixed(2) : null, savings: priced && ev != null ? (cost - Number(ev)).toFixed(2) : null };
  }),
});

describe('picks', () => {
  it('reads what was saved, and anything else as none', () => {
    expect(parsePicks('{"off":["gas:15"],"on":["diesel:25"]}')).toEqual({ off: ['gas:15'], on: ['diesel:25'] });
    for (const bad of ['', 'nope', '[]', '5', 'null']) expect(parsePicks(bad)).toEqual({ off: [], on: [] });
    expect(parsePicks('{"off":["gas:15", 4, null],"on":"x"}')).toEqual({ off: ['gas:15'], on: [] });
  });

  it('carries over the gas cars switched off before there was diesel', () => {
    expect(picksFromOldHidden('[15, 27.5]')).toEqual({ off: ['gas:15', 'gas:27.5'], on: [] });
    for (const bad of ['', 'nope', '{"a":1}']) expect(picksFromOldHidden(bad)).toEqual({ off: [], on: [] });
  });

  it('draws every gas car and no diesel car until told otherwise', () => {
    const none = { off: [], on: [] };
    expect(visibleComparisons(comps, none).map(compKey)).toEqual(['gas:15', 'gas:20', 'gas:25', 'gas:27.5', 'gas:40']);
    expect(visibleComparisons(comps, { off: ['gas:15', 'gas:40'], on: ['diesel:25'] }).map(compKey)).toEqual(['gas:20', 'gas:25', 'gas:27.5', 'diesel:25']);
    expect(isShown({ fuel: 'diesel', mpg: 15 }, none)).toBe(false);
    expect(isShown({ fuel: 'gas', mpg: 15 }, { off: ['diesel:15'], on: [] })).toBe(true); // switching off a diesel car says nothing of the gas car
  });

  it('ticks and unticks: a gas car by the list switched off, diesel by the list switched on', () => {
    let p = togglePick({ off: [], on: [] }, 'gas:20');
    expect(p).toEqual({ off: ['gas:20'], on: [] });
    p = togglePick(p, 'diesel:25');
    expect(p).toEqual({ off: ['gas:20'], on: ['diesel:25'] });
    p = togglePick(p, 'gas:20');
    p = togglePick(p, 'diesel:25');
    expect(p).toEqual({ off: [], on: [] });
    expect(togglePick({ off: [], on: [] }, 'diesel:price')).toEqual({ off: [], on: ['diesel:price'] });
  });
});

describe('names and looks', () => {
  it('names a diesel car as such', () => {
    expect(carName({ fuel: 'gas', mpg: 25 })).toBe('25 mpg');
    expect(carName({ fuel: 'diesel', mpg: 25 })).toBe('25 mpg diesel');
  });

  it('draws the presets of each fuel as one colour at falling strength, the thirstiest car the strongest', () => {
    for (const fuel of ['gas', 'diesel'] as const) {
      const styles = comps.filter((c) => !c.custom && c.fuel === fuel).map((c) => gasLineStyle(c, comps));
      expect(styles.every((s) => s.colorVar === '--color-accent-red')).toBe(true);
      expect(styles[0]!.alpha).toBe(1);
      expect(styles[styles.length - 1]!.alpha).toBeCloseTo(0.45, 9);
      for (let i = 1; i < styles.length; i++) expect(styles[i]!.alpha).toBeLessThan(styles[i - 1]!.alpha);
    }
  });

  it('draws a diesel car the same as its gas twin but dashed', () => {
    const gas = gasLineStyle(comps[0]!, comps);
    const diesel = gasLineStyle(comps[5]!, comps);
    expect(gas.dashed).toBe(false);
    expect(diesel.dashed).toBe(true);
    expect({ ...diesel, dashed: false }).toEqual(gas);
  });

  it('gives the owners own car a colour of its own at full strength', () => {
    expect(gasLineStyle(comps[3]!, comps)).toEqual({ colorVar: '--color-accent-blue', alpha: 1, width: 2, dashed: false });
  });
});

describe('countedStretches', () => {
  it('counts a stretch that has every price and the electric cost', () => {
    const list = [stretch('2026-10-01', '2026-10-05', 100, '2.00'), stretch('2026-10-05', '2026-10-09', 100, null), stretch('2026-10-09', '2026-10-12', 100, '2.00', false)];
    expect(countedStretches(list).map((s) => s.from_day)).toEqual(['2026-10-01']);
  });

  it('needs no diesel price when no diesel car is compared', () => {
    const gasOnly: Stretch = { ...stretch('2026-10-01', '2026-10-05', 100, '2.00'), diesel_price: null, gas: stretch('2026-10-01', '2026-10-05', 100, '2.00').gas.filter((g) => g.fuel === 'gas') };
    expect(countedStretches([gasOnly]).length).toBe(1);
  });
});

describe('cumulativeCosts', () => {
  it('adds each car, charging and the savings up over the counted stretches, from zero on the first day', () => {
    const c = cumulativeCosts({ comparisons: comps, stretches: [stretch('2026-10-01', '2026-10-05', 100, '2.00'), stretch('2026-10-05', '2026-10-09', 100, '2.00')] });
    expect(c.x.map((s) => new Date(s * 1000).toISOString().slice(0, 10))).toEqual(['2026-10-01', '2026-10-05', '2026-10-09']);
    expect(c.ev).toEqual([0, 2, 4]);
    expect(c.gas.get('gas:20')).toEqual([0, 20, 40]); // 100 miles at 20 per gallon is 5 gallons at 4 dollars
    expect(c.gas.get('diesel:25')).toEqual([0, 20, 40]); // 4 gallons at 5 dollars
    expect(c.gas.get('gas:40')).toEqual([0, 10, 20]);
    expect(c.saved.get('gas:20')).toEqual([0, 18, 36]);
    // The gap between a car and charging is the savings.
    for (const comp of comps) {
      const gas = c.gas.get(compKey(comp))!;
      const saved = c.saved.get(compKey(comp))!;
      gas.forEach((g, i) => expect(g - c.ev[i]!).toBeCloseTo(saved[i]!, 2));
    }
  });

  it('counts only the stretches that have every cost, so every line covers the same days', () => {
    const c = cumulativeCosts({
      comparisons: comps,
      stretches: [stretch('2026-10-01', '2026-10-05', 100, null), stretch('2026-10-05', '2026-10-09', 100, '2.00'), stretch('2026-10-09', '2026-10-12', 100, '2.00', false)],
    });
    expect(c.ev).toEqual([0, 2]);
    expect(c.gas.get('gas:20')).toEqual([0, 20]);
    expect(new Date(c.x[0]! * 1000).toISOString().slice(0, 10)).toBe('2026-10-05');
  });

  it('can go below zero when a car cost less than charging', () => {
    const c = cumulativeCosts({ comparisons: comps, stretches: [stretch('2026-10-01', '2026-10-05', 100, '12.00')] });
    expect(c.saved.get('gas:40')).toEqual([0, -2]); // 100 miles at 40 per gallon is 10 dollars of gas against 12 of charging
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
      { fuel: 'gas', mpg: 15, gallons: 12.67, gas_cost: '55.00', savings: '51.56', gas_cost_per_mile: '0.289474' },
      { fuel: 'gas', mpg: 25, gallons: 7.6, gas_cost: '33.00', savings: '29.56', gas_cost_per_mile: '0.173684' },
      { fuel: 'gas', mpg: 40, gallons: 4.75, gas_cost: '20.625', savings: '17.185', gas_cost_per_mile: '0.108553' },
      { fuel: 'diesel', mpg: 25, gallons: 7.6, gas_cost: '41.25', savings: '37.81', gas_cost_per_mile: '0.217105' },
    ],
  };

  it('gives the least and the most saved over the cars drawn', () => {
    const s = savingsSummary(totals, ['gas:15', 'gas:25', 'gas:40'])!;
    expect(s.count).toBe(3);
    expect(s.gasCount).toBe(3);
    expect(s.dieselCount).toBe(0);
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

  it('counts a diesel car by its own key, so the same miles per gallon is a different car', () => {
    const diesel = savingsSummary(totals, ['diesel:25'])!;
    expect(diesel.count).toBe(1);
    expect(diesel.lowest).toBeCloseTo(37.81, 9);
    expect(diesel.dieselCount).toBe(1);
    const both = savingsSummary(totals, ['gas:25', 'diesel:25'])!;
    expect(both.lowest).toBeCloseTo(29.56, 9);
    expect(both.highest).toBeCloseTo(37.81, 9);
  });

  it('follows the cars ticked, down to one', () => {
    const one = savingsSummary(totals, ['gas:25'])!;
    expect(one.count).toBe(1);
    expect(one.lowest).toBe(one.highest);
  });

  it('has nothing without totals or without a car drawn', () => {
    expect(savingsSummary(null, ['gas:15'])).toBeNull();
    expect(savingsSummary(totals, [])).toBeNull();
    expect(savingsSummary(totals, ['gas:99'])).toBeNull();
  });

  it('says what kind of cars it stands for', () => {
    expect(fuelWords({ gasCount: 3, dieselCount: 0 })).toBe('gas');
    expect(fuelWords({ gasCount: 0, dieselCount: 2 })).toBe('diesel');
    expect(fuelWords({ gasCount: 1, dieselCount: 1 })).toBe('gas and diesel');
  });
});
