// Checking a bill against what the house recorded over the same days. The server does the sums; this says them in words.
import type { components } from './api-types';
import { formatDuration } from './lib';

export type BillCheck = components['schemas']['BillCheck'];
export type BillReconciliation = components['schemas']['BillReconciliation'];

export function verdictLabel(v: BillCheck['verdict']): string {
  return v === 'match' ? 'Matches' : v === 'close' ? 'Close' : v === 'differs' ? 'Differs' : '—';
}

/** A share as a signed percent: -4.0%, +0.8%. */
export function shareText(share: number | null | undefined): string {
  if (share == null || !Number.isFinite(share)) return '—';
  const pct = share * 100;
  return `${pct > 0 ? '+' : ''}${pct.toFixed(Math.abs(pct) < 10 ? 1 : 0)}%`;
}

/** Below this share of a period recorded, the energy is judged on what was recorded only and the gap is said. */
export const FULL_COVERAGE = 0.95;

/** What the recorded energy would be if the unrecorded part of the period had used power at the same rate as the rest (an estimate), with its difference from the bill.
 * Only for a period recorded at least half the time; null when it is fully recorded or too little was recorded to say. */
export function scaledToPeriod(c: BillCheck): { kwh: number; share: number } | null {
  if (c.recorded_kwh == null || c.coverage == null || c.in_progress) return null;
  if (c.coverage >= FULL_COVERAGE || c.coverage < 0.5) return null;
  const kwh = c.recorded_kwh / c.coverage;
  return { kwh, share: (kwh - c.billed_kwh) / c.billed_kwh };
}

/** Short things worth knowing about one check, in the order they matter. */
export function hints(c: BillCheck): string[] {
  const out: string[] = [];
  if (c.in_progress) out.push('Not over yet.');
  if (c.recorded_kwh == null) {
    out.push('Nothing recorded for these days.');
    return out;
  }
  if (c.coverage != null && c.coverage < FULL_COVERAGE) {
    out.push(`Recorded for ${Math.round(c.coverage * 100)}% of the period.`);
    const scaled = scaledToPeriod(c);
    if (scaled) out.push(`At the same rate over all of it: ${scaled.kwh.toFixed(0)} kWh (${shareText(scaled.share)}).`);
  }
  const o = c.outages;
  const parts = [
    o.power_seconds > 0 ? `power out ${formatDuration(o.power_seconds)}` : '',
    o.server_seconds > 0 ? `server off ${formatDuration(o.server_seconds)}` : '',
    o.unknown_seconds > 0 ? `not recorded ${formatDuration(o.unknown_seconds)}` : '',
  ].filter(Boolean);
  if (parts.length > 0) out.push(`In the outage log: ${parts.join(', ')}.`);
  return out;
}
