import { describe, expect, it } from 'vitest';
import { changeText, changeYearText, houseLine, stretchLabel } from './standby';

describe('standby text', () => {
  it('shows how a floor moved', () => {
    expect(changeText({ change_watts: null })).toBe('');
    expect(changeText({ change_watts: 0 })).toBe('no change');
    expect(changeText({ change_watts: -39.8 })).toBe('-40 W');
    expect(changeText({ change_watts: 12.4 })).toBe('+12 W');
    expect(changeText({ change_watts: -0.7 })).toBe('-0.7 W');
  });

  it('prices a change over a year', () => {
    expect(changeYearText({ change_watts: -40, change_cost_per_year: '-34.9' })).toBe('-$34.90 a year');
    expect(changeYearText({ change_watts: 12, change_cost_per_year: '10.5' })).toBe('+$10.50 a year');
    expect(changeYearText({ change_watts: 0, change_cost_per_year: '0' })).toBe('');
    expect(changeYearText({ change_watts: -3, change_cost_per_year: null })).toBe('');
  });

  it('compares the house with the stretch before', () => {
    expect(houseLine({ house_watts: 130, house_prev_watts: 150 })).toBe('The house never goes below 130 W (150 W before).');
    expect(houseLine({ house_watts: 130, house_prev_watts: null })).toBe('The house never goes below 130 W.');
    expect(houseLine({ house_watts: null, house_prev_watts: 150 })).toBeNull();
  });

  it('names a stretch by its first and last day', () => {
    expect(stretchLabel('2026-10-25T12:00:00Z', '2026-11-04T12:00:00Z')).toMatch(/Oct 25 to Nov 4/);
  });
});
