// Pure helpers for telling at a glance whether a meter is working or resting right now. The server learns each meter's levels from its last 30 days
// (the same working level the runs use); this only compares a live reading with them.
import type { components } from './api-types';
import { guzzlable } from './guzzlers';
import { formatWatts, type Meter, type Reading } from './lib';

export type Levels = components['schemas']['Levels'];
export type MeterLevels = Levels['meters'][number];

export type MeterState = 'working' | 'resting';

/** Whether a live reading is the meter working or resting; undefined when its levels are not known (too little history) or there is no reading. */
export function meterState(level: MeterLevels | undefined, watts: number | null | undefined): MeterState | undefined {
  if (!level || watts == null) return undefined;
  return level.working_above_watts != null && watts >= level.working_above_watts ? 'working' : 'resting';
}

/** The words for the cell's tooltip, so the colour is never the only thing saying it. */
export function stateTitle(state: MeterState, level: MeterLevels): string {
  if (state === 'working') return `Working: above resting draw (about ${formatWatts(level.rest_watts)})`;
  return level.working_above_watts == null ? `Resting: draw barely changes, about ${formatWatts(level.rest_watts)}` : `Resting: at resting draw (about ${formatWatts(level.rest_watts)})`;
}

/** The levels by meter id, for looking one up per row. */
export function levelsById(levels: Levels | undefined): Map<string, MeterLevels> {
  return new Map((levels?.meters ?? []).map((m) => [m.meter_id, m]));
}

export interface Running {
  meter: Meter;
  watts: number;
}

/** What is working right now, the biggest draw first: the appliances (meters that hold no other meter, so a feed and the plug inside it are not both listed)
 * whose live reading is above their own resting level. A meter whose levels are not known, or that barely changes, is never listed. */
export function runningNow(meters: Meter[], readings: Pick<Reading, 'meter_id' | 'watts'>[], levels: Levels | undefined): Running[] {
  const byLevel = levelsById(levels);
  const now = new Map(readings.map((r) => [r.meter_id, r.watts]));
  const out: Running[] = [];
  for (const meter of guzzlable(meters)) {
    const watts = now.get(meter.id);
    if (meterState(byLevel.get(meter.id), watts) === 'working') out.push({ meter, watts: watts! });
  }
  return out.sort((a, b) => b.watts - a.watts);
}
