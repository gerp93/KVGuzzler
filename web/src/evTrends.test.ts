import { describe, expect, it } from 'vitest';
import { efficiencyTrend, mpge, MIN_TREND_MILES, type EvInterval } from './ev';

const iv = (from: string, to: string, miles: number, kwh: number, cost: string | null): EvInterval => ({
  from_day: from,
  to_day: to,
  miles,
  kwh,
  cost,
  cost_per_mile: null,
  miles_per_kwh: null,
  coverage: 1,
  cost_estimated: false,
});

describe('mpge', () => {
  it("is miles per kWh times the EPA's kWh in a gallon", () => {
    expect(mpge(3)).toBeCloseTo(101.115, 3);
  });
});

describe('efficiencyTrend', () => {
  it('has nothing without readings', () => {
    expect(efficiencyTrend([])).toEqual([]);
  });

  it('shares a stretch evenly over its days and ends on the day before the later reading', () => {
    const t = efficiencyTrend([iv('2026-10-01', '2026-10-04', 90, 30, '4.50')], 3);
    expect(t.map((p) => p.day)).toEqual(['2026-10-01', '2026-10-02', '2026-10-03']);
    // 30 miles and 10 kWh a day: the same 3 miles per kWh in any full window. Before the window is full there is none.
    expect(t.slice(0, 2).map((p) => p.milesPerKwh)).toEqual([null, null]);
    expect(t[2]!.milesPerKwh).toBeCloseTo(3, 9);
  });

  it('puts the same date on a point in every time zone (noon UTC)', () => {
    const [p] = efficiencyTrend([iv('2026-10-26', '2026-10-27', 40, 12, '1.80')]);
    expect(new Date(p!.x * 1000).toISOString()).toBe('2026-10-26T12:00:00.000Z');
  });

  it('judges a week by its miles over its energy, so a charging day and a driving day even out', () => {
    // Day 1: driven 60 miles, no charge. Day 2: charged 20 kWh, driven nothing.
    const t = efficiencyTrend([iv('2026-10-01', '2026-10-02', 60, 0, '0.00'), iv('2026-10-02', '2026-10-03', 0, 20, '3.00')], 2);
    expect(t[0]!.milesPerKwh).toBeNull(); // not a full window yet
    expect(t[1]!.milesPerKwh).toBeCloseTo(3, 9); // 60 miles over 20 kWh
  });

  it('works out nothing from too few miles', () => {
    const t = efficiencyTrend([iv('2026-10-01', '2026-10-02', MIN_TREND_MILES - 1, 5, '0.75')], 1);
    expect(t[0]!.milesPerKwh).toBeNull();
    expect(efficiencyTrend([iv('2026-10-01', '2026-10-02', MIN_TREND_MILES, 5, '0.75')], 1)[0]!.milesPerKwh).toBeCloseTo(3, 9);
  });

  it('gives a window cost only when the window is full and every day in it is priced', () => {
    const stretches = Array.from({ length: 9 }, (_, i) => {
      const d = (n: number) => `2026-10-${String(n).padStart(2, '0')}`;
      return iv(d(i + 1), d(i + 2), 30, 10, i === 4 ? null : '1.50');
    });
    const t = efficiencyTrend(stretches);
    expect(t.slice(0, 6).every((p) => p.windowCost === null)).toBe(true); // not a full week yet, then a day without a price
    // Day 9 (index 8) has the unpriced day 5 in its window (days 3..9): still nothing, never a cost of "nothing".
    expect(t[8]!.windowCost).toBeNull();
  });

  it('adds a full priced window up', () => {
    const stretches = Array.from({ length: 8 }, (_, i) => {
      const d = (n: number) => `2026-10-${String(n).padStart(2, '0')}`;
      return iv(d(i + 1), d(i + 2), 30, 10, '1.50');
    });
    const t = efficiencyTrend(stretches);
    expect(t[5]!.windowCost).toBeNull(); // 6 days
    expect(t[6]!.windowCost).toBeCloseTo(10.5, 9); // 7 days at 1.50
    expect(t[7]!.windowCost).toBeCloseTo(10.5, 9); // still the last 7
  });

  it('ignores a stretch of no days', () => {
    expect(efficiencyTrend([iv('2026-10-01', '2026-10-01', 5, 5, '1')])).toEqual([]);
  });
});
