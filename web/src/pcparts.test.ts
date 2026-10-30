import { describe, expect, it } from 'vitest';
import { pcParts } from './pcparts';
import { formatMoney, type ComputerDetail } from './lib';

const detail = (extra: Partial<ComputerDetail> = {}, live = true): ComputerDetail => ({
  computer: { id: 'c1', name: 'Desk PC', peripheral_meter_ids: [], wall_meter_id: 'wall' },
  from: 'a', to: 'b', peripherals: [], cost_estimated: false,
  wall: { meter_id: 'wall', scope: 'pc', kwh: 10, cost: '1.50', monitored_hours: 24 },
  pc: {
    source_id: 's', programs: [], gaps: [],
    components: [
      { name: 'Memory', scope: 'component', kind: 'other', kwh: 0.5 },
      { name: 'GPU', scope: 'component', kind: 'gpu', kwh: 4 },
      { name: 'CPU package', scope: 'component', kind: 'cpu', kwh: 3 },
      { name: 'CPU cores', scope: 'breakdown', kind: 'cpu', kwh: 2.5 },
      { name: 'Wall meter', scope: 'wall', kind: 'other', kwh: 10 },
    ],
    live: live
      ? {
          at: 'now', age_seconds: 3,
          readings: [
            { name: 'GPU', scope: 'component', kind: 'gpu', watts: 120 },
            { name: 'CPU package', scope: 'component', kind: 'cpu', watts: 60 },
            { name: 'Memory', scope: 'component', kind: 'other', watts: 8 },
            { name: 'CPU cores', scope: 'breakdown', kind: 'cpu', watts: 50 },
          ],
        }
      : null,
  },
  ...extra,
});

describe('the parts of a computer under its wall plug', () => {
  it('are its sensors biggest first (not the parts inside a sensor, not the wall reading), then what they do not account for', () => {
    const parts = pcParts(detail(), 250);
    expect(parts.map((p) => [p.label, p.kwh])).toEqual([['GPU', 4], ['CPU package', 3], ['Memory', 0.5], ['Everything else', 2.5]]);
    expect(parts.at(-1)).toMatchObject({ remainder: true, kwh: 2.5, cost: '0.38' }); // 10 less 7.5 at the wall's 0.15 a kWh
  });

  it("apportion the wall plug's cost at its average price, and say nothing when it has none", () => {
    expect(pcParts(detail(), null)[0]!.cost).toBe('0.60');
    const free = detail({ wall: { meter_id: 'wall', scope: 'pc', kwh: 10, monitored_hours: 24 } });
    expect(pcParts(free, null).every((p) => p.cost === null)).toBe(true);
  });

  it('show what each part draws now, and the rest of the wall reading, only while the collector is fresh', () => {
    const parts = pcParts(detail(), 250);
    expect(parts.map((p) => p.watts)).toEqual([120, 60, 8, 62]); // 250 less 188
    expect(pcParts(detail({}, false), 250).map((p) => p.watts)).toEqual([null, null, null, null]);
    const stale = detail();
    stale.pc!.live = { ...stale.pc!.live!, age_seconds: 120 };
    expect(pcParts(stale, 250)[0]!.watts).toBeNull();
  });

  it('never go below zero when the sensors read more than the wall plug did, and are empty with no sensors or no wall plug', () => {
    const over = detail({ wall: { meter_id: 'wall', scope: 'pc', kwh: 6, cost: '0.90', monitored_hours: 24 } });
    expect(pcParts(over, null).at(-1)!.kwh).toBe(0);
    expect(pcParts(detail({ wall: null }), null)).toEqual([]);
    const none = detail();
    none.pc!.components = [];
    expect(pcParts(none, null)).toEqual([]);
    expect(pcParts(detail({ pc: null }), null)).toEqual([]);
  });
});

describe('a part whose cost is a sliver of a cent', () => {
  it('says so instead of rounding to nothing', () => {
    // The wall plug used 10 kWh for $1.50; a sensor that used 0.03 kWh cost under half a cent.
    const parts = pcParts(
      detail({
        wall: { meter_id: 'wall', scope: 'pc', kwh: 10, cost: '1.50', monitored_hours: 24 },
        pc: { source_id: 's', programs: [], gaps: [], components: [{ name: 'GPU', scope: 'component', kind: 'gpu', kwh: 0.03 }], live: null },
      }),
      null,
    );
    expect(formatMoney(parts[0]!.cost)).toBe('<$0.01');
    expect(formatMoney('0.00')).toBe('$0.00');
  });
});
