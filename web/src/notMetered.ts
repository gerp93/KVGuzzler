// The "Everything else" group of the Live page's circuits table: what the house carries beyond its clamped circuits, with the plugs and computers
// that sit on circuits nothing clamps listed inside it (that is where they are: in the house, on none of the clamped circuits). The group's own line is the
// house's figure less the circuits' (the server's `unmetered_kwh`), so the plugs under it are already in it and are never added to it; what no plug explains is
// its last line. This is the same tree the Hierarchy tab draws, with the house as the parent.
import type { Meter, Row, Summary } from './lib';

/** A line of the table that stands for no meter of its own: a figure worked out from others. */
export interface Lump {
  label: string;
  kwh: number | null;
  cost: string | null;
}

export type BarRow = Row & { lump?: Lump };

/** The money left after taking some of it away; null unless every figure is known. */
function moneyLeft(whole: string | null | undefined, parts: (string | null | undefined)[]): string | null {
  if (whole == null || parts.some((p) => p == null)) return null;
  return Math.max(0, Number(whole) - parts.reduce((t, p) => t + Number(p), 0)).toFixed(2);
}

/** Plugs and computers that name no circuit they are on, as the rows to list inside the group: the top-level plugs of `circuits` (nestCircuits' output) and
 * the PC wall plugs that were not placed under a circuit. Biggest first. */
export function unclamped(circuits: Row[], pc: Row[]): Row[] {
  const plugs = circuits.filter((r) => r.meter.scope === 'plug' && !r.remainder && (r.depth ?? 0) === 0);
  return [...plugs, ...pc].sort((a, b) => b.watts - a.watts);
}

/** The group's lines: its own, the unclamped plugs indented under it, then (only when it says something) what none of them explains.
 * `house` is the house's power now and `host` any meter, used only to carry the lines (they are drawn from `lump`, never from the meter). */
export function notMeteredRows(args: { house: number; clamped: Row[]; plugs: Row[]; summary: Summary | undefined; host: Meter }): BarRow[] {
  const { house, clamped, plugs, summary, host } = args;
  const top = clamped.filter((r) => r.meter.scope === 'circuit' && !r.remainder && (r.depth ?? 0) === 0);
  const watts = Math.max(0, house - top.reduce((t, r) => t + r.watts, 0));
  const figures = new Map((summary?.meters ?? []).map((f) => [f.meter_id, f]));
  // The house less its circuits cannot be negative: when the circuits used more than the house did, the house clamps were not recording for all the time the circuits were
  // (as over "all time" with a monitor added later), and what is left is not known. Saying 0.00 kWh for something drawing power now would be untrue, so it says nothing.
  const circuitsKwh = top.reduce((t, r) => t + (figures.get(r.meter.id)?.kwh ?? 0), 0);
  const known = summary?.house_kwh == null || circuitsKwh <= summary.house_kwh;
  const kwh = known ? (summary?.unmetered_kwh ?? null) : null;
  const cost = known ? moneyLeft(summary?.house_cost, top.map((r) => figures.get(r.meter.id)?.cost)) : null;
  const out: BarRow[] = [{ meter: host, watts, depth: 0, lump: { label: 'Everything else', kwh, cost } }];
  if (plugs.length === 0) return out;
  for (const p of plugs) out.push({ ...p, depth: 1 });
  const inside = plugs.map((p) => figures.get(p.meter.id));
  const leftKwh = kwh == null || inside.some((f) => f == null) ? null : Math.max(0, kwh - inside.reduce((t, f) => t + f!.kwh, 0));
  const leftWatts = Math.max(0, watts - plugs.reduce((t, p) => t + p.watts, 0));
  if (leftWatts >= 1 || (leftKwh ?? 0) > 0) {
    out.push({
      meter: host,
      watts: leftWatts,
      depth: 1,
      lump: { label: 'Not individually metered', kwh: leftKwh, cost: kwh == null ? null : moneyLeft(cost, inside.map((f) => f?.cost)) },
    });
  }
  return out;
}
