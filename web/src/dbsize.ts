// The database's size over time, as the Database card shows it: one line a day (the last size noted that day) and how much it changed since the day before.
import type { components } from './api-types';

export type DatabaseGrowth = components['schemas']['DatabaseGrowth'];

export interface DaySize {
  /** The local day, like 2026-11-01. */
  day: string;
  bytes: number;
  /** Bytes gained since the day before's size; null for the first day listed. */
  change: number | null;
}

const dayOf = (iso: string): string => {
  const d = new Date(iso);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};

/** The size at the end of each day that has a reading, newest first, each with what it gained over the day listed before it (older). */
export function dailySizes(points: DatabaseGrowth['points']): DaySize[] {
  const last = new Map<string, number>();
  for (const p of [...points].sort((a, b) => a.at.localeCompare(b.at))) last.set(dayOf(p.at), p.bytes);
  const days = [...last.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  return days.map(([day, bytes], i) => ({ day, bytes, change: i === 0 ? null : bytes - days[i - 1]![1] })).reverse();
}
