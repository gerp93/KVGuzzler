import { describe, expect, it } from 'vitest';
import { flowTree } from './flow';
import { arrangeByPanel, shownMeters, underHouse, type Meter, type Summary } from './lib';
import { restRows } from './treeRest';

const m = (id: string, scope: Meter['scope'], parent: string | null = null): Meter =>
  ({ id, device_id: 'd', scope, label: id, enabled: true, type: scope === 'mains' ? 'house' : 'circuit', parent_id: parent, invert: false, pair_with: null, favorite: false, ev: false }) as unknown as Meter;

const summary: Summary = {
  from: 'a',
  to: 'b',
  house_kwh: 10,
  house_cost: '2.00',
  unmetered_kwh: 0,
  meters: [
    { meter_id: 'house', scope: 'mains', kwh: 10, cost: '2.00', monitored_hours: 1 },
    { meter_id: 'garage', scope: 'circuit', kwh: 6, cost: '1.20', monitored_hours: 1 },
    { meter_id: 'ev', scope: 'circuit', kwh: 5, cost: '1.00', monitored_hours: 1 },
    { meter_id: 'dryer', scope: 'circuit', kwh: 3, cost: '0.60', monitored_hours: 1 },
  ],
} as unknown as Summary;

describe('the "Other (not metered)" lines of the tree', () => {
  const meters = [m('house', 'mains'), m('dryer', 'circuit'), m('garage', 'circuit'), m('ev', 'circuit', 'garage')];
  const rows = underHouse(arrangeByPanel(shownMeters(meters)));
  const watts = new Map([['house', 8000], ['dryer', 3000], ['garage', 4200], ['ev', 4000]]);

  it('sits under the house, and under each meter that holds others, from the same figures as the flow chart', () => {
    const rest = restRows(rows, flowTree(meters, summary), watts, 8000);
    // rows: house(0) dryer(1) garage(1) ev(2): the garage's closes after ev, the house's after the last row
    expect(rows.map((r) => [r.meter.id, r.depth])).toEqual([['house', 0], ['dryer', 1], ['garage', 1], ['ev', 2]]);
    expect(rest.map((r) => [r.parent.id, r.after, r.depth])).toEqual([['garage', 3, 2], ['house', 3, 1]]);
    const garage = rest[0]!;
    expect(garage.kwh).toBeCloseTo(1); // 6 less the 5 inside it
    expect(garage.cost).toBe('0.20');
    expect(garage.watts).toBe(200);
    const house = rest[1]!;
    expect(house.kwh).toBeCloseTo(1); // 10 less the dryer's 3 and the garage's 6
    expect(house.cost).toBe('0.20');
    expect(house.watts).toBe(800); // 8000 less 3000 and 4200: the ev is inside the garage, not taken off again
  });

  it('is nothing, never negative, when the meters inside add up to the parent or a little more', () => {
    const over = restRows(rows, flowTree(meters, { ...summary, house_kwh: 9 } as Summary), new Map([['house', 7000], ['dryer', 3000], ['garage', 4200], ['ev', 4000]]), 7000);
    const house = over.find((r) => r.parent.id === 'house')!;
    expect(house.kwh).toBe(0);
    expect(house.watts).toBe(0);
    expect(house.cost).toBe('0.00'); // a price is known: nothing, not unknown
  });

  it('has no cost where no price is known, and no power where the parent has no reading', () => {
    const noPrice = { ...summary, house_cost: null, meters: summary.meters.map((x) => ({ ...x, cost: null })) } as unknown as Summary;
    const rest = restRows(rows, flowTree(meters, noPrice), new Map(), null);
    expect(rest.every((r) => r.cost === null && r.watts === null)).toBe(true);
  });

  it('draws nothing for a meter with nothing listed inside it', () => {
    const flat = underHouse(arrangeByPanel(shownMeters([m('dryer', 'circuit')])));
    expect(restRows(flat, undefined, new Map(), null)).toEqual([]);
  });
});
