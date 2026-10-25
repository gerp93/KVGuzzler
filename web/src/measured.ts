// "Everything measured": what the dashboard can add up when there is no whole-house clamp. The house is the mains clamps alone; without them nothing
// says what the whole house used, but the meters at the top of the tree (circuits that are on no sub-panel, and plugs or computers that name no
// circuit they are on) are separate things, so they can be added to one another. Nothing inside a meter is added to it: its reading already holds
// what is inside it.
import { shownMeters, sumMoney, type Meter, type Reading, type Summary } from './lib';

/** The meters at the top of the tree: circuits, plugs and computers that are inside no other shown meter. */
export function topLevelMeters(meters: Meter[]): Meter[] {
  const shown = shownMeters(meters);
  const ids = new Set(shown.map((m) => m.id));
  return shown.filter((m) => (m.scope === 'circuit' || m.scope === 'plug' || m.scope === 'pc') && (!m.parent_id || !ids.has(m.parent_id)));
}

export interface Measured {
  /** Power right now; null when none of them is reporting. */
  watts: number | null;
  kwh: number | null;
  cost: string | null;
  /** The highest draws added: an upper bound, as the meters need not peak in the same second. */
  peakWatts: number | null;
  /** How many meters it adds up. */
  count: number;
}

/** The top-level meters added up over a period and right now. Each figure is null when none of the meters has it. */
export function measuredTotals(meters: Meter[], summary: Summary | undefined, readings: Pick<Reading, 'meter_id' | 'watts'>[]): Measured {
  const top = topLevelMeters(meters);
  const ids = new Set(top.map((m) => m.id));
  const figures = (summary?.meters ?? []).filter((f) => ids.has(f.meter_id));
  const now = readings.filter((r) => ids.has(r.meter_id));
  const peaks = figures.flatMap((f) => (f.peak_watts != null ? [f.peak_watts] : []));
  return {
    watts: now.length > 0 ? now.reduce((t, r) => t + r.watts, 0) : null,
    kwh: figures.length > 0 ? figures.reduce((t, f) => t + f.kwh, 0) : null,
    cost: figures.length > 0 ? sumMoney(figures.map((f) => f.cost)) : null,
    peakWatts: peaks.length > 0 ? peaks.reduce((t, p) => t + p, 0) : null,
    count: top.length,
  };
}
