// Pure helpers for telling at a glance whether a meter is working or resting right now. The server learns each meter's levels from its last 30 days
// (the same working level the runs use); this only compares a live reading with them.
import type { components } from './api-types';
import { formatWatts } from './lib';

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
