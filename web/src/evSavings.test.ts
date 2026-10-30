import { describe, expect, it } from 'vitest';
import { cumulativeSavings, gasPriceSeries, parseMpg, type EvSavings } from './ev';

const stretch = (to: string, savings: string | null): EvSavings['stretches'][number] => ({
  from_day: '2026-10-01',
  to_day: to,
  miles: 100,
  gallons: 4,
  gas_price: '4.00',
  gas_cost: '16.00',
  ev_cost: '2.00',
  savings,
});

describe('cumulativeSavings', () => {
  it('adds the savings up reading by reading', () => {
    const c = cumulativeSavings([stretch('2026-10-05', '14.00'), stretch('2026-10-09', '10.50')]);
    expect(c.y).toEqual([14, 24.5]);
    expect(c.x.map((s) => new Date(s * 1000).toISOString().slice(0, 10))).toEqual(['2026-10-05', '2026-10-09']);
  });

  it('skips a stretch with no savings without losing what came before', () => {
    const c = cumulativeSavings([stretch('2026-10-05', '14.00'), stretch('2026-10-09', null), stretch('2026-10-12', '1.00')]);
    expect(c.y).toEqual([14, 15]);
  });

  it('can go below zero when the electric car cost more', () => {
    expect(cumulativeSavings([stretch('2026-10-05', '-3.25')]).y).toEqual([-3.25]);
  });

  it('has nothing without stretches', () => {
    expect(cumulativeSavings([])).toEqual({ x: [], y: [] });
  });
});

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
