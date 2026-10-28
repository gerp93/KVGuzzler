import { describe, expect, it } from 'vitest';
import { dayLabel, formatDistance, formatEfficiency, formatMiles, inUnit, lowCoverage, parseMiles, stretchLabel, toMiles, today } from './ev';
import { formatPerMile } from './lib';

describe('a charging meter\'s odometer', () => {
  it('reads what is typed as miles: commas are fine, anything else is not', () => {
    expect(parseMiles('18,240')).toBe(18240);
    expect(parseMiles(' 0 ')).toBe(0);
    expect(parseMiles('1200.5')).toBe(1200.5);
    expect(parseMiles('')).toBeNull();
    expect(parseMiles('far')).toBeNull();
    expect(parseMiles('-5')).toBeNull();
  });

  it('dates a new reading with this device\'s calendar day, zero-padded', () => {
    expect(today(new Date(2026, 0, 5, 23, 59))).toBe('2026-01-05');
    expect(today(new Date(2026, 9, 26, 0, 1))).toBe('2026-10-26');
  });

  it('words a day without a time zone moving it, and adds the year only when it is not this year', () => {
    const now = new Date(2026, 9, 26);
    expect(dayLabel('2026-10-01', now)).toMatch(/1/);
    expect(dayLabel('2026-10-01', now)).not.toMatch(/2026/);
    expect(dayLabel('2025-12-31', now)).toMatch(/2025/);
    expect(dayLabel('2025-12-31', now)).toMatch(/31/); // not the 30th: a plain date, whatever the zone
    expect(dayLabel('nonsense', now)).toBe('nonsense');
    expect(stretchLabel({ from_day: '2026-10-01', to_day: '2026-10-26' }, now)).toContain(' to ');
  });

  it('words miles, efficiency and a cost per mile', () => {
    expect(formatMiles(335)).toBe('335 mi');
    expect(formatMiles(18240)).toMatch(/^18[,.   ]?240 mi$/);
    expect(formatEfficiency(3.466)).toBe('3.5 mi/kWh');
    expect(formatEfficiency(null)).toBe('—');
    expect(formatPerMile('0.045910')).toBe('$0.046/mi');
    expect(formatPerMile('0.045910', true)).toBe('≈ $0.046/mi');
    expect(formatPerMile(null)).toBe('—');
  });

  it('warns when the meter was measured for too little of a stretch', () => {
    expect(lowCoverage(1)).toBe(false);
    expect(lowCoverage(0.95)).toBe(false);
    expect(lowCoverage(0.8)).toBe(true);
  });
});

describe('the distance unit', () => {
  it('shows miles as they are and converts kilometers from the stored miles', () => {
    expect(formatDistance(58205, 'miles')).toMatch(/^58[,.   ]?205 mi$/);
    expect(formatDistance(58205.08, 'km')).toMatch(/^93[,.   ]?672 km$/);
    expect(formatDistance(10, 'km')).toBe('16.1 km');
    expect(formatMiles(335)).toBe('335 mi');
  });

  it('converts what is typed back to miles', () => {
    expect(toMiles(93672, 'km')).toBeCloseTo(58205.08, 1);
    expect(toMiles(1200, 'miles')).toBe(1200);
    expect(inUnit(toMiles(500, 'km'), 'km')).toBeCloseTo(500, 9);
  });

  it('words efficiency and cost per distance in the unit', () => {
    expect(formatEfficiency(3.5)).toBe('3.5 mi/kWh');
    expect(formatEfficiency(3.5, 'km')).toBe('5.6 km/kWh');
    expect(formatEfficiency(null, 'km')).toBe('—');
    expect(formatPerMile('0.0500')).toBe('$0.050/mi');
    expect(formatPerMile('0.0500', false, 'km')).toBe('$0.031/km');
    expect(formatPerMile('0.0500', true, 'km')).toBe('≈ $0.031/km');
  });
});

