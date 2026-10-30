import { describe, expect, it } from 'vitest';
import { collectorState } from './collector';

const now = Date.parse('2026-10-30T22:00:00Z');

describe('whether a PC collector is reporting', () => {
  it('is reporting when it was heard from a moment ago', () => {
    expect(collectorState('2026-10-30T21:59:57Z', now)).toEqual({ state: 'reporting', ageSeconds: 3 });
  });

  it('is silent once it has not been heard from for more than 30 seconds', () => {
    expect(collectorState('2026-10-30T21:59:30Z', now).state).toBe('reporting');
    expect(collectorState('2026-10-30T21:59:29Z', now)).toEqual({ state: 'silent', ageSeconds: 31 });
  });

  it('has never reported without a time', () => {
    expect(collectorState(null, now).state).toBe('never');
    expect(collectorState(undefined, now).state).toBe('never');
  });

  it('does not go negative when the PC clock runs ahead', () => {
    expect(collectorState('2026-10-30T22:00:05Z', now)).toEqual({ state: 'reporting', ageSeconds: 0 });
  });
});
