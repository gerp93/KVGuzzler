// Pure helpers for currents and breakers. A breaker may carry a steady load only up to 80% of its size (the continuous-load rule: a load that runs for
// three hours or more); above that it is "high", and above 100% it is over. A 240 V pair is one breaker with the same current in both legs, so its current is
// the larger leg's and is never added (the server does that; nothing here adds currents).

/** The usual breaker sizes of a US panel, offered when one is set. */
export const BREAKER_SIZES = [15, 20, 25, 30, 40, 50, 60, 70, 80, 90, 100, 125, 150, 200];

/** The share of a breaker a current is, as a whole percent; null when either is missing or the breaker is not a real size. */
export function breakerPercent(amps: number | null | undefined, breaker: number | null | undefined): number | null {
  if (amps == null || breaker == null || !(breaker > 0)) return null;
  return Math.round((amps / breaker) * 100);
}

export type BreakerLevel = 'ok' | 'high' | 'over';

/** Below 80% is fine, 80% to 100% is above what a steady load should draw, above 100% is over the breaker. */
export function breakerLevel(percent: number | null): BreakerLevel | null {
  if (percent == null) return null;
  if (percent > 100) return 'over';
  return percent >= 80 ? 'high' : 'ok';
}

/** "31.6 A", or "125 A" once it is large. */
export function formatAmps(a: number | null | undefined): string {
  if (a == null) return '—';
  return `${Math.abs(a) >= 100 ? Math.round(a) : a.toFixed(1)} A`;
}

export function formatVolts(v: number | null | undefined): string {
  return v == null ? '—' : `${Math.round(v)} V`;
}

/** The sentence a warning says, or null when there is nothing to warn about. `peak` is the highest percent seen in the period. */
export function breakerAdvice(peak: number | null, breaker: number | null | undefined): string | null {
  const level = breakerLevel(peak);
  if (level === 'high') {
    return `It reached ${peak}% of its ${breaker} A breaker. A load that runs for 3 hours or more should stay at or below 80% of the breaker.`;
  }
  if (level === 'over') {
    return `It reached ${peak}% of its ${breaker} A breaker, which would trip if that lasted. Check that the breaker size is right and that the clamp reads this breaker alone.`;
  }
  return null;
}

/** The sizes to choose from: the usual ones, and the one already set if it is not among them. */
export function breakerOptions(current: number | null | undefined): number[] {
  const sizes = current != null && !BREAKER_SIZES.includes(current) ? [...BREAKER_SIZES, current] : BREAKER_SIZES;
  return [...sizes].sort((a, b) => a - b);
}
