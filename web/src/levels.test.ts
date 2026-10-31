import { describe, expect, it } from 'vitest';
import type { Meter } from './lib';
import { levelsById, meterState, runningNow, stateTitle, type MeterLevels } from './levels';

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

describe('runningNow', () => {
  const m = (id: string, scope: Meter['scope'], parent: string | null = null, enabled = true): Meter =>
    ({ id, device_id: 'd', scope, label: id, enabled, type: 'circuit', parent_id: parent, invert: false, pair_with: null, favorite: false, ev: false }) as unknown as Meter;
  const meters = [m('house', 'mains'), m('garage', 'circuit'), m('c', 'circuit', 'garage'), m('f', 'plug'), m('dryer', 'circuit'), m('unknown', 'plug')];
  const dryer: MeterLevels = { meter_id: 'dryer', rest_watts: 1, working_above_watts: 300, minutes: 1000 };
  const levels = { meters: [charger, fridge, dryer, { ...charger, meter_id: 'garage' }], from: '', to: '' };

  it('lists the appliances above their working level, biggest first, and no feed, resting or unknown meter', () => {
    const readings = [{ meter_id: 'house', watts: 9000 }, { meter_id: 'garage', watts: 7200 }, { meter_id: 'c', watts: 7200 }, { meter_id: 'f', watts: 90 }, { meter_id: 'dryer', watts: 5000 }, { meter_id: 'unknown', watts: 800 }];
    expect(runningNow(meters, readings, levels).map((r) => [r.meter.id, r.watts])).toEqual([['c', 7200], ['dryer', 5000]]);
    expect(runningNow(meters, readings, { ...levels, meters: [{ ...charger, meter_id: 'c' }, { ...dryer, working_above_watts: 6000 }] }).map((r) => r.meter.id)).toEqual(['c']);
  });
  it('is empty with nothing working, no readings or no levels', () => {
    expect(runningNow(meters, [{ meter_id: 'c', watts: 2 }], levels)).toEqual([]);
    expect(runningNow(meters, [], levels)).toEqual([]);
    expect(runningNow(meters, [{ meter_id: 'c', watts: 7200 }], undefined)).toEqual([]);
  });
});
