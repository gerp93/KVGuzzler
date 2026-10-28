// Pure helpers for the "Always on" figure: the power a meter (or the house) hardly ever goes below.
import type { components } from './api-types';

export type BaseLoad = components['schemas']['BaseLoad'];

/** The share of use that is always on, as a whole percent; "under 1%" for a sliver, so it never reads as nothing at all. */
export function shareText(share: number): string {
  const pct = share * 100;
  if (pct <= 0) return '0%';
  if (pct < 1) return 'under 1%';
  return `${Math.round(pct)}%`;
}

/** A year of it in kWh, whole numbers once it is large. */
export function yearKwh(kwh: number): string {
  return kwh >= 100 ? `${Math.round(kwh).toLocaleString()} kWh` : `${kwh.toFixed(1)} kWh`;
}

/** How long the figure has been looked at, so a reader knows how much to trust it ("1 day"). */
export function lookedAt(minutes: number): string {
  if (minutes < 120) return `${minutes} min`;
  const h = minutes / 60;
  return h < 48 ? `${Math.round(h)} hours` : `${Math.round(h / 24)} days`;
}
