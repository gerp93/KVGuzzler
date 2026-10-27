// Pure helpers for a charging meter's odometer page: the miles the car went between readings, over what the meter used. The server does the sums; this
// only words them.
import type { components } from './api-types';

export type MeterEv = components['schemas']['MeterEv'];
export type EvInterval = MeterEv['intervals'][number];

/** Below this share of a stretch measured, the energy (and so the cost per mile) is too low to trust. */
export const LOW_COVERAGE = 0.95;

export const lowCoverage = (coverage: number): boolean => coverage < LOW_COVERAGE;

/** "1 Oct", or "1 Oct 2025" when it is not this year. A day is a plain calendar date, so no time zone moves it. */
export function dayLabel(day: string, now: Date = new Date()): string {
  const d = new Date(`${day}T00:00:00Z`);
  if (Number.isNaN(d.getTime())) return day;
  const withYear = d.getUTCFullYear() !== now.getFullYear();
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', ...(withYear ? { year: 'numeric' } : {}), timeZone: 'UTC' });
}

/** "1 Oct to 26 Oct". */
export const stretchLabel = (i: Pick<EvInterval, 'from_day' | 'to_day'>, now: Date = new Date()): string => `${dayLabel(i.from_day, now)} to ${dayLabel(i.to_day, now)}`;

/** Today's date on this device as a plain calendar day, `2026-10-26`: what a new reading is dated unless the user picks another. */
export function today(now: Date = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())}`;
}

/** An odometer figure as typed: a non-negative number, or null for anything else. */
export function parseMiles(text: string): number | null {
  const n = Number(text.trim().replace(/,/g, ''));
  return text.trim() !== '' && Number.isFinite(n) && n >= 0 ? n : null;
}

/** Miles with thousands separators and no decimals above 1000, one decimal below. */
export function formatMiles(m: number): string {
  return `${Math.abs(m) >= 1000 ? Math.round(m).toLocaleString() : (Math.round(m * 10) / 10).toString()} mi`;
}

/** Miles per kWh, one decimal. */
export const formatEfficiency = (v: number | null | undefined): string => (v == null ? '—' : `${v.toFixed(1)} mi/kWh`);
