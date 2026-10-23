import { describe, expect, it } from 'vitest';
import {
  applianceList, circuitsByWatts, formatDuration, formatKwh, formatMoney, formatWatts, gapLabel, groupComponents, isTheme, pcSlices, periodRange, splitSummary, sumByTime, unmetered,
  type Channel, type Computer, type PcComponent, type Reading, type Series, type Summary,
} from './lib';

const ch = (id: string, scope: Channel['scope'], enabled = true): Channel => ({
  id, device_id: 'd', scope, label: id, enabled, index: null, max_watts: null,
});
const rd = (channel_id: string, watts: number): Reading => ({ channel_id, watts });

const channels = [ch('mains-a', 'mains'), ch('dryer', 'circuit'), ch('ev', 'circuit'), ch('off', 'circuit', false), ch('pc', 'pc'), ch('plug', 'plug')];
const readings = [rd('mains-a', 9000), rd('dryer', 5000), rd('ev', 200), rd('off', 999), rd('pc', 300), rd('plug', 50), rd('ghost', 1)];

describe('scopes are never summed across each other', () => {
  it('circuitsByWatts leaves out mains, the PC slice, switched-off and unknown channels, and ranks by watts', () => {
    expect(circuitsByWatts(channels, readings).map((r) => r.channel.id)).toEqual(['dryer', 'ev', 'plug']);
  });

  it('keeps the PC slice apart', () => {
    expect(pcSlices(channels, readings).map((r) => [r.channel.id, r.watts])).toEqual([['pc', 300]]);
  });

  it('unmetered is the house minus circuits only, never negative, and never subtracts the PC or plugs', () => {
    const rows = circuitsByWatts(channels, readings);
    expect(unmetered(9000, rows)).toBe(9000 - 5200); // the plug's 50 W is not a circuit
    expect(unmetered(100, rows)).toBe(0);
    expect(unmetered(null, rows)).toBeNull();
  });

  it('splitSummary does not fold the PC into the circuits', () => {
    const s: Summary = {
      from: 'a', to: 'b', house_kwh: 10, unmetered_kwh: 4,
      channels: [
        { channel_id: 'mains-a', scope: 'mains', kwh: 10, monitored_hours: 1 },
        { channel_id: 'dryer', scope: 'circuit', kwh: 5, monitored_hours: 1 },
        { channel_id: 'ev', scope: 'circuit', kwh: 1, monitored_hours: 1 },
        { channel_id: 'pc', scope: 'pc', kwh: 3, monitored_hours: 1 },
      ],
    };
    const split = splitSummary(s);
    expect(split.circuits.map((c) => c.channel_id)).toEqual(['dryer', 'ev']);
    expect(split.pc.map((c) => c.channel_id)).toEqual(['pc']);
    expect(split.houseKwh).toBe(10);
  });
});

describe('sumByTime', () => {
  const s = (points: [string, number][]): Series => ({
    channel_id: 'x', bucket: 'hour',
    points: points.map(([t, w]) => ({ t, avg_watts: w, max_watts: w * 2, kwh: w / 1000 })),
  });

  it('adds legs at the same time and sorts', () => {
    const sum = sumByTime([s([['2026-01-01T02:00:00Z', 100], ['2026-01-01T01:00:00Z', 10]]), s([['2026-01-01T01:00:00Z', 20]])]);
    expect(sum.map((p) => [p.t, p.avg_watts])).toEqual([['2026-01-01T01:00:00Z', 30], ['2026-01-01T02:00:00Z', 100]]);
    expect(sum[0]!.max_watts).toBe(60);
  });

  it('does not change the series it was given', () => {
    const a = s([['2026-01-01T01:00:00Z', 10]]);
    sumByTime([a, s([['2026-01-01T01:00:00Z', 20]])]);
    expect(a.points[0]!.avg_watts).toBe(10);
  });
});

describe('formatting and periods', () => {
  it('formats watts and kWh', () => {
    expect(formatWatts(312.4)).toBe('312 W');
    expect(formatWatts(2726.3)).toBe('2.73 kW');
    expect(formatWatts(null)).toBe('—');
    expect(formatKwh(3.456)).toBe('3.46 kWh');
    expect(formatKwh(120.04)).toBe('120.0 kWh');
    expect(formatKwh(0.0036)).toBe('3.6 Wh'); // a few watt-hours is not "0.00 kWh"
    expect(formatKwh(0)).toBe('0.00 kWh');
  });

  it('formats money as dollars and cents, marking an estimate', () => {
    expect(formatMoney('13.68')).toBe('$13.68');
    expect(formatMoney('0.199155')).toBe('$0.20');
    expect(formatMoney('61.9')).toBe('$61.90');
    expect(formatMoney('-4')).toBe('-$4.00');
    expect(formatMoney('13.68', true)).toBe('≈ $13.68');
    expect(formatMoney(null)).toBe('—');
    expect(formatMoney('abc')).toBe('—');
  });

  it('periodRange ends at the next whole minute', () => {
    const r = periodRange('1h', new Date('2026-10-22T18:04:11Z'));
    expect(r).toEqual({ from: '2026-10-22T17:05:00.000Z', to: '2026-10-22T18:05:00.000Z' });
  });

  it('knows its themes', () => {
    expect(isTheme('blue-oval')).toBe(true);
    expect(isTheme('nope')).toBe(false);
    expect(isTheme(undefined)).toBe(false);
  });
});

describe('the PC tab helpers', () => {
  const comp = (name: string, scope: PcComponent['scope'], kwh: number): PcComponent => ({ name, scope, kind: 'other', kwh, monitored_hours: 1 });

  it('groups the PC readings by what they are and never mixes the groups', () => {
    const g = groupComponents([
      comp('CPU cores', 'breakdown', 0.4), comp('GPU', 'component', 2), comp('Wall meter', 'wall', 3),
      comp('CPU package', 'component', 0.6), comp('Monitor', 'peripheral', 0.2),
    ]);
    expect(g.wall.map((c) => c.name)).toEqual(['Wall meter']);
    expect(g.sensors.map((c) => c.name)).toEqual(['GPU', 'CPU package']); // biggest first
    expect(g.inside.map((c) => c.name)).toEqual(['CPU cores']);
    expect(g.meters.map((c) => c.name)).toEqual(['Monitor']);
  });

  it('words the gap reasons and durations', () => {
    expect(gapLabel('asleep')).toBe('Asleep');
    expect(gapLabel('off')).toBe('Off or stopped');
    expect(gapLabel('lost_power')).toBe('Lost power');
    expect(formatDuration(45)).toBe('45 s');
    expect(formatDuration(12 * 60)).toBe('12 min');
    expect(formatDuration(4.5 * 3600)).toBe('4 h 30 min');
    expect(formatDuration(2 * 3600)).toBe('2 h');
    expect(formatDuration(-5)).toBe('0 s');
  });
});

describe('the Appliances list', () => {
  const summary: Summary = {
    from: 'a', to: 'b', house_kwh: 20, unmetered_kwh: 5,
    channels: [
      { channel_id: 'mains-a', scope: 'mains', kwh: 20, monitored_hours: 1 },
      { channel_id: 'dryer', scope: 'circuit', kwh: 6, monitored_hours: 1 },
      { channel_id: 'fridge', scope: 'plug', kwh: 2, monitored_hours: 1 },
      { channel_id: 'wall', computer_id: 'comp-1', scope: 'pc', kwh: 5, monitored_hours: 1 },
      { channel_id: 'monitor', computer_id: 'comp-1', scope: 'plug', kwh: 1, monitored_hours: 1 },
    ],
  };
  const channels = ['dryer', 'fridge', 'wall', 'monitor'].map((id) => ch(id, 'plug'));
  const computer = (id: string, wall?: string): Computer => ({ id, name: id, peripheral_channel_ids: [], wall_channel_id: wall ?? null });

  it('lists ordinary appliances without the ones that belong to a computer, and not the house', () => {
    const list = applianceList(summary, channels, [computer('comp-1', 'wall')]);
    expect(list.appliances.map((r) => r.channel.channel_id)).toEqual(['dryer', 'fridge']);
  });

  it('lists a computer with its wall plug figure, biggest first, and one with no wall plug last', () => {
    const list = applianceList(summary, channels, [computer('empty'), computer('comp-1', 'wall')]);
    expect(list.computers.map((r) => [r.computer.id, r.wall?.kwh])).toEqual([['comp-1', 5], ['empty', undefined]]);
  });
});
