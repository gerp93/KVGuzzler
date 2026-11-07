import { describe, expect, it } from 'vitest';
import { ago, bannerHeadline, changeKwh, changeText, digestNotes, digestTitle, patternLines, periodLabel, ruleSummary, severityRank, type Alert, type AlertPattern, type DigestBody } from './alerts';

const alert = (over: Partial<Alert>): Alert => ({
  id: 1,
  rule_id: 'r',
  severity: 'warn',
  title: 't',
  detail: '',
  opened_at: '2026-11-07T10:00:00Z',
  updated_at: '2026-11-07T10:00:00Z',
  cleared_at: null,
  acked_at: null,
  active: true,
  ...over,
});

describe('alert text', () => {
  it('ranks severities', () => {
    expect(severityRank('urgent')).toBeGreaterThan(severityRank('warn'));
    expect(severityRank('warn')).toBeGreaterThan(severityRank('info'));
  });

  it('says how long ago', () => {
    const now = new Date('2026-11-07T12:00:00Z');
    expect(ago('2026-11-07T11:59:40Z', now)).toBe('just now');
    expect(ago('2026-11-07T11:20:00Z', now)).toBe('40 min ago');
    expect(ago('2026-11-07T08:50:00Z', now)).toBe('3 h 10 min ago');
  });

  it('puts the worst, then the newest, alert first on the banner', () => {
    const list = [alert({ id: 1, severity: 'info' }), alert({ id: 2, severity: 'urgent', opened_at: '2026-11-07T09:00:00Z' }), alert({ id: 3, severity: 'urgent', opened_at: '2026-11-07T11:00:00Z' })];
    const h = bannerHeadline(list);
    expect(h.first?.id).toBe(3);
    expect(h.more).toBe(2);
    expect(bannerHeadline([])).toEqual({ first: undefined, more: 0 });
  });

  it('describes the owner\'s rules', () => {
    expect(ruleSummary({ kind: 'level', params: { op: 'above', watts: 5, minutes: 90 } })).toBe('Above 5 W for 1 h 30 min');
    expect(ruleSummary({ kind: 'level', params: { op: 'below', watts: 0.5, minutes: 30 } })).toBe('Below 0.5 W for 30 min');
    expect(ruleSummary({ kind: 'quiet', params: { watts: 50, hours: 72 } })).toBe('Nothing above 50 W for 3.0 days');
    expect(ruleSummary({ kind: 'long', params: { watts: 800, minutes: 20 } })).toBe('Working above 800 W for more than 20 min');
    expect(ruleSummary({ kind: 'pattern', params: { watch: { overdue: true, rest: true } } })).toBe(
      'Usual pattern: has not run for longer than usual; draws a different amount between runs',
    );
  });

  const pattern: AlertPattern = {
    ok: true,
    level_watts: 334.4,
    suggested: true,
    rest_watts: 1.94,
    runs: 5,
    days: 10.6,
    gap_median_hours: 42,
    gap_max_hours: 58.7,
    overdue_after_hours: 73.4,
    run_median_minutes: 4,
    run_max_minutes: 5,
    peak_median_watts: 841.2,
    most_per_day: 1,
    last_run_at: '2026-11-04T12:22:00Z',
    hours_since_last_run: 65.8,
  };

  it('says what was learned', () => {
    const lines = patternLines(pattern);
    expect(lines[0]).toBe('Rests at about 1.9 W. Counts as working above 334 W (picked from its own readings).');
    expect(lines[1]).toBe('5 runs in 11 days: about every 42 h, the longest wait 2.4 days.');
    expect(lines).toContain('A wait longer than 3.1 days would raise an alert.');
    expect(lines).toContain('It last ran 2.7 days ago.');
  });

  it('says why there is no pattern', () => {
    expect(patternLines({ ...pattern, ok: false, why: 'no_spikes' })[0]).toMatch(/does not rise/);
    expect(patternLines({ ...pattern, ok: false, why: 'too_few_runs', runs: 3 })[0]).toBe('It has worked 3 times in the days looked at. A pattern needs at least 4.');
    expect(patternLines({ ...pattern, ok: false, why: 'no_data' })[0]).toMatch(/Not enough/);
  });
});

describe('summaries', () => {
  const body: DigestBody = {
    kind: 'weekly',
    from: '2026-11-02',
    to: '2026-11-08',
    kwh: 168,
    cost: '25.20',
    cost_estimated: false,
    prev_kwh: 160,
    prev_cost: '24.00',
    change_share: 0.05,
    coverage: 0.8,
    top: [],
    biggest_rise: null,
    base_watts: 130,
    base_prev_watts: 120,
    outages: { power_seconds: 5400, server_seconds: 0, unknown_seconds: 0 },
    unusual_days: 2,
    alerts: { info: 1, warn: 1, urgent: 1 },
  };

  it('labels a period', () => {
    expect(periodLabel('2026-11-02', '2026-11-08')).toMatch(/Nov 2 to Nov 8/);
    expect(periodLabel('2026-11-08', '2026-11-08')).toMatch(/Nov 8/);
    expect(digestTitle({ kind: 'weekly', from: '2026-11-02', to: '2026-11-08' })).toMatch(/^Week of Nov 2 to Nov 8/);
    expect(digestTitle({ kind: 'daily', from: '2026-11-08', to: '2026-11-08' })).toMatch(/^Nov 8/);
  });

  it('compares with the period before', () => {
    expect(changeText(body)).toBe('+5.0% on the week before');
    expect(changeText({ ...body, change_share: -0.2, kind: 'daily' })).toBe('-20% on the same day last week');
    expect(changeText({ ...body, change_share: 0.001 })).toBe('About the same as the week before');
    expect(changeText({ ...body, change_share: null })).toBeUndefined();
  });

  it('lists what is worth knowing', () => {
    expect(digestNotes(body)).toEqual([
      'The house was recorded for 80% of the period.',
      'Outages: power out 1 h 30 min.',
      'Always-on power 130 W (120 W the period before).',
      '2 unusual days.',
      '3 alerts raised, 1 urgent.',
    ]);
    expect(digestNotes({ ...body, coverage: 1, outages: { power_seconds: 0, server_seconds: 0, unknown_seconds: 0 }, base_watts: null, unusual_days: 0, alerts: { info: 0, warn: 0, urgent: 0 } })).toEqual([]);
  });

  it('signs a change in energy', () => {
    expect(changeKwh({ change_kwh: 1.234 })).toBe('+1.23 kWh');
    expect(changeKwh({ change_kwh: -0.5 })).toBe('-0.50 kWh');
  });
});
