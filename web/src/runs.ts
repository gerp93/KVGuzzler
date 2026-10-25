// Pure helpers for a meter's "Runs": the times it was working (a car charging, a dryer drying). The server finds them; this only words and sizes them.
import type { components } from './api-types';

export type MeterRuns = components['schemas']['MeterRuns'];
export type MeterRun = components['schemas']['MeterRun'];

export interface RunStats {
  count: number;
  avgKwh: number;
  /** The cost of an average run; null unless every run has a price. */
  avgCost: number | null;
  avgMinutes: number;
  longestMinutes: number;
  /** The share (whole percent) of the meter's energy in the period that happened during a run; null without any energy. */
  shareOfMeter: number | null;
}

/** The summary line's figures. Null when the meter has no level to run above (it barely changes) or no run in the period. */
export function runStats(r: Pick<MeterRuns, 'threshold_watts' | 'totals' | 'meter_kwh'>): RunStats | null {
  const t = r.totals;
  if (r.threshold_watts == null || t.count === 0) return null;
  return {
    count: t.count,
    avgKwh: t.kwh / t.count,
    avgCost: t.cost != null ? Number(t.cost) / t.count : null,
    avgMinutes: t.minutes / t.count,
    longestMinutes: t.longest_minutes,
    shareOfMeter: r.meter_kwh > 0 ? Math.min(100, Math.round((t.kwh / r.meter_kwh) * 100)) : null,
  };
}

/** Whether the card is worth showing: a meter that barely changes has no runs to speak of. */
export function showRuns(r: Pick<MeterRuns, 'threshold_watts'> | undefined): boolean {
  return r != null && r.threshold_watts != null;
}

/** Each run's bar as a percent of the biggest run's energy (a visible sliver at least), in the order given. */
export function runBars(list: Pick<MeterRun, 'kwh'>[]): number[] {
  const top = Math.max(0, ...list.map((r) => r.kwh));
  return list.map((r) => (top > 0 ? Math.max(3, Math.round((r.kwh / top) * 100)) : 0));
}

/** "2 h 15 min", "45 min": a run's length. */
export function runLength(minutes: number): string {
  const m = Math.round(minutes);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  return m % 60 === 0 ? `${h} h` : `${h} h ${m % 60} min`;
}

/** When a run began, short: "Fri 6:00 PM", with the date once it is older than a week. */
export function runWhen(startIso: string, now: Date): string {
  const d = new Date(startIso);
  const time = d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const ageDays = (now.getTime() - d.getTime()) / 86_400_000;
  if (ageDays < 7) return `${d.toLocaleDateString([], { weekday: 'short' })} ${time}`;
  return `${d.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${time}`;
}
