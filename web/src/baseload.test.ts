import { describe, expect, it } from 'vitest';
import { lookedAt, shareText, yearKwh } from './baseload';

describe('always-on wording', () => {
  it('rounds the share and never shows a sliver as nothing', () => {
    expect(shareText(0.256)).toBe('26%');
    expect(shareText(0.004)).toBe('under 1%');
    expect(shareText(0)).toBe('0%');
  });
  it('writes a year in kWh', () => {
    expect(yearKwh(26.298)).toBe('26.3 kWh');
    expect(/^2.?630 kWh$/.test(yearKwh(2629.8))).toBe(true);
  });
  it('says how much was looked at', () => {
    expect(lookedAt(90)).toBe('90 min');
    expect(lookedAt(600)).toBe('10 hours');
    expect(lookedAt(10080)).toBe('7 days');
  });
});
