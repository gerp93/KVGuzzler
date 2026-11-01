import { describe, expect, it } from 'vitest';
import { dailySizes } from './dbsize';

// Local noon, so the day is the same in any time zone the tests run in.
const at = (day: number, hour = 12) => new Date(2026, 9, day, hour, 0, 0).toISOString();

describe('dailySizes', () => {
  it('keeps the last size of each day, newest first, with what it gained over the day before', () => {
    const rows = dailySizes([
      { at: at(20, 9), bytes: 100 },
      { at: at(20, 18), bytes: 110 },
      { at: at(21, 12), bytes: 150 },
      { at: at(23, 12), bytes: 150 },
    ]);
    expect(rows.map((r) => [r.day, r.bytes, r.change])).toEqual([
      ['2026-10-23', 150, 0],
      ['2026-10-21', 150, 40],
      ['2026-10-20', 110, null],
    ]);
  });

  it('says nothing for no readings and does not depend on the order they come in', () => {
    expect(dailySizes([])).toEqual([]);
    const rows = dailySizes([
      { at: at(21), bytes: 5 },
      { at: at(20), bytes: 3 },
    ]);
    expect(rows.map((r) => r.bytes)).toEqual([5, 3]);
  });
});
