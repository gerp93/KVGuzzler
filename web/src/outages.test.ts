import { describe, expect, it } from 'vitest';
import { fromLocalInput, kindLabel, toLocalInput, whenText } from './outages';

describe('outage times', () => {
  it('goes from an instant to an input value and back, to the minute', () => {
    const iso = '2026-11-02T19:14:00.000Z';
    expect(fromLocalInput(toLocalInput(iso))).toBe(iso);
  });

  it('reads nothing from an empty or half-typed value', () => {
    for (const v of ['', '2026-11-02', '2026-11-02T', 'soon', '2026-13-45T99:99']) expect(fromLocalInput(v)).toBeNull();
  });

  it('shows the end with its day only when it is another day', () => {
    const sameDay = whenText({
      started_at: '2026-11-02T19:14:00Z',
      ended_at: '2026-11-02T19:47:00Z',
    });
    expect(sameDay.split('–')).toHaveLength(2);
    expect(sameDay.split('–')[1]).not.toMatch(/Nov|Dec|Oct/);
    // 30 hours later is always another day, whatever the viewer's zone.
    const otherDay = whenText({
      started_at: '2026-11-02T19:14:00Z',
      ended_at: '2026-11-04T01:14:00Z',
    });
    expect(otherDay.split('–')[1]).toMatch(/\d/);
    expect(otherDay.split('–')[1]).toMatch(/Nov/);
  });

  it('names the kinds in plain words', () => {
    expect(kindLabel('power')).toBe('Power outage');
    expect(kindLabel('server')).toBe('Server off');
    expect(kindLabel('unknown')).toBe('Not recorded');
  });
});
