import { describe, expect, it } from 'vitest';
import { guzzlable, summaryHours, topGuzzlers } from './guzzlers';
import type { Meter, Summary } from './lib';

const m = (id: string, scope: Meter['scope'], parent: string | null = null, enabled = true): Meter =>
  ({ id, device_id: 'd', scope, label: id, enabled, type: 'circuit', parent_id: parent, invert: false, pair_with: null, favorite: false, ev: false }) as unknown as Meter;

const f = (id: string, scope: string, kwh: number, cost: string | null, peak = 1000) => ({ meter_id: id, scope, kwh, cost, peak_watts: peak, monitored_hours: 1 });

const meters = [m('house', 'mains'), m('garage', 'circuit'), m('ev', 'circuit', 'garage'), m('welder', 'circuit', 'garage'), m('dryer', 'circuit'), m('fridge', 'plug'), m('off', 'circuit', null, false)];
// Ten hours: the fridge rests at 50 W (0.5 kWh of its 1), the dryer at 10 W, the EV charger at nothing.
const summary = {
  from: '2026-10-26T00:00:00Z',
  to: '2026-10-26T10:00:00Z',
  house_kwh: 20,
  meters: [f('house', 'mains', 20, '4.00'), f('garage', 'circuit', 12, '2.40'), f('ev', 'circuit', 11, '2.20', 7300), f('welder', 'circuit', 0, '0.00'), f('dryer', 'circuit', 5, '1.50', 5000), f('fridge', 'plug', 1, '0.20', 150), f('off', 'circuit', 9, '9.00')],
} as unknown as Summary;
const rests = new Map([['fridge', 50], ['dryer', 10]]);

describe('the top guzzlers', () => {
  it('ranks only meters that hold nothing: never the house, a feed, or a hidden meter, so nothing is listed twice', () => {
    expect(guzzlable(meters).map((x) => x.id)).toEqual(['ev', 'welder', 'dryer', 'fridge']);
    expect(topGuzzlers(meters, summary, 'kwh', 20).map((g) => g.meter.id)).toEqual(['ev', 'dryer', 'fridge']); // the welder used nothing
  });

  it('is biggest first by energy, with its share of the house and its peak', () => {
    const top = topGuzzlers(meters, summary, 'kwh', 20);
    expect(top.map((g) => [g.meter.id, g.kwh, g.share, g.peakWatts])).toEqual([['ev', 11, 55, 7300], ['dryer', 5, 25, 5000], ['fridge', 1, 5, 150]]);
  });

  it('can be ordered by cost instead, and a meter with no known price comes last', () => {
    const noEvPrice = { ...summary, meters: summary.meters.map((x) => (x.meter_id === 'dryer' ? { ...x, cost: '3.00' } : x.meter_id === 'ev' ? { ...x, cost: null } : x)) } as unknown as Summary;
    expect(topGuzzlers(meters, noEvPrice, 'cost', 20).map((g) => g.meter.id)).toEqual(['dryer', 'fridge', 'ev']);
  });

  it('active guzzlers are ranked by the energy above their resting level, and the rest is left out of them', () => {
    expect(summaryHours(summary)).toBe(10);
    const active = topGuzzlers(meters, summary, 'kwh', 20, 'active', rests);
    // the dryer rests at 10 W: 0.1 kWh of its 5; the fridge rests at 50 W: 0.5 of its 1
    expect(active.map((g) => [g.meter.id, Number(g.kwh.toFixed(2))])).toEqual([['ev', 11], ['dryer', 4.9], ['fridge', 0.5]]);
    expect(active[1]!.cost).toBe('1.47'); // the same average price: 1.50 * 4.9 / 5
  });

  it('guzzlers at rest are ranked by their always-on power, with what that adds up to over the period', () => {
    const rest = topGuzzlers(meters, summary, 'kwh', 20, 'rest', rests);
    expect(rest.map((g) => [g.meter.id, g.restWatts, Number(g.kwh.toFixed(2))])).toEqual([['fridge', 50, 0.5], ['dryer', 10, 0.1]]); // the EV charger rests at nothing: not listed
    expect(rest[0]!.cost).toBe('0.10');
  });

  it('never takes more at rest than a meter used (a level learned from a longer history)', () => {
    const rest = topGuzzlers(meters, summary, 'kwh', 20, 'rest', new Map([['fridge', 5000]]));
    expect(rest[0]!.kwh).toBe(1);
    expect(topGuzzlers(meters, summary, 'kwh', 20, 'active', new Map([['fridge', 5000]])).some((g) => g.meter.id === 'fridge')).toBe(false); // all of it was rest
  });

  it('leaves the share out when it does not add up (the house has less history than the meter) or the house is unknown', () => {
    expect(topGuzzlers(meters, summary, 'kwh', 8)[0]!.share).toBeNull(); // 11 of 8 kWh
    expect(topGuzzlers(meters, summary, 'kwh', null)[0]!.share).toBeNull();
  });

  it('is empty without a summary, without anything that used energy, or at rest with no resting levels known', () => {
    expect(topGuzzlers(meters, undefined, 'kwh', 20)).toEqual([]);
    expect(topGuzzlers(meters, { ...summary, meters: [] } as unknown as Summary, 'kwh', 20)).toEqual([]);
    expect(topGuzzlers(meters, summary, 'kwh', 20, 'rest')).toEqual([]);
  });
});
