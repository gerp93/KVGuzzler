import { describe, expect, it } from 'vitest';
import { runBars, runLength, runStats, runWhen, showRuns, type MeterRuns } from './runs';

const base = (over: Partial<MeterRuns> = {}): MeterRuns => ({
  meter_id: 'ev',
  threshold_watts: 1800,
  meter_kwh: 100,
  runs: [],
  totals: { count: 4, kwh: 80, cost: '12.00', minutes: 480, longest_minutes: 200, peak_watts: 7400 },
  truncated: false,
  cost_estimated: false,
  from: '2026-10-17T00:00:00Z',
  to: '2026-10-24T00:00:00Z',
  ...over,
});

describe('runStats', () => {
  it('averages the runs and says how much of the meter they hold', () => {
    expect(runStats(base())).toEqual({ count: 4, avgKwh: 20, avgCost: 3, avgMinutes: 120, longestMinutes: 200, shareOfMeter: 80 });
  });
  it('has no average cost unless every run is priced', () => {
    expect(runStats(base({ totals: { ...base().totals, cost: null } }))!.avgCost).toBeNull();
  });
  it('is nothing for a steady meter or a period without runs', () => {
    expect(runStats(base({ threshold_watts: null }))).toBeNull();
    expect(runStats(base({ totals: { ...base().totals, count: 0, kwh: 0, minutes: 0, longest_minutes: 0 } }))).toBeNull();
  });
  it('never claims more than all of the meter, and says nothing about a meter with no energy', () => {
    expect(runStats(base({ meter_kwh: 50 }))!.shareOfMeter).toBe(100);
    expect(runStats(base({ meter_kwh: 0 }))!.shareOfMeter).toBeNull();
  });
});

describe('the card is for meters that run', () => {
  it('hides for a steady meter and before the answer arrives', () => {
    expect(showRuns(base())).toBe(true);
    expect(showRuns(base({ threshold_watts: null }))).toBe(false);
    expect(showRuns(undefined)).toBe(false);
  });
});

describe('runBars', () => {
  it('sizes each run against the biggest, with a visible sliver for a tiny one', () => {
    expect(runBars([{ kwh: 10 }, { kwh: 5 }, { kwh: 0.01 }])).toEqual([100, 50, 3]);
    expect(runBars([])).toEqual([]);
    expect(runBars([{ kwh: 0 }])).toEqual([0]);
  });
});

describe('words', () => {
  it('shows a run length', () => {
    expect(runLength(45)).toBe('45 min');
    expect(runLength(120)).toBe('2 h');
    expect(runLength(135)).toBe('2 h 15 min');
  });
  it('shows the weekday for a recent run and the date for an old one', () => {
    const now = new Date(2026, 9, 24, 12, 0);
    const recent = runWhen(new Date(2026, 9, 22, 18, 0).toISOString(), now);
    const old = runWhen(new Date(2026, 9, 1, 18, 0).toISOString(), now);
    expect(recent).toMatch(/^[A-Za-z]{3} /);
    expect(old).toMatch(/Oct/);
  });
});
