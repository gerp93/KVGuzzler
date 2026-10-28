import { describe, expect, it } from 'vitest';
import { levelsById, meterState, stateTitle, type MeterLevels } from './levels';

const charger: MeterLevels = { meter_id: 'c', rest_watts: 3, working_above_watts: 50, minutes: 1000 };
const fridge: MeterLevels = { meter_id: 'f', rest_watts: 92, working_above_watts: null, minutes: 1000 };

describe('meterState', () => {
  it('is working from the working level up and resting below it', () => {
    expect(meterState(charger, 2)).toBe('resting');
    expect(meterState(charger, 49.9)).toBe('resting');
    expect(meterState(charger, 50)).toBe('working');
    expect(meterState(charger, 7200)).toBe('working');
  });
  it('is always resting for a meter that barely changes', () => {
    expect(meterState(fridge, 0)).toBe('resting');
    expect(meterState(fridge, 5000)).toBe('resting');
  });
  it('says nothing without levels or a reading', () => {
    expect(meterState(undefined, 100)).toBeUndefined();
    expect(meterState(charger, null)).toBeUndefined();
    expect(meterState(charger, undefined)).toBeUndefined();
  });
});

describe('stateTitle', () => {
  it('names the usual resting level', () => {
    expect(stateTitle('working', charger)).toContain('3 W');
    expect(stateTitle('resting', charger)).toContain('3 W');
    expect(stateTitle('resting', fridge)).toContain('barely changes');
  });
});

describe('levelsById', () => {
  it('looks a meter up by id', () => {
    const m = levelsById({ meters: [charger, fridge], from: '', to: '' });
    expect(m.get('f')).toBe(fridge);
    expect(levelsById(undefined).size).toBe(0);
  });
});
