// Pure helpers for currents and breakers. A breaker may carry a steady load only up to 80% of its size (the continuous-load rule: a load that runs for
// three hours or more); above that it is "high", and above 100% it is over. A 240 V pair is one breaker with the same current in both legs, so its current is
// the larger leg's and is never added (the server does that; nothing here adds currents).

/** The usual breaker sizes of a US panel, offered when one is set. */
export const BREAKER_SIZES = [15, 20, 25, 30, 40, 50, 60, 70, 80, 90, 100, 125, 150, 200];

/** The sizes a whole house's main breaker comes in, beyond a circuit's. */
export const MAIN_SIZES = [225, 250, 300, 400];

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

/** "3.7 / 50 A": the current out of what its breaker allows, when the breaker's size is set; otherwise just the current. */
export function formatAmpsOf(a: number | null | undefined, breaker: number | null | undefined): string {
  if (a == null || breaker == null || !(breaker > 0)) return formatAmps(a);
  return `${Math.abs(a) >= 100 ? Math.round(a) : a.toFixed(1)} / ${breaker} A`;
}

export function formatVolts(v: number | null | undefined): string {
  return v == null ? '—' : `${Math.round(v)} V`;
}

/** The sentence a warning says, or null when there is nothing to warn about. `peak` is the highest percent seen in the period. */
export function breakerAdvice(peak: number | null, breaker: number | null | undefined): string | null {
  const level = breakerLevel(peak);
  if (level === 'high') {
    return `Reached ${peak}% of the ${breaker} A breaker. A load running 3 h or more should stay at or below 80%.`;
  }
  if (level === 'over') {
    return `Reached ${peak}% of the ${breaker} A breaker, which would trip if that lasted. Check the breaker size and that the clamp reads this breaker alone.`;
  }
  return null;
}

/** The sizes to choose from: the usual ones (with the bigger ones of a main breaker when `main`), and the one already set if it is not among them. */
export function breakerOptions(current: number | null | undefined, main = false): number[] {
  const usual = main ? [...BREAKER_SIZES, ...MAIN_SIZES] : BREAKER_SIZES;
  const sizes = current != null && !usual.includes(current) ? [...usual, current] : usual;
  return [...sizes].sort((a, b) => a - b);
}

/** A load that runs this long (minutes) has to stay at or below 80% of its breaker. */
export const SUSTAINED_MINUTES = 180;

export interface RunCurrent {
  start: string;
  minutes: number;
  avg_amps?: number | null | undefined;
}

/** The run that most strongly breaks the continuous-load rule: one of 3 hours or more whose average current was 80% of the breaker or more. */
export function sustainedHigh(runs: RunCurrent[], breaker: number | null | undefined): { run: RunCurrent; percent: number } | null {
  let worst: { run: RunCurrent; percent: number } | null = null;
  for (const run of runs) {
    const pct = run.minutes >= SUSTAINED_MINUTES ? breakerPercent(run.avg_amps, breaker) : null;
    if (pct !== null && pct >= 80 && (worst === null || pct > worst.percent)) worst = { run, percent: pct };
  }
  return worst;
}

export type VerdictLevel = 'ok' | 'brief' | 'sustained' | 'over';

/**
 * What to say about a circuit's breaker. The rule is about a load that keeps running (3 hours or more), so with the meter's runs known a single
 * high minute is only mentioned, not warned about; without them (a meter that does not run in stretches) the highest minute is all there is.
 */
export function breakerVerdict(peakPct: number | null, runs: RunCurrent[] | null, breaker: number | null | undefined): { level: VerdictLevel; text: string | null } {
  if (peakPct === null) return { level: 'ok', text: null };
  if (peakPct > 100) return { level: 'over', text: breakerAdvice(peakPct, breaker) };
  if (runs === null) {
    const advice = breakerAdvice(peakPct, breaker);
    return { level: advice ? 'sustained' : 'ok', text: advice };
  }
  const long = sustainedHigh(runs, breaker);
  if (long) {
    const hours = Math.round((long.run.minutes / 60) * 10) / 10;
    return {
      level: 'sustained',
      text: `A ${hours} h run averaged ${long.percent}% of the ${breaker} A breaker. A load running 3 h or more should stay at or below 80%.`,
    };
  }
  if (peakPct >= 80) {
    return { level: 'brief', text: `Peaked at ${peakPct}% of the ${breaker} A breaker; no run of 3 h or more averaged above 80%.` };
  }
  return { level: 'ok', text: null };
}

/** The usual current of a meter's runs, in amps: the runs' averages weighted by their length; null when none reports one. */
export function typicalAmps(runs: RunCurrent[]): number | null {
  let weighted = 0;
  let minutes = 0;
  for (const r of runs) {
    if (r.avg_amps != null) {
      weighted += r.avg_amps * r.minutes;
      minutes += r.minutes;
    }
  }
  return minutes > 0 ? weighted / minutes : null;
}
