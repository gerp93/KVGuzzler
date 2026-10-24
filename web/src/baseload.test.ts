import { describe, expect, it } from 'vitest';
import { baseLoadSentence, lookedAt, shareText, yearKwh, type BaseLoad } from './baseload';

const b = (over: Partial<BaseLoad> = {}): BaseLoad => ({
  scope: 'house', watts: 300, avg_watts: 1200, share: 0.25, minutes: 10080, kwh_per_year: 2629.8, cost_per_year: '414.00', rate: '0.1580', rate_estimated: false,
  from: '2026-10-17T00:00:00Z', to: '2026-10-24T00:00:00Z', ...over,
});

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
  it('says who it is about, and leaves out a share of nothing', () => {
    expect(baseLoadSentence(b(), 'this meter')).toContain('The house hardly ever');
    expect(baseLoadSentence(b(), 'this meter')).toContain('25% of what the house uses');
    expect(baseLoadSentence(b({ scope: 'meter', share: 0 }), 'this meter')).not.toContain('That is');
  });
  it('says how much was looked at', () => {
    expect(lookedAt(90)).toBe('90 min');
    expect(lookedAt(600)).toBe('10 hours');
    expect(lookedAt(10080)).toBe('7 days');
  });
});
