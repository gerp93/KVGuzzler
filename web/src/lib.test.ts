import { describe, expect, it } from 'vitest';
import {
  applianceList, BASELINE, circuitsByWatts, formatAge, OTHERS, programRows, programSplit, UNATTRIBUTED, gapShades, pcLineStyle, formatDuration, idleText, isLiveStale, liveSplit, programsNow, runDuration, runKwh, sharePercent, formatKwh, formatMoney, formatWatts, gapLabel, groupComponents, isTheme, pcSlices, periodRange, splitSummary, sumByTime, unmetered,
  type ApplianceGroup, type Channel, type Computer, type PcComponent, type PcLiveReading, type PcProgramTotal, type Reading, type Series, type Summary,
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
      { channel_id: 'dryer', scope: 'circuit', kwh: 6, cost: '0.90', monitored_hours: 1 },
      { channel_id: 'fridge', scope: 'plug', kwh: 2, monitored_hours: 1 },
      { channel_id: 'wall', computer_id: 'comp-1', scope: 'pc', kwh: 5, monitored_hours: 1 },
      { channel_id: 'monitor', computer_id: 'comp-1', scope: 'plug', kwh: 1, monitored_hours: 1 },
      { channel_id: 'strip-1', group_id: 'grp-1', scope: 'plug', kwh: 3, cost: '0.45', monitored_hours: 1 },
      { channel_id: 'strip-2', group_id: 'grp-1', scope: 'plug', kwh: 1.5, cost: '0.23', monitored_hours: 1 },
    ],
  };
  const channels = ['dryer', 'fridge', 'wall', 'monitor', 'strip-1', 'strip-2'].map((id) => ch(id, 'plug'));
  const computer = (id: string, wall?: string): Computer => ({ id, name: id, peripheral_channel_ids: [], wall_channel_id: wall ?? null });
  const group: ApplianceGroup = { id: 'grp-1', name: 'Entertainment center', channel_ids: ['strip-1', 'strip-2'] };

  it('lists ordinary appliances without the plugs that belong to a computer or a group, and not the house', () => {
    const list = applianceList(summary, channels, [computer('comp-1', 'wall')], [group]);
    expect(list.appliances.map((r) => [r.kind, r.id])).toEqual([['channel', 'dryer'], ['group', 'grp-1'], ['channel', 'fridge']]);
  });

  it('shows a group once, as the sum of its plugs, with a cost only when every plug has one', () => {
    const row = applianceList(summary, channels, [], [group]).appliances.find((r) => r.kind === 'group')!;
    expect(row).toMatchObject({ label: 'Entertainment center', kwh: 4.5, cost: '0.68' });

    const partly: Summary = { ...summary, channels: summary.channels.map((c) => (c.channel_id === 'strip-2' ? { ...c, cost: null } : c)) };
    expect(applianceList(partly, channels, [], [group]).appliances.find((r) => r.kind === 'group')!.cost).toBeNull();
  });

  it('leaves out a group with no plugs reporting', () => {
    const empty: ApplianceGroup = { id: 'grp-2', name: 'Nothing', channel_ids: [] };
    expect(applianceList(summary, channels, [], [empty]).appliances.some((r) => r.id === 'grp-2')).toBe(false);
  });

  it('lists a computer with its wall plug figure, biggest first, and one with no wall plug last', () => {
    const list = applianceList(summary, channels, [computer('empty'), computer('comp-1', 'wall')]);
    expect(list.computers.map((r) => [r.computer.id, r.wall?.kwh])).toEqual([['comp-1', 5], ['empty', undefined]]);
  });
});

const lr = (name: string, scope: PcLiveReading['scope'], watts: number): PcLiveReading => ({ name, scope, kind: 'other', watts });

describe('the PC right now', () => {
  const readings = [lr('GPU', 'component', 120), lr('CPU package', 'component', 60), lr('CPU cores', 'breakdown', 50), lr('Wall meter', 'wall', 300), lr('Monitor', 'peripheral', 30)];

  it('keeps wall, sensors, inside and meters in their own groups and shares the wall by the sensors and the rest', () => {
    const s = liveSplit({ readings });
    expect(s.sensors.map((r) => r.name)).toEqual(['GPU', 'CPU package']);
    expect(s.inside.map((r) => r.name)).toEqual(['CPU cores']);
    expect(s.meters.map((r) => r.name)).toEqual(['Monitor']);
    expect(s.wall?.watts).toBe(300);
    expect(s.sensorWatts).toBe(180); // the CPU cores inside the package and the monitor are not added
    expect(s.baseWatts).toBe(300);
    expect(s.baseIsWall).toBe(true);
    expect(s.otherWatts).toBe(120);
  });

  it('shares the sensors total when there is no wall meter, and never reports a negative remainder', () => {
    const none = liveSplit({ readings: readings.filter((r) => r.scope !== 'wall') });
    expect([none.baseWatts, none.baseIsWall, none.otherWatts]).toEqual([180, false, null]);
    expect(liveSplit({ readings: [lr('GPU', 'component', 200), lr('Wall meter', 'wall', 150)] }).otherWatts).toBe(0);
  });

  it('percent shares need something to share of', () => {
    expect(sharePercent(120, 300)).toBe(40);
    expect(sharePercent(5, 0)).toBeNull();
  });

  it('lists programs biggest first and drops the ones causing next to nothing', () => {
    const rows = programsNow({ programs: [{ name: 'a', cpu_watts: 1, gpu_watts: 0 }, { name: 'b', cpu_watts: 20, gpu_watts: 110 }, { name: 'idle', cpu_watts: 0.1, gpu_watts: 0.2 }] });
    expect(rows.map((r) => [r.name, r.watts])).toEqual([['b', 130], ['a', 1]]);
  });

  it('says what the estimate is measured from, and that a missing level is not learned yet', () => {
    expect(idleText({ idle_cpu_watts: 18.5, idle_gpu_watts: null })).toBe('CPU 19 W, GPU not learned yet');
  });

  it('is stale after half a minute and says how old in words', () => {
    expect([isLiveStale({ age_seconds: 12 }), isLiveStale({ age_seconds: 31 })]).toEqual([false, true]);
    expect([formatAge(2), formatAge(95), formatAge(7200)]).toEqual(['just now', '2 min ago', '2 h ago']);
  });

  it('adds a run CPU and GPU energy and measures its length', () => {
    const run = { name: 'x', start: '2026-10-22T12:00:00Z', end: '2026-10-22T12:20:00Z', cpu_kwh: 0.01, gpu_kwh: 0.04, active_seconds: 1, still_running: false };
    expect(runKwh(run)).toBeCloseTo(0.05);
    expect(runDuration(run)).toBe(1200);
  });
});

describe('the inside-the-PC chart', () => {
  it('draws the wall in the text colour, sensors by kind and meters dashed', () => {
    expect(pcLineStyle({ scope: 'wall', kind: 'other' }, 0)).toEqual({ colorVar: '--color-text', dashed: false });
    expect(pcLineStyle({ scope: 'component', kind: 'cpu' }, 1).colorVar).toBe('--color-accent-blue');
    expect(pcLineStyle({ scope: 'component', kind: 'gpu' }, 2).colorVar).toBe('--color-accent-green');
    expect(pcLineStyle({ scope: 'peripheral', kind: 'other' }, 0).dashed).toBe(true);
  });

  it('turns gaps into shaded stretches and ignores an empty one', () => {
    expect(gapShades([{ from: '2026-10-22T12:00:00Z', to: '2026-10-22T12:10:00Z' }, { from: '2026-10-22T13:00:00Z', to: '2026-10-22T13:00:00Z' }])).toEqual([
      { from: Date.parse('2026-10-22T12:00:00Z') / 1000, to: Date.parse('2026-10-22T12:10:00Z') / 1000 },
    ]);
  });
});

const prog = (name: string, cpu: number, gpu: number, active = 0, cost: string | null = null): PcProgramTotal => ({
  name, cpu_kwh: cpu, gpu_kwh: gpu, kwh: cpu + gpu, active_seconds: active, cost,
});

describe('programs over a period', () => {
  const totals = [
    prog(BASELINE, 0.05, 0.02),
    prog('python.exe (ComfyUI)', 0.1, 0.4, 3600, '0.10'),
    prog(UNATTRIBUTED, 0.001, 0),
    prog('chrome.exe', 0.02, 0, 120, '0.004'),
    prog('tiny-a.exe', 0.00001, 0, 3, '0.00000'),
    prog('tiny-b.exe', 0.00002, 0, 5, '0.00001'),
  ];

  it('lists programs biggest first, rolls the tiny ones into one line, and ends with the two rows that belong to no program', () => {
    const rows = programRows(totals, null);
    expect(rows.map((r) => r.name)).toEqual(['python.exe (ComfyUI)', 'chrome.exe', '2 smaller programs', BASELINE, UNATTRIBUTED]);
    expect(rows.map((r) => r.kind)).toEqual(['program', 'program', 'rollup', 'baseline', 'unattributed']);
    expect(rows[0]!.note).toBe('GPU 0.40 kWh · CPU 0.10 kWh · active 1 h');
    expect(rows[2]!.cost).toBe('0.00'); // 0.00000 + 0.00001, shown as the sum of the two
  });

  it('shares the yardstick: the whole PC when it is known, else what the CPU and GPU used', () => {
    const own = programRows(totals, null);
    expect(own[0]!.share).toBeCloseTo(0.5 / 0.59101, 4);
    const wall = programRows(totals, 5);
    expect(wall[0]!.share).toBeCloseTo(0.1, 6);
    expect(wall.every((r) => r.share >= 0 && r.share <= 1)).toBe(true);
  });

  it('splits into programs, baseline, not attributed and, with the wall known, everything else', () => {
    const noWall = programSplit(totals, null);
    expect(noWall.map((p) => p.kind)).toEqual(['programs', 'baseline', 'unattributed']);
    const wall = programSplit(totals, 5);
    expect(wall.map((p) => p.kind)).toEqual(['programs', 'baseline', 'unattributed', 'rest']);
    expect(wall[3]!.kwh).toBeCloseTo(5 - 0.52003 - 0.07 - 0.001, 5);
    // a wall figure a hair under what the parts add up to is not shown as a negative rest
    expect(programSplit(totals, 0.5).some((p) => p.kind === 'rest')).toBe(false);
  });

  it('names a program with the many-small-programs label', () => {
    const rows = programRows([prog(OTHERS, 0.01, 0, 90)], null);
    expect(rows[0]!.note).toBe('many small programs together · active up to 2 min');
  });
});
