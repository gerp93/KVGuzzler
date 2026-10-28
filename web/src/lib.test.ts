import { describe, expect, it } from 'vitest';
import { effectiveDomain } from './Chart';
import {
  arrangeByPanel, underHouse, isKilowatts, isKilowattHours, visiblePeriods, type PeriodKey, BASELINE, canHold, clampName, indentTarget, inputName, outdentTarget, shownMeters, usualPartner, filterTariffs, formatRate, rateRange, sharedUtility, houseShare, houseUsageBasis, carbonFacts, formatCarbon, currencySymbol, setCurrencyCode, tariffFacts, formatBytes, COUNT_PC, countedTotals, countedWatts, estimateLines, parseCountOff, toggleCount, usageBasis, historyDetail, PERIODS, circuitsByWatts, nestCircuits, formatAge, OTHERS, programRows, programSplit, UNATTRIBUTED, gapShades, pcLineStyle, formatDuration, idleText, isLiveStale, liveSplit, programsNow, runDuration, runKwh, sharePercent, formatKwh, formatMoney, formatWatts, gapLabel, groupComponents, isTheme, pcSlices, periodRange, splitSummary, sumByTime, unmetered,
  type Meter, type MeterSummary, type PcComponent, type AverageUse, type PcHistoryPeriod, type PcLiveReading, type PcProgramTotal, type Reading, type Series, type Summary,
} from './lib';

const ch = (id: string, scope: Meter['scope'], enabled = true): Meter => ({
  id, device_id: 'd', scope, type: scope === 'mains' ? 'house' : scope === 'circuit' ? 'circuit' : 'device', label: id, enabled, index: null, max_watts: null,
});
const rd = (meter_id: string, watts: number): Reading => ({ meter_id, watts });

const meters = [ch('mains-a', 'mains'), ch('dryer', 'circuit'), ch('ev', 'circuit'), ch('off', 'circuit', false), ch('pc', 'pc'), ch('plug', 'plug')];
const readings = [rd('mains-a', 9000), rd('dryer', 5000), rd('ev', 200), rd('off', 999), rd('pc', 300), rd('plug', 50), rd('ghost', 1)];

describe('scopes are never summed across each other', () => {
  it('circuitsByWatts leaves out mains, the PC slice, switched-off and unknown meters, and ranks by watts', () => {
    expect(circuitsByWatts(meters, readings).map((r) => r.meter.id)).toEqual(['dryer', 'ev', 'plug']);
  });

  it('keeps the PC slice apart', () => {
    expect(pcSlices(meters, readings).map((r) => [r.meter.id, r.watts])).toEqual([['pc', 300]]);
  });

  it('unmetered is the house minus circuits only, never negative, and never subtracts the PC or plugs', () => {
    const rows = circuitsByWatts(meters, readings);
    expect(unmetered(9000, rows)).toBe(9000 - 5200); // the plug's 50 W is not a circuit
    expect(unmetered(100, rows)).toBe(0);
    expect(unmetered(null, rows)).toBeNull();
  });

  it('a sub-panel feed holds its circuits: they are listed under it, taken off the house only once, and what is left is its own line', () => {
    const wired = [
      ch('mains-a', 'mains'), ch('garage', 'circuit'), { ...ch('ev', 'circuit'), parent_id: 'garage' }, { ...ch('lights', 'circuit'), parent_id: 'garage' },
      ch('dryer', 'circuit'), { ...ch('orphan', 'circuit'), parent_id: 'switched-off' }, ch('switched-off', 'circuit', false),
    ];
    const reads = [rd('mains-a', 12000), rd('garage', 7500), rd('ev', 7300), rd('lights', 50), rd('dryer', 4000), rd('orphan', 20), rd('switched-off', 99)];
    const rows = nestCircuits(circuitsByWatts(wired, reads));
    expect(rows.map((r) => [r.meter.id, r.depth, !!r.remainder])).toEqual([
      ['garage', 0, false], ['ev', 1, false], ['lights', 1, false], ['garage', 1, true], ['dryer', 0, false], ['orphan', 0, false],
    ]);
    expect(rows.find((r) => r.remainder)!.watts).toBe(7500 - 7300 - 50);
    // The garage's circuits are inside the garage's figure: only the top level comes off the house.
    expect(unmetered(12000, rows)).toBe(12000 - 7500 - 4000 - 20);
  });

  it('arrangeByPanel puts a sub-panel feed before the circuits inside it and keeps the order otherwise', () => {
    const list = [ch('a', 'circuit'), { ...ch('c', 'circuit'), parent_id: 'b' }, ch('b', 'circuit'), { ...ch('d', 'circuit'), parent_id: 'gone' }];
    expect(arrangeByPanel(list).map((r) => [r.meter.id, r.depth])).toEqual([['a', 0], ['b', 0], ['c', 1], ['d', 0]]);
  });

  it('underHouse puts the one house meter on top and everything else a level beneath it', () => {
    const list = [ch('a', 'circuit'), ch('house', 'mains'), { ...ch('c', 'circuit'), parent_id: 'b' }, ch('b', 'circuit')];
    const rows = underHouse(arrangeByPanel(list));
    expect(rows.map((r) => [r.meter.id, r.depth])).toEqual([['house', 0], ['a', 1], ['b', 1], ['c', 2]]);
    // no house meter, or more than one: nothing says what a top-level circuit hangs from
    const none = arrangeByPanel([ch('a', 'circuit'), ch('b', 'circuit')]);
    expect(underHouse(none)).toBe(none);
    const two = arrangeByPanel([ch('h1', 'mains'), ch('h2', 'mains'), ch('a', 'circuit')]);
    expect(underHouse(two)).toBe(two);
  });

  it('isKilowattHours is true exactly when formatKwh writes kWh, and an empty amount is not one', () => {
    expect(isKilowattHours(2.62)).toBe(true);
    expect(isKilowattHours(0.01)).toBe(true);
    expect(isKilowattHours(0.0099)).toBe(false);
    expect(isKilowattHours(0)).toBe(false);
    expect(isKilowattHours(null)).toBe(false);
    for (const k of [2.62, 0.16, 0.0018, 0.0025]) expect(isKilowattHours(k)).toBe(formatKwh(k).endsWith(' kWh'));
  });

  it('isKilowatts is the point where formatWatts changes unit', () => {
    expect(isKilowatts(999.4)).toBe(false);
    expect(isKilowatts(1000)).toBe(true);
    expect(isKilowatts(-7300)).toBe(true);
    expect(isKilowatts(null)).toBe(false);
    expect(formatWatts(7300)).toBe('7.30 kW');
    expect(formatWatts(999.4)).toBe('999 W');
  });

  it('splitSummary does not fold the PC into the circuits', () => {
    const s: Summary = {
      from: 'a', to: 'b', house_kwh: 10, unmetered_kwh: 4,
      meters: [
        { meter_id: 'mains-a', scope: 'mains', kwh: 10, monitored_hours: 1 },
        { meter_id: 'dryer', scope: 'circuit', kwh: 5, monitored_hours: 1 },
        { meter_id: 'ev', scope: 'circuit', kwh: 1, monitored_hours: 1 },
        { meter_id: 'pc', scope: 'pc', kwh: 3, monitored_hours: 1 },
      ],
    };
    const split = splitSummary(s);
    expect(split.circuits.map((c) => c.meter_id)).toEqual(['dryer', 'ev']);
    expect(split.pc.map((c) => c.meter_id)).toEqual(['pc']);
    expect(split.houseKwh).toBe(10);
  });
});

describe('sumByTime', () => {
  const s = (points: [string, number][]): Series => ({
    meter_id: 'x', bucket: 'hour',
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
    // More than nothing but under half a cent is not shown as no money at all; a real zero is.
    expect(formatMoney('0.0015')).toBe('<$0.01');
    expect(formatMoney('0.000852', true)).toBe('≈ <$0.01');
    expect(formatMoney('0.005')).toBe('$0.01');
    expect(formatMoney('0.00')).toBe('$0.00');
    expect(formatMoney('0')).toBe('$0.00');
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
    expect(rows[0]!.note).toBe('many small programs · active up to 2 min');
  });
});

describe('the periods the history reaches', () => {
  const now = new Date(2026, 9, 27, 18, 0, 0);
  const keys = (since: Date | string | null | undefined, selected: PeriodKey = '24h') =>
    visiblePeriods(since instanceof Date ? since.toISOString() : since, selected, now).map((p) => p.key);

  it('offers only the periods there is as much data as', () => {
    expect(keys(new Date(2026, 9, 27, 13, 0, 0), '1h')).toEqual(['1h', '3h', 'all']);
    expect(keys(new Date(2026, 9, 27, 10, 1, 0), '1h')).toEqual(['1h', '3h', 'all']);
    expect(keys(new Date(2026, 9, 27, 10, 0, 0), '1h')).toEqual(['1h', '3h', '8h', 'all']);
    expect(keys(new Date(2026, 9, 20, 18, 0, 0), '1h')).toEqual(['1h', '3h', '8h', '24h', '3d', '7d', 'all']);
  });

  it('keeps year to date for when there is data from before 1 January, and the year for a whole year', () => {
    expect(keys(new Date(2026, 0, 2, 0, 0, 0), '1h')).not.toContain('ytd');
    expect(keys(new Date(2026, 0, 1, 0, 0, 0), '1h')).toContain('ytd');
    expect(keys(new Date(2026, 0, 1, 0, 0, 0), '1h')).not.toContain('1y');
    expect(keys(new Date(2025, 9, 26, 0, 0, 0), '1h')).toEqual(['1h', '3h', '8h', '24h', '3d', '7d', '14d', '30d', 'ytd', '1y', 'all']);
  });

  it('always keeps the one picked, and everything when it is not known when recording began', () => {
    expect(keys(new Date(2026, 9, 27, 13, 0, 0), '1y')).toEqual(['1h', '3h', '1y', 'all']);
    expect(keys(null)).toHaveLength(11);
    expect(keys('nonsense')).toHaveLength(11);
  });
});

describe('year and all-time periods', () => {
  it('has hours, days, year to date, a year and an all-time choice, shortest first', () => {
    expect(PERIODS.map((p) => p.key)).toEqual(['1h', '3h', '8h', '24h', '3d', '7d', '14d', '30d', 'ytd', '1y', 'all']);
    const spans = (['3h', '8h', '3d', '14d'] as const).map((k) => {
      const r = periodRange(k, new Date('2026-10-22T18:04:11Z'));
      return (Date.parse(r.to) - Date.parse(r.from)) / 3600_000;
    });
    expect(spans).toEqual([3, 8, 72, 336]);
    // Year to date starts on 1 January in the viewer's own time and ends at the next whole minute.
    const now = new Date(2026, 9, 22, 18, 4, 11);
    const ytd = periodRange('ytd', now);
    expect(new Date(ytd.from).getTime()).toBe(new Date(2026, 0, 1).getTime());
    expect(Date.parse(ytd.to)).toBe(new Date(2026, 9, 22, 18, 5, 0).getTime());
    const year = periodRange('1y', new Date('2026-10-22T18:04:11Z'));
    expect(year.to).toBe('2026-10-22T18:05:00.000Z');
    expect(Date.parse(year.to) - Date.parse(year.from)).toBe(365 * 24 * 3600_000);
    expect(Date.parse(periodRange('all', new Date('2026-10-22T18:04:11Z')).from)).toBeLessThan(Date.parse('2005-01-01'));
  });

  it('starts a chart at its first point when the period begins years before it, and otherwise keeps the period', () => {
    const t = (iso: string) => Date.parse(iso) / 1000;
    const all = periodRange('all', new Date('2026-10-22T18:04:11Z'));
    const lines = [{ x: [t('2026-09-01T00:00:00Z'), t('2026-10-01T00:00:00Z')] }, { x: [t('2026-09-10T00:00:00Z')] }];
    expect(effectiveDomain([t(all.from), t(all.to)], lines)).toEqual([t('2026-09-01T00:00:00Z'), t(all.to)]);
    const day = periodRange('24h', new Date('2026-10-22T18:04:11Z'));
    expect(effectiveDomain([t(day.from), t(day.to)], lines)).toEqual([t(day.from), t(day.to)]);
    expect(effectiveDomain([1, 2], [{ x: [] }])).toEqual([1, 2]);
  });
});

describe('the energy history row detail', () => {
  const row = (over: Partial<PcHistoryPeriod>): PcHistoryPeriod => ({
    key: '24h', label: 'Last 24 hours', from: '', to: '', kwh: 4, basis: 'wall', cost: null, cost_estimated: false,
    sensors_kwh: 3, asleep_seconds: 0, off_seconds: 0, lost_power_seconds: 0, plug_gap_kwh: 0, ...over,
  });

  it('says how much the sensors account for and every stretch nothing was recorded, by reason', () => {
    expect(historyDetail(row({}))).toBe('CPU, GPU, memory 3.00 kWh (75%)');
    expect(historyDetail(row({ asleep_seconds: 13800, plug_gap_kwh: 0.06, off_seconds: 7200, lost_power_seconds: 600 }))).toBe(
      'CPU, GPU, memory 3.00 kWh (75%) · asleep 3 h 50 min · off 2 h · lost power 10 min',
    );
  });

  it('without a wall plug says the figure is the sensors, and leaves out the plug counter it does not have', () => {
    expect(historyDetail(row({ basis: 'sensors', asleep_seconds: 600 }))).toBe('Sensors only · asleep 10 min');
  });
});

describe('what a computer counts', () => {
  const sum = (id: string, kwh: number, cost: string | null, peak: number | null) =>
    ({ meter_id: id, scope: 'plug', kwh, cost, peak_watts: peak, monitored_hours: 1 }) as MeterSummary;
  const detail = {
    wall: { ...sum('wall', 1, '0.20', 300), scope: 'pc', carbon_g: 400, carbon_estimated_kwh: 0.25 } as MeterSummary,
    peripherals: [{ ...sum('monitor', 0.2, '0.04', 40), carbon_g: 80, carbon_estimated_kwh: 0.1 }, sum('speakers', 0.1, null, 10)],
  };
  const label = (id: string) => id.toUpperCase();

  it('counts everything until something is switched off, adding the parts and bounding the peak', () => {
    const t = countedTotals(detail, new Set(), label);
    expect(t.kwh).toBeCloseTo(1.3);
    expect(t.cost).toBeNull(); // the speakers have no price: a partly priced total would mislead
    expect([t.peak, t.peakIsBound]).toEqual([350, true]);
    expect(t.parts).toEqual(['the whole PC at the wall', 'MONITOR', 'SPEAKERS']);
  });

  it('leaves out what is unticked, the PC included, and says nothing when nothing is counted', () => {
    const noPc = countedTotals(detail, new Set([COUNT_PC, 'speakers']), label);
    expect([noPc.kwh, noPc.cost, noPc.peak, noPc.peakIsBound, noPc.parts]).toEqual([0.2, '0.04', 40, false, ['MONITOR']]);
    expect(countedTotals(detail, new Set([COUNT_PC, 'monitor', 'speakers']), label).kwh).toBeNull();
  });

  it('adds the draw right now of the ticked parts that have a reading', () => {
    const plugs = [{ key: 'monitor', watts: 30 }, { key: 'speakers', watts: null }];
    expect(countedWatts(300, plugs, new Set())).toBe(330);
    expect(countedWatts(300, plugs, new Set(['monitor']))).toBe(300);
    expect(countedWatts(null, plugs, new Set([COUNT_PC]))).toBe(30);
    expect(countedWatts(null, [], new Set())).toBeNull();
  });

  it('round-trips the switched-off list', () => {
    expect([...toggleCount(new Set(['a']), 'b')].sort()).toEqual(['a', 'b']);
    expect([...toggleCount(new Set(['a', 'b']), 'a')]).toEqual(['b']);
    expect([...parseCountOff('a,,b')].sort()).toEqual(['a', 'b']);
  });
});

describe('the cost estimator', () => {
  it('scales a day to an hour (when asked), 30 days and a year, and prices each at the rate', () => {
    const lines = estimateLines(2.4, '0.20', true);
    expect(lines.map((l) => l.label)).toEqual(['Per hour', 'Per day', 'Per 30 days', 'Per year']);
    expect(lines[1]).toMatchObject({ kwh: 2.4, cost: '0.4800' });
    expect(lines[3]!.kwh).toBeCloseTo(876);
    expect(estimateLines(2.4, null, false).map((l) => [l.label, l.cost])).toEqual([['Per day', null], ['Per 30 days', null], ['Per year', null]]);
  });

  it('says where a usage estimate comes from, and how much was really recorded', () => {
    const use: AverageUse = { kwh: 36.4, days: 7, kwh_per_day: 5.2, average_watts: 216.7, whole_pc: true, includes_pc: true, from: '', peripheral_count: 1 };
    expect(usageBasis(use, '7d')).toBe('Last 7 days: 36.4 kWh, 5.20 kWh a day, avg 217 W · whole PC at the wall + 1 peripheral');
    expect(usageBasis({ ...use, days: 0.125 }, '30d')).toContain('3 h recorded');
    expect(usageBasis({ ...use, includes_pc: false, peripheral_count: 2 }, '7d')).toContain('· 2 peripherals');
    expect(usageBasis({ ...use, whole_pc: false, peripheral_count: 0 }, '7d')).toContain('· sensors only');
  });
});

describe('file sizes', () => {
  it('reads in bytes, KB, MB and GB', () => {
    expect([formatBytes(812), formatBytes(217_088), formatBytes(3_565_158), formatBytes(5_000_000_000)]).toEqual(['812 B', '212 KB', '3.4 MB', '4.7 GB']);
  });
});

describe('currency and the looked-up price note', () => {
  it('shows the sign of the saved currency on every amount, and the code when there is no sign', () => {
    expect([currencySymbol('USD'), currencySymbol('EUR'), currencySymbol('GBP'), currencySymbol('SEK'), currencySymbol(undefined)]).toEqual(['$', '€', '£', 'SEK ', '$']);
    setCurrencyCode('EUR');
    expect(formatMoney('12.3')).toBe('€12.30');
    expect(formatMoney('-1', true)).toBe('≈ -€1.00');
    setCurrencyCode('SEK');
    expect(formatMoney('2')).toBe('SEK 2.00');
    setCurrencyCode('USD');
    expect(formatMoney('12.3')).toBe('$12.30');
  });

  it('names the utility and rate and what the period averaged to', () => {
    const tariff = { utility: 'Example Electric', name: 'Residential Time of Use', tiers: 2, fixed_charge: '6.75 $/month', varies_by_time: true };
    expect(tariffFacts(tariff, 10, '1.50')).toEqual([
      { label: 'Rate', value: 'Example Electric · Residential Time of Use' },
      { label: 'Average price', value: '$0.1500 per kWh, time-weighted' },
    ]);
    expect(tariffFacts({ ...tariff, varies_by_time: false }, 0, null)).toEqual([{ label: 'Rate', value: 'Example Electric · Residential Time of Use' }]);
  });
});

describe('the carbon footprint', () => {
  it('adds the carbon of what is counted and says how much was valued at an average', () => {
    const sum = (id: string, kwh: number, carbon_g: number | null, est: number | null) =>
      ({ meter_id: id, scope: 'plug', kwh, cost: null, peak_watts: null, monitored_hours: 1, carbon_g, carbon_estimated_kwh: est }) as MeterSummary;
    const detail = { wall: { ...sum('wall', 1, 400, 0.25), scope: 'pc' } as MeterSummary, peripherals: [sum('monitor', 0.2, 80, 0.1), sum('speakers', 0.1, null, null)] };
    const all = countedTotals(detail, new Set(), (id) => id);
    expect([all.carbonG, all.carbonEstimatedKwh]).toEqual([480, 0.35]);
    expect(countedTotals(detail, new Set([COUNT_PC, 'monitor']), (id) => id).carbonG).toBeNull(); // the speakers have no carbon: nothing to show
    expect(countedTotals({ ...detail, wall: undefined }, new Set(['monitor', 'speakers']), (id) => id).carbonG).toBeNull();
  });

  it('writes an amount as grams, kilograms or tonnes, always approximate', () => {
    expect([formatCarbon(412), formatCarbon(1240), formatCarbon(15_000), formatCarbon(2_100_000)]).toEqual(['≈ 412 g CO₂e', '≈ 1.24 kg CO₂e', '≈ 15.0 kg CO₂e', '≈ 2.1 t CO₂e']);
  });

  it('says the amount and the average it came to, and nothing without carbon', () => {
    expect(carbonFacts(480, 1.2)).toEqual([
      { label: 'Carbon', value: '≈ 480 g CO₂e' },
      { label: 'Grid average', value: '400 g/kWh' },
    ]);
    expect(carbonFacts(null, 1)).toEqual([]);
    expect(carbonFacts(5, null)).toEqual([]);
  });
});

describe('the house figures', () => {
  it('shares a part of the house as a whole-number percent and says nothing without a house figure', () => {
    expect(houseShare(5, 20)).toBe(25);
    expect(houseShare(5, 0)).toBeNull();
    expect(houseShare(5, null)).toBeNull();
    expect(houseShare(20, 20)).toBe(100);
    expect(houseShare(50, 20)).toBeNull(); // more than the house: its clamps were not recording for all that time
  });

  it('says where a house usage estimate comes from', () => {
    const use = { kwh: 36.4, days: 7, kwh_per_day: 5.2, average_watts: 216.7, whole_pc: true, includes_pc: false, from: '', peripheral_count: 0 };
    expect(houseUsageBasis(use, '7d')).toBe('Last 7 days: 36.4 kWh, 5.20 kWh a day, avg 217 W · mains clamps');
    expect(houseUsageBasis({ ...use, days: 0.125 }, '30d')).toContain('3 h recorded');
  });
});

describe('the list of rates found', () => {
  const t = (utility: string, name: string) => ({ utility, name });

  it('says a rate in short: one price, or the range by time of day', () => {
    expect(formatRate(0.1558)).toBe('$0.1558');
    expect(rateRange({ cheapest: 0.0727, dearest: 0.0727, varies_by_time: false })).toBe('$0.0727 per kWh');
    expect(rateRange({ cheapest: 0.1558, dearest: 0.1745, varies_by_time: true })).toBe('$0.1558 to $0.1745 per kWh by time of day');
  });

  it('filters by every word typed, in the name or the utility, in any case', () => {
    const list = [t('Northern States Power Co - Minnesota', 'Residential Service - Overhead Standard (A01)'), t('Northern States Power Co - Minnesota', 'Energy Controlled Service Non-demand metered Residential (A05)'), t('Other Co', 'Residential Service')];
    expect(filterTariffs(list, '')).toHaveLength(3);
    expect(filterTariffs(list, '  ')).toHaveLength(3);
    expect(filterTariffs(list, 'residential service')).toHaveLength(3); // every word, anywhere in the name
    expect(filterTariffs(list, 'energy controlled')).toEqual([list[1]]);
    expect(filterTariffs(list, 'OVERHEAD a01')).toEqual([list[0]]);
    expect(filterTariffs(list, 'other residential')).toEqual([list[2]]);
    expect(filterTariffs(list, 'nothing like it')).toEqual([]);
  });

  it('names the utility once when every rate is from it', () => {
    expect(sharedUtility([t('A', 'x'), t('A', 'y')])).toBe('A');
    expect(sharedUtility([t('A', 'x'), t('B', 'y')])).toBeNull();
    expect(sharedUtility([])).toBeNull();
  });
});

describe('the moves in the hierarchy tab', () => {
  const rf = (id: string, index: number, extra: Partial<Meter> = {}): Meter => ({ ...ch(id, 'circuit'), device_id: 'refoss-h', index, ...extra });
  const a1 = rf('a1', 1, { pair_with: 'b1' });
  const b1 = rf('b1', 7, { enabled: false });
  const a2 = rf('a2', 2, { parent_id: 'a1' });
  const a3 = rf('a3', 3, { parent_id: 'a1' });
  const a4 = rf('a4', 4);
  const plug = { ...ch('plug', 'plug'), parent_id: 'a4' };
  const all = [a1, b1, a2, a3, a4, plug];

  it('names a Refoss input as the monitor does, and shows a breaker once', () => {
    expect([inputName(a1), inputName(b1), inputName(plug), clampName(a2), clampName(plug)]).toEqual(['A1', 'B1', null, 'A2', 'plug']);
    expect(shownMeters(all).map((m) => m.id)).toEqual(['a1', 'a2', 'a3', 'a4', 'plug']);
  });

  it('suggests the same number on the other phase for a 240 V breaker', () => {
    expect(usualPartner(rf('x', 4), all)?.id).toBe(undefined);
    expect(usualPartner(a1, all)?.id).toBe('b1');
    expect(usualPartner(rf('x', 9), [rf('p', 3)])?.id).toBe('p');
    expect(usualPartner(rf('x', 14), all)).toBeUndefined();
  });

  it('only a shown circuit that is not inside the meter can hold it', () => {
    expect(canHold(a4, a2, all)).toBe(true);
    expect(canHold(a2, a1, all)).toBe(false); // a1 holds a2: a loop
    expect(canHold(a1, a1, all)).toBe(false);
    expect(canHold(plug, a2, all)).toBe(false); // a plug holds nothing
    expect(canHold(b1, a2, all)).toBe(false); // hidden
    expect(canHold(a4, ch('mains', 'mains'), all)).toBe(false);
  });

  it('the arrows: into the row above at the same level, out to the grandparent', () => {
    const rows = arrangeByPanel(shownMeters(all));
    expect(rows.map((r) => [r.meter.id, r.depth])).toEqual([['a1', 0], ['a2', 1], ['a3', 1], ['a4', 0], ['plug', 1]]);
    expect(indentTarget(rows, 0, all)).toBeUndefined(); // first row
    expect(indentTarget(rows, 1, all)).toBeUndefined(); // directly under its parent
    expect(indentTarget(rows, 2, all)?.id).toBe('a2'); // a3 goes into a2
    expect(indentTarget(rows, 3, all)?.id).toBe('a1'); // a4 goes into a1, skipping a1's circuits
    expect(outdentTarget(a2, all)).toEqual({ parent: null });
    expect(outdentTarget(a4, all)).toBeUndefined();
    expect(outdentTarget({ ...a3, parent_id: 'a2' }, [...all, { ...a2, parent_id: 'a1' }])).toEqual({ parent: 'a1' });
  });
});
