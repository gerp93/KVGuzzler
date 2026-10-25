// Pure helpers for the "Unusual days" card: the days the house, or a meter, used far more than it usually does. The server finds them; this words them.
import type { components } from './api-types';
import { formatKwh, formatMoney } from './lib';

export type Anomalies = components['schemas']['Anomalies'];
export type AnomalyDay = components['schemas']['AnomalyDay'];

/** A local day as "Wed 5 Nov" (the date is a plain day, so it is read as one, never moved by a time zone). */
export function dayLabel(date: string): string {
  const [y, m, d] = date.split('-').map(Number);
  const when = new Date(y!, (m ?? 1) - 1, d);
  return `${when.toLocaleDateString([], { weekday: 'short' })} ${when.getDate()} ${when.toLocaleDateString([], { month: 'short' })}`;
}

/** "50 kWh instead of the usual 20: 30 kWh more, about $6.00". */
export function dayLine(day: Pick<AnomalyDay, 'kwh' | 'usual_kwh' | 'extra_kwh' | 'extra_cost'>): string {
  const cost = day.extra_cost != null ? `, about ${formatMoney(day.extra_cost)}` : '';
  return `${formatKwh(day.kwh)} instead of the usual ${formatKwh(day.usual_kwh)}: ${formatKwh(day.extra_kwh)} more${cost}`;
}

/** "Mostly EV charger (+30 kWh), Workshop (+3.1 kWh)"; empty when nothing inside it was unusual itself. */
export function causesLine(causes: AnomalyDay['causes']): string {
  if (causes.length === 0) return '';
  return `Mostly ${causes.map((c) => `${c.label} (+${formatKwh(c.extra_kwh)})`).join(', ')}`;
}

/** The days to show first, and how many are held back behind "Show all". */
export function firstDays<T>(days: T[], shown: number): { list: T[]; more: number } {
  return { list: days.slice(0, shown), more: Math.max(0, days.length - shown) };
}
