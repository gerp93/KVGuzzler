import { describe, expect, it } from 'vitest';
import { causesLine, dayLabel, dayLine, firstDays } from './anomalies';

describe('unusual days wording', () => {
  it('reads a plain date as that local day, whatever the time zone', () => {
    expect(dayLabel('2026-11-05')).toMatch(/5 Nov$/);
    expect(dayLabel('2026-01-31')).toMatch(/31 Jan$/);
  });
  it('says how much more than usual and what it cost, and drops the cost without a price', () => {
    expect(dayLine({ kwh: 50, usual_kwh: 20, extra_kwh: 30, extra_cost: '6.00' })).toBe('50.0 kWh instead of the usual 20.0 kWh: 30.0 kWh more, about $6.00');
    expect(dayLine({ kwh: 3, usual_kwh: 1.5, extra_kwh: 1.5, extra_cost: null })).toBe('3.00 kWh instead of the usual 1.50 kWh: 1.50 kWh more');
  });
  it('names the meters that made the day unusual, or nothing', () => {
    expect(causesLine([{ meter_id: 'a', label: 'EV charger', extra_kwh: 30 }, { meter_id: 'b', label: 'Workshop', extra_kwh: 3.1 }])).toBe('Mostly EV charger (+30.0 kWh), Workshop (+3.10 kWh)');
    expect(causesLine([])).toBe('');
  });
  it('holds days back behind show all', () => {
    expect(firstDays([1, 2, 3, 4, 5, 6], 4)).toEqual({ list: [1, 2, 3, 4], more: 2 });
    expect(firstDays([1], 4)).toEqual({ list: [1], more: 0 });
  });
});
