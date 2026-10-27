import { describe, expect, it } from 'vitest';
import { guzzlable, topGuzzlers } from './guzzlers';
import type { Meter, Summary } from './lib';

const m = (id: string, scope: Meter['scope'], parent: string | null = null, enabled = true): Meter =>
  ({ id, device_id: 'd', scope, label: id, enabled, type: 'circuit', parent_id: parent, invert: false, pair_with: null, favorite: false, ev: false }) as unknown as Meter;

const f = (id: string, scope: string, kwh: number, cost: string | null) => ({ meter_id: id, scope, kwh, cost, monitored_hours: 1 });

const meters = [m('house', 'mains'), m('garage', 'circuit'), m('ev', 'circuit', 'garage'), m('welder', 'circuit', 'garage'), m('dryer', 'circuit'), m('fridge', 'plug'), m('off', 'circuit', null, false)];
const summary = {
  from: 'a',
  to: 'b',
  house_kwh: 20,
  meters: [f('house', 'mains', 20, '4.00'), f('garage', 'circuit', 12, '2.40'), f('ev', 'circuit', 11, '2.20'), f('welder', 'circuit', 0, '0.00'), f('dryer', 'circuit', 5, '1.50'), f('fridge', 'plug', 1, '0.20'), f('off', 'circuit', 9, '9.00')],
} as unknown as Summary;

describe('the top guzzlers', () => {
  it('ranks only meters that hold nothing: never the house, a feed, or a hidden meter, so nothing is listed twice', () => {
    expect(guzzlable(meters).map((x) => x.id)).toEqual(['ev', 'welder', 'dryer', 'fridge']);
    expect(topGuzzlers(meters, summary, 'kwh', 20).map((g) => g.meter.id)).toEqual(['ev', 'dryer', 'fridge']); // the welder used nothing
  });

  it('is biggest first by energy, with its share of the house', () => {
    const top = topGuzzlers(meters, summary, 'kwh', 20);
    expect(top.map((g) => [g.meter.id, g.kwh, g.share])).toEqual([['ev', 11, 55], ['dryer', 5, 25], ['fridge', 1, 5]]);
  });

  it('can be ordered by cost instead, and a meter with no known price comes last', () => {
    const byCost = topGuzzlers(meters, { ...summary, meters: summary.meters.map((x) => (x.meter_id === 'dryer' ? { ...x, cost: '3.00' } : x.meter_id === 'ev' ? { ...x, cost: null } : x)) } as unknown as Summary, 'cost', 20);
    expect(byCost.map((g) => g.meter.id)).toEqual(['dryer', 'fridge', 'ev']);
  });

  it('leaves the share out when it does not add up (the house has less history than the meter) or the house is unknown', () => {
    expect(topGuzzlers(meters, summary, 'kwh', 8)[0]!.share).toBeNull(); // 11 of 8 kWh
    expect(topGuzzlers(meters, summary, 'kwh', null)[0]!.share).toBeNull();
  });

  it('is empty without a summary or without anything that used energy', () => {
    expect(topGuzzlers(meters, undefined, 'kwh', 20)).toEqual([]);
    expect(topGuzzlers(meters, { ...summary, meters: [] } as unknown as Summary, 'kwh', 20)).toEqual([]);
  });
});
