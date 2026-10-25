import { describe, expect, it } from 'vitest';
import { splitFigure } from './figure';

describe('a big figure and its unit', () => {
  it('puts the unit beside the number', () => {
    expect(splitFigure('661.4 kWh')).toEqual(['661.4', 'kWh']);
    expect(splitFigure('9.23 kW')).toEqual(['9.23', 'kW']);
    expect(splitFigure('607 W')).toEqual(['607', 'W']);
    expect(splitFigure('≤ 14.93 kW')).toEqual(['≤ 14.93', 'kW']);
    expect(splitFigure('37.4 Wh')).toEqual(['37.4', 'Wh']);
  });
  it('keeps a currency sign with its number and has no unit for it', () => {
    expect(splitFigure('$104.51')).toEqual(['$104.51', '']);
    expect(splitFigure('≈ $5.57')).toEqual(['≈ $5.57', '']);
    expect(splitFigure('-$0.30')).toEqual(['-$0.30', '']);
    expect(splitFigure('€1,204.00')).toEqual(['€1,204.00', '']);
  });
  it('leaves a dash, or anything without a number, as it is', () => {
    expect(splitFigure('—')).toEqual(['—', '']);
    expect(splitFigure('')).toEqual(['', '']);
  });
});
