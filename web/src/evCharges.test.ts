import { describe, expect, it } from 'vitest';
import { batteryText, pctNote, perPct } from './ev';

describe('perPct', () => {
  it('keeps three places under 1 kWh and two above', () => {
    expect(perPct(0.4881)).toBe('0.488');
    expect(perPct(1.234)).toBe('1.23');
  });
});

describe('batteryText', () => {
  it('words the car reports at the two ends of a charge', () => {
    expect(batteryText({ start_pct: 34, end_pct: 85 })).toBe('34 to 85 %');
    expect(batteryText({ start_pct: 61.129997, end_pct: 85 })).toBe('61.1 to 85 %');
  });
  it('shows only the start while the end is unknown, and a dash with no start', () => {
    expect(batteryText({ start_pct: 61, end_pct: null })).toBe('61 %');
    expect(batteryText({ start_pct: null, end_pct: null })).toBe('—');
  });
});

describe('pctNote', () => {
  it('has nothing to say about a charge with an energy per percent', () => {
    expect(pctNote('ok')).toBeNull();
  });
  it('gives every other state a word and a sentence', () => {
    for (const s of ['active', 'none', 'waiting', 'early', 'small_gain'] as const) {
      const n = pctNote(s);
      expect(n?.short).toBeTruthy();
      expect(n?.long.length).toBeGreaterThan(20);
    }
  });
  it('never words a state in the first person', () => {
    for (const s of ['active', 'none', 'waiting', 'early', 'small_gain'] as const) {
      expect(pctNote(s)?.long).not.toMatch(/\b(my|our|I)\b/);
    }
  });
});
