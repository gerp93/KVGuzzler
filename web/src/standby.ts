// The standby report: what each appliance draws when it is doing nothing, against the stretch before. The server does the sums; this says them in words.
import type { components } from './api-types';
import { formatMoney, formatWatts } from './lib';

export type Standby = components['schemas']['Standby'];
export type StandbyPart = components['schemas']['StandbyPart'];

/** The lengths of stretch to compare, in days. */
export const STANDBY_DAYS = [14, 30, 60, 90] as const;

const shortDay = (iso: string): string => new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });

/** "Oct 25 to Nov 4": the stretch a floor is taken over. The end is the last day in it, not the instant after. */
export function stretchLabel(from: string, to: string): string {
  const last = new Date(Date.parse(to) - 1000).toISOString();
  return `${shortDay(from)} to ${shortDay(last)}`;
}

/** A floor in watts, with a decimal under 10 W (a floor of 1.9 W is not "2 W"). */
export function floorText(w: number): string {
  const a = Math.abs(w);
  return a >= 1000 ? `${(a / 1000).toFixed(2)} kW` : a >= 10 ? `${Math.round(a)} W` : `${a.toFixed(1)} W`;
}

/** How a floor moved: "-39.8 W", "+12 W", or "no change"; empty with no earlier floor to compare with. */
export function changeText(p: Pick<StandbyPart, 'change_watts'>): string {
  if (p.change_watts == null) return '';
  if (p.change_watts === 0) return 'no change';
  return `${p.change_watts > 0 ? '+' : '-'}${floorText(p.change_watts)}`;
}

/** What the change comes to in a year, for a row that moved: "-$34.91 a year". Empty when nothing moved or nothing can be priced. */
export function changeYearText(p: Pick<StandbyPart, 'change_watts' | 'change_cost_per_year'>): string {
  if (!p.change_watts || p.change_cost_per_year == null) return '';
  const n = Number(p.change_cost_per_year);
  if (!Number.isFinite(n) || n === 0) return '';
  return `${n > 0 ? '+' : '-'}${formatMoney(String(Math.abs(n)))} a year`;
}

/** The house's own floor against the stretch before, in a sentence. Null when there is none. */
export function houseLine(s: Pick<Standby, 'house_watts' | 'house_prev_watts'>): string | null {
  if (s.house_watts == null) return null;
  const now = formatWatts(s.house_watts);
  return s.house_prev_watts != null ? `The house never goes below ${now} (${formatWatts(s.house_prev_watts)} before).` : `The house never goes below ${now}.`;
}
