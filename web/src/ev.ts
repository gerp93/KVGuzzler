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

/** The unit distances are shown in. The server keeps and sums miles; this only changes how they are worded and typed. */
export type DistanceUnit = 'miles' | 'km';

export const KM_PER_MILE = 1.609344;

/** A distance in miles as the number to show in the unit. */
export const inUnit = (miles: number, unit: DistanceUnit): number => (unit === 'km' ? miles * KM_PER_MILE : miles);

/** A distance typed in the unit, as miles. */
export const toMiles = (typed: number, unit: DistanceUnit): number => (unit === 'km' ? typed / KM_PER_MILE : typed);

export const unitAbbreviation = (unit: DistanceUnit): string => (unit === 'km' ? 'km' : 'mi');

/** A distance (given in miles) with thousands separators and no decimals above 1000, one decimal below. */
export function formatDistance(miles: number, unit: DistanceUnit = 'miles'): string {
  const d = inUnit(miles, unit);
  return `${Math.abs(d) >= 1000 ? Math.round(d).toLocaleString() : (Math.round(d * 10) / 10).toString()} ${unitAbbreviation(unit)}`;
}

/** Miles with thousands separators and no decimals above 1000, one decimal below. */
export const formatMiles = (m: number): string => formatDistance(m, 'miles');

/** Energy per distance (given as miles per kWh), one decimal. */
export const formatEfficiency = (v: number | null | undefined, unit: DistanceUnit = 'miles'): string =>
  v == null ? '—' : `${inUnit(v, unit).toFixed(1)} ${unitAbbreviation(unit)}/kWh`;
