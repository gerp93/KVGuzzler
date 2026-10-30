import { describe, expect, it } from 'vitest';
import { gasPriceSeries, parseMpg } from './ev';

describe('gasPriceSeries', () => {
  it('puts a price at its week with noon UTC, so every time zone shows that date', () => {
    const s = gasPriceSeries([{ week: '2026-10-26', dollars: '4.146' }]);
    expect(s.y).toEqual([4.146]);
    expect(new Date(s.x[0]! * 1000).toISOString()).toBe('2026-10-26T12:00:00.000Z');
  });
});

describe('parseMpg', () => {
  it('takes a believable figure', () => {
    expect(parseMpg('25')).toBe(25);
    expect(parseMpg(' 27.5 ')).toBe(27.5);
  });
  it('refuses the rest', () => {
    for (const bad of ['', 'abc', '4', '201', '-5']) expect(parseMpg(bad)).toBeNull();
  });
});
