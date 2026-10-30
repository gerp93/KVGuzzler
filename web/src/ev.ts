// Pure helpers for a charging meter's odometer page: the miles the car went between readings, over what the meter used. The server does the sums; this
// only words them.
import type { components } from './api-types';

export type MeterEv = components['schemas']['MeterEv'];
export type EvInterval = MeterEv['intervals'][number];
export type EvChargesAnswer = components['schemas']['EvCharges'];
export type EvCharge = EvChargesAnswer['charges'][number];

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

/** A moment as a short phrase: the time when it was today, else the day and the time. */
export function whenText(iso: string, now: Date = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const time = d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
  return d.toDateString() === now.toDateString() ? time : `${d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}, ${time}`;
}

/** Energy per percent of battery: three places under 1 kWh (0.488), two above. */
export const perPct = (kwhPerPct: number): string => (kwhPerPct < 1 ? kwhPerPct.toFixed(3) : kwhPerPct.toFixed(2));

/** A charge's battery column: the car's reports at the start and the end ("61 to 85 %"), or a dash when the start was never read. */
export function batteryText(c: Pick<EvCharge, 'start_pct' | 'end_pct'>): string {
  const p = (n: number) => String(Math.round(n * 10) / 10);
  if (c.start_pct == null) return '—';
  if (c.end_pct == null) return `${p(c.start_pct)} %`;
  return `${p(c.start_pct)} to ${p(c.end_pct)} %`;
}

/** What the energy-per-percent cell says when the charge has none: a word for the cell and the sentence behind it. Null when the charge has one. */
export function pctNote(status: EvCharge['pct_status']): { short: string; long: string } | null {
  switch (status) {
    case 'ok':
      return null;
    case 'active':
      return { short: 'Charging', long: 'Still charging: the battery gain is known once the charge has ended.' };
    case 'waiting':
      return { short: 'Waiting', long: "Waiting for the car to report its battery charge after the end of the charge." };
    case 'early':
      return { short: 'Not final', long: "The car's last report was from before the charge ended, so the gain is short by the last of it." };
    case 'small_gain':
      return { short: 'No gain', long: 'The battery gained under 2 %: preconditioning or a top-up, with nothing to divide by.' };
    default:
      return { short: '—', long: "The battery charge was not read when this charge began (the car is not linked, or did not answer)." };
  }
}

/** The energy in a gallon of gasoline by the EPA's reckoning, 33.705 kWh: miles per kWh times it is "miles per gallon equivalent". */
export const KWH_PER_GALLON_EQUIVALENT = 33.705;

/** Miles per gallon equivalent from miles per kWh at the wall (charging losses included). */
export const mpge = (milesPerKwh: number): number => milesPerKwh * KWH_PER_GALLON_EQUIVALENT;

/** The days a trend window covers. */
export const TREND_DAYS = 7;

/** Under this many miles in a window the efficiency is not worked out: a few miles over a day's charge says nothing about the car. */
export const MIN_TREND_MILES = 15;

export interface TrendPoint {
  /** The local calendar day the window ends on, `2026-10-26`. */
  day: string;
  /** Seconds since the epoch at noon UTC of that day, so every time zone shows the same date. */
  x: number;
  /** Miles over the kWh the meter used, over the window; null until the window is full, or with too few miles or no energy. */
  milesPerKwh: number | null;
  /** What the meter's energy cost over the window; null until the window is full, or when a day in it has no price. */
  windowCost: number | null;
}

const DAY_MS = 86_400_000;
const dayMs = (day: string): number => Date.parse(`${day}T00:00:00Z`);
const dayText = (ms: number): string => new Date(ms).toISOString().slice(0, 10);

/**
 * The car's efficiency and what charging cost, over a rolling window of days ending on each day. A stretch between two odometer readings is shared
 * evenly over its days (the odometer is only read now and then), and a window adds up the days in it, so a day that is mostly driving and a day that is
 * mostly charging come out as the week they are part of; a window that is not yet a full week has no figure. The last day is the day before the latest reading: a reading counts as taken at the start of
 * its day.
 */
export function efficiencyTrend(intervals: EvInterval[], window: number = TREND_DAYS): TrendPoint[] {
  type Day = { day: string; miles: number; kwh: number; cost: number; priced: boolean };
  const days: Day[] = [];
  for (const iv of [...intervals].sort((a, b) => a.from_day.localeCompare(b.from_day))) {
    const from = dayMs(iv.from_day);
    const n = Math.round((dayMs(iv.to_day) - from) / DAY_MS);
    if (!(n >= 1)) continue;
    const cost = iv.cost == null ? null : Number(iv.cost);
    for (let i = 0; i < n; i++) {
      days.push({ day: dayText(from + i * DAY_MS), miles: iv.miles / n, kwh: iv.kwh / n, cost: cost == null ? 0 : cost / n, priced: cost != null });
    }
  }
  return days.map((_, k) => {
    const span = days.slice(Math.max(0, k - window + 1), k + 1);
    const miles = span.reduce((s, d) => s + d.miles, 0);
    const kwh = span.reduce((s, d) => s + d.kwh, 0);
    const full = span.length === window && span.every((d) => d.priced);
    const day = days[k]!.day;
    return {
      day,
      x: dayMs(day) / 1000 + 43_200,
      milesPerKwh: span.length === window && miles >= MIN_TREND_MILES && kwh > 0 ? miles / kwh : null,
      windowCost: full ? span.reduce((s, d) => s + d.cost, 0) : null,
    };
  });
}

export type EvCompare = components['schemas']['EvCompare'];
export type EvSavings = components['schemas']['EvSavings'];

/** Seconds since the epoch at noon UTC of a calendar day, so every time zone shows the same date. */
const noon = (day: string): number => Date.parse(`${day}T00:00:00Z`) / 1000 + 43_200;

/** The savings so far, one point at each odometer reading that closes a stretch with savings (a stretch whose gas price or electric cost is unknown adds nothing). */
export function cumulativeSavings(stretches: EvSavings['stretches']): { x: number[]; y: number[] } {
  const x: number[] = [];
  const y: number[] = [];
  let sum = 0;
  for (const s of stretches) {
    if (s.savings == null) continue;
    sum += Number(s.savings);
    x.push(noon(s.to_day));
    y.push(sum);
  }
  return { x, y };
}

/** The weekly gas prices as a chart line: dollars per gallon at each week's Monday. */
export function gasPriceSeries(prices: EvSavings['prices']): { x: number[]; y: number[] } {
  return { x: prices.map((p) => noon(p.week)), y: prices.map((p) => Number(p.dollars)) };
}

/** A gas car's miles per gallon as typed: a number from 5 to 200, or null. */
export function parseMpg(text: string): number | null {
  const n = Number(text.trim().replace(/,/g, ''));
  return text.trim() !== '' && Number.isFinite(n) && n >= 5 && n <= 200 ? n : null;
}

export type EvBattery = components['schemas']['EvBattery'];

/** A battery's usable energy as typed: a number of kWh from 10 to 300, or null. */
export function parseKwh(text: string): number | null {
  const n = Number(text.trim().replace(/,/g, ''));
  return text.trim() !== '' && Number.isFinite(n) && n >= 10 && n <= 300 ? n : null;
}

/** A share (0 to 1) as a whole percent: "86 %". */
export const percentText = (share: number): string => `${Math.round(share * 100)} %`;
