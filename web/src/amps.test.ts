import { describe, expect, it } from 'vitest';
import { BREAKER_SIZES, breakerAdvice, breakerLevel, breakerOptions, breakerPercent, formatAmps, formatVolts } from './amps';

describe('share of a breaker', () => {
  it('is the current over the size, in whole percent', () => {
    expect(breakerPercent(31.6, 50)).toBe(63);
    expect(breakerPercent(40, 50)).toBe(80);
  });
  it('is nothing without both numbers or with a breaker that is not a size', () => {
    expect(breakerPercent(null, 50)).toBeNull();
    expect(breakerPercent(10, null)).toBeNull();
    expect(breakerPercent(10, 0)).toBeNull();
    expect(breakerPercent(10, -5)).toBeNull();
  });
});

describe('breaker level (the continuous-load rule: 80%)', () => {
  it('is fine below 80, high from 80 to 100, over above 100', () => {
    expect(breakerLevel(79)).toBe('ok');
    expect(breakerLevel(80)).toBe('high');
    expect(breakerLevel(100)).toBe('high');
    expect(breakerLevel(101)).toBe('over');
    expect(breakerLevel(null)).toBeNull();
  });
  it('warns only above what a steady load should draw', () => {
    expect(breakerAdvice(70, 50)).toBeNull();
    expect(breakerAdvice(null, 50)).toBeNull();
    expect(breakerAdvice(85, 50)).toContain('85% of its 50 A breaker');
    expect(breakerAdvice(85, 50)).toContain('3 hours');
    expect(breakerAdvice(120, 30)).toContain('would trip');
  });
});

describe('words', () => {
  it('writes amps and volts', () => {
    expect(formatAmps(31.64)).toBe('31.6 A');
    expect(formatAmps(125.4)).toBe('125 A');
    expect(formatAmps(null)).toBe('—');
    expect(formatVolts(236.8)).toBe('237 V');
    expect(formatVolts(undefined)).toBe('—');
  });
  it('offers the usual sizes and keeps an unusual one that is set', () => {
    expect(breakerOptions(null)).toEqual(BREAKER_SIZES);
    expect(breakerOptions(35)).toContain(35);
    expect(breakerOptions(35)).toEqual([...breakerOptions(35)].sort((a, b) => a - b));
    expect(breakerOptions(50).filter((s) => s === 50)).toHaveLength(1);
  });
});
