import { describe, expect, it } from 'vitest';
import { circuitsByWatts, nestCircuits, pcSlices, type Meter, type Reading, type Summary } from './lib';
import { notMeteredRows, unclamped } from './notMetered';

const m = (id: string, scope: Meter['scope'], parent: string | null = null): Meter =>
  ({ id, device_id: 'd', scope, label: id, enabled: true, type: 'circuit', parent_id: parent, invert: false, pair_with: null, favorite: false, ev: false }) as unknown as Meter;
const rd = (meter_id: string, watts: number): Reading => ({ meter_id, watts }) as unknown as Reading;

const summary = {
  from: 'a',
  to: 'b',
  house_kwh: 10,
  house_cost: '2.00',
  unmetered_kwh: 4,
  meters: [
    { meter_id: 'range', scope: 'circuit', kwh: 6, cost: '1.20', monitored_hours: 1 },
    { meter_id: 'monitor', scope: 'plug', kwh: 0.5, cost: '0.10', monitored_hours: 1 },
    { meter_id: 'pc', scope: 'pc', kwh: 1.5, cost: '0.30', monitored_hours: 1 },
  ],
} as unknown as Summary;

describe('the Everything else group of the circuits table', () => {
  const meters = [m('house', 'mains'), m('range', 'circuit'), m('monitor', 'plug'), m('pc', 'pc'), m('fan', 'plug', 'range')];
  const reads = [rd('house', 1000), rd('range', 600), rd('monitor', 40), rd('pc', 160), rd('fan', 20)];
  const circuits = nestCircuits(circuitsByWatts(meters, reads));
  const plugs = unclamped(circuits, pcSlices(meters, reads));

  it('lists the plugs that are on no clamped circuit, biggest first, and not the one on a circuit', () => {
    expect(plugs.map((r) => r.meter.id)).toEqual(['pc', 'monitor']);
    expect(circuits.map((r) => r.meter.id)).toEqual(['range', 'fan', 'range', 'monitor']); // the fan is under the range, with the range's own remainder
  });

  it('is the house less the clamped circuits, with the plugs inside it and what none of them explains last', () => {
    const rows = notMeteredRows({ house: 1000, clamped: circuits, plugs, summary, host: meters[0]! });
    expect(rows.map((r) => [r.lump?.label ?? r.meter.id, r.depth, Math.round(r.watts)])).toEqual([
      ['Everything else', 0, 400], // 1000 less the range's 600: the plugs are not taken off again
      ['pc', 1, 160],
      ['monitor', 1, 40],
      ['Not individually metered', 1, 200],
    ]);
    expect(rows[0]!.lump).toMatchObject({ kwh: 4, cost: '0.80' }); // 2.00 less the range's 1.20
    expect(rows[3]!.lump!.kwh).toBeCloseTo(2); // 4 less 1.5 and 0.5
    expect(rows[3]!.lump!.cost).toBe('0.40'); // 0.80 less 0.30 and 0.10
  });

  it('is only its own line when no plug is outside the clamps, and has no cost when a price is missing', () => {
    const none = notMeteredRows({ house: 1000, clamped: circuits, plugs: [], summary: { ...summary, house_cost: null } as unknown as Summary, host: meters[0]! });
    expect(none).toHaveLength(1);
    expect(none[0]!.lump!.cost).toBeNull();
  });

  it('says nothing about what is left when the circuits used more than the house did (the house was not recorded all that time)', () => {
    const short = { ...summary, house_kwh: 5, unmetered_kwh: 0 } as unknown as Summary; // the range alone used 6
    const rows = notMeteredRows({ house: 1000, clamped: circuits, plugs, summary: short, host: meters[0]! });
    expect(rows[0]!.lump).toMatchObject({ kwh: null, cost: null });
    expect(rows[3]!.lump).toMatchObject({ kwh: null, cost: null });
    expect(Math.round(rows[0]!.watts)).toBe(400); // what it draws now is still known
  });
});
