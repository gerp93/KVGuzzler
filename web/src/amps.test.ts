import { describe, expect, it } from 'vitest';
import { BREAKER_SIZES, SUSTAINED_MINUTES, breakerAdvice, breakerLevel, breakerOptions, breakerPercent, breakerVerdict, formatAmps, formatVolts, sustainedHigh, typicalAmps } from './amps';

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

const run = (minutes: number, avg: number | null, start = '2026-10-22T20:00:00Z') => ({ start, minutes, avg_amps: avg });

describe('the continuous-load rule is about runs of 3 hours or more', () => {
  it('finds the run that most breaks it, and ignores short ones and low ones', () => {
    expect(SUSTAINED_MINUTES).toBe(180);
    expect(sustainedHigh([run(120, 49), run(200, 30)], 50)).toBeNull(); // the short run is high, the long one is not
    expect(sustainedHigh([run(179, 49)], 50)).toBeNull();
    const w = sustainedHigh([run(190, 41, 'a'), run(240, 47, 'b'), run(200, 20, 'c')], 50);
    expect(w?.run.start).toBe('b');
    expect(w?.percent).toBe(94);
  });
  it('needs a breaker and a current', () => {
    expect(sustainedHigh([run(300, 49)], null)).toBeNull();
    expect(sustainedHigh([run(300, null)], 50)).toBeNull();
  });
});

describe('what is said about a breaker', () => {
  it('is nothing without a peak', () => {
    expect(breakerVerdict(null, [], 50)).toEqual({ level: 'ok', text: null });
  });
  it('warns when a long run averaged above 80%', () => {
    const v = breakerVerdict(90, [run(240, 45)], 50);
    expect(v.level).toBe('sustained');
    expect(v.text).toContain('4 hours averaged 90%');
  });
  it('only mentions a brief high minute when the runs show it did not last', () => {
    const v = breakerVerdict(92, [run(60, 46), run(200, 25)], 50);
    expect(v.level).toBe('brief');
    expect(v.text).toContain('only a load that lasts that long');
  });
  it('always warns above the breaker, and falls back to the highest minute when it knows no runs', () => {
    expect(breakerVerdict(120, [run(30, 10)], 30).level).toBe('over');
    expect(breakerVerdict(85, null, 50).level).toBe('sustained');
    expect(breakerVerdict(70, null, 50).level).toBe('ok');
    expect(breakerVerdict(70, [], 50).level).toBe('ok');
  });
});

describe('typical current', () => {
  it('weights runs by their length and skips ones with no current', () => {
    expect(typicalAmps([run(60, 10), run(180, 30), run(100, null)])).toBe(25);
    expect(typicalAmps([run(60, null)])).toBeNull();
    expect(typicalAmps([])).toBeNull();
  });
});
