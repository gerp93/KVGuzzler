import { describe, expect, it } from 'vitest';
import { hints, scaledToPeriod, shareText, verdictLabel, type BillCheck } from './reconcile';

const base: BillCheck = {
  start: '2026-10-01',
  end: '2026-10-31',
  days: 31,
  billed_kwh: 800,
  billed_total: '120.00',
  billed_fixed: '10.00',
  billed_variable: '110.00',
  billed_price: '0.1375',
  recorded_kwh: 760,
  coverage: 1,
  diff_kwh: -40,
  diff_share: -0.05,
  recorded_cost: '104.50',
  diff_cost: '-5.50',
  recorded_price: '0.1375',
  verdict: 'close',
  in_progress: false,
  outages: { power_seconds: 0, server_seconds: 0, unknown_seconds: 0 },
};

describe('reconcile text', () => {
  it('signs and rounds a share', () => {
    expect(shareText(-0.04)).toBe('-4.0%');
    expect(shareText(0.008)).toBe('+0.8%');
    expect(shareText(0.123)).toBe('+12%');
    expect(shareText(null)).toBe('—');
  });

  it('names the verdicts', () => {
    expect(verdictLabel('match')).toBe('Matches');
    expect(verdictLabel('differs')).toBe('Differs');
    expect(verdictLabel(null)).toBe('—');
  });

  it('says nothing for a fully recorded bill with no outages', () => {
    expect(hints(base)).toEqual([]);
  });

  it('says what was missing when only part of the period was recorded', () => {
    const c: BillCheck = { ...base, coverage: 0.8, recorded_kwh: 600, outages: { power_seconds: 7200, server_seconds: 0, unknown_seconds: 1800 } };
    expect(hints(c)).toEqual(['Recorded for 80% of the period.', 'At the same rate over all of it: 750 kWh (-6.3%).', 'In the outage log: power out 2 h, not recorded 30 min.']);
  });

  it('does not scale a period that was barely recorded', () => {
    expect(scaledToPeriod({ ...base, coverage: 0.3, recorded_kwh: 200 })).toBeNull();
    expect(scaledToPeriod({ ...base, coverage: 0.99 })).toBeNull();
    expect(scaledToPeriod({ ...base, coverage: 0.5, recorded_kwh: 400 })?.kwh).toBe(800);
  });

  it('handles nothing recorded and a bill that is not over', () => {
    expect(hints({ ...base, recorded_kwh: null, coverage: null })).toEqual(['Nothing recorded for these days.']);
    expect(hints({ ...base, in_progress: true })[0]).toBe('Not over yet.');
  });
});
