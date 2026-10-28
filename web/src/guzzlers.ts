// The top guzzlers: the things that used the most energy, or cost the most, over the period. A ranking of meters has to be of meters that do not hold one
// another, or the same energy would be listed twice (a garage feed and the EV charger inside it): so only a meter with nothing listed inside it is ranked,
// the appliance itself (a dryer, a fridge, a charger, a PC), never a feed or a panel and never the whole house.
//
// Two kinds of guzzler: one that works (it goes up and down, and pulls a lot while it runs: an EV charger, a dryer) and one that rests (it is always on at a
// steady draw: a fridge, a network closet, a heated floor on a thermostat). The always-on power of a meter is the level it hardly ever goes below (the server's
// "Always on" figure); a meter's energy over the period splits into what that level accounts for (at rest) and the rest (active).
import { houseShare, moneyText, shownMeters, type Meter, type MeterSummary, type Summary } from './lib';

export type GuzzlerBy = 'kwh' | 'cost';
export type GuzzlerMode = 'active' | 'rest';

export interface Guzzler {
  meter: Meter;
  /** Active: the energy above its resting level. At rest: the energy its resting level adds up to over the period. */
  kwh: number;
  /** The cost of that part, at the meter's own average price over the period (so it is exact only if the price did not change); null with no price. */
  cost: string | null;
  /** The whole percent of the house's energy this is, or null when that is not known or does not add up (the house has less history than this meter). */
  share: number | null;
  /** The highest it drew in the period, or null. */
  peakWatts: number | null;
  /** What it hardly ever goes below, in watts; 0 for a meter that rests at nothing (or whose rest is not known). */
  restWatts: number;
}

/** The meters that hold nothing: circuits, plugs and computers' wall plugs, shown, with no shown meter inside them. */
export function guzzlable(meters: Meter[]): Meter[] {
  const shown = shownMeters(meters);
  const holders = new Set(shown.flatMap((m) => (m.parent_id ? [m.parent_id] : [])));
  return shown.filter((m) => (m.scope === 'circuit' || m.scope === 'plug' || m.scope === 'pc') && !holders.has(m.id));
}

const price = (cost: string | null): number => (cost == null ? -1 : Number(cost));

/** The hours the summary covers. */
export function summaryHours(s: Pick<Summary, 'from' | 'to'>): number {
  const h = (Date.parse(s.to) - Date.parse(s.from)) / 3_600_000;
  return Number.isFinite(h) && h > 0 ? h : 0;
}

/** The biggest users of the period, first. `rests` is each meter's always-on power in watts (the server lists the biggest few; a meter it does not list rests
 * at nothing here). Active: ranked by the energy above that level, or by its cost; a meter with no known price comes after every priced one. At rest: ranked by
 * the always-on power itself. Only meters that have something to show are listed. */
export function topGuzzlers(
  meters: Meter[],
  summary: Summary | undefined,
  by: GuzzlerBy,
  houseKwh: number | null | undefined,
  mode: GuzzlerMode = 'active',
  rests: ReadonlyMap<string, number> = new Map(),
): Guzzler[] {
  if (!summary) return [];
  const hours = summaryHours(summary);
  const figures = new Map<string, MeterSummary>(summary.meters.map((f) => [f.meter_id, f]));
  const out: Guzzler[] = [];
  for (const meter of guzzlable(meters)) {
    const f = figures.get(meter.id);
    if (!f || !(f.kwh > 0)) continue;
    const restWatts = Math.max(0, rests.get(meter.id) ?? 0);
    const restKwh = Math.min(f.kwh, (restWatts * hours) / 1000);
    const kwh = mode === 'rest' ? restKwh : f.kwh - restKwh;
    if (!(kwh > 0) || (mode === 'rest' && !(restWatts > 0))) continue;
    // The same average price for both parts of the meter's energy.
    const cost = f.cost == null ? null : moneyText((Number(f.cost) * kwh) / f.kwh);
    const share = houseShare(kwh, houseKwh);
    out.push({ meter, kwh, cost, share: share != null && share <= 100 ? share : null, peakWatts: f.peak_watts ?? null, restWatts });
  }
  out.sort((a, b) => (mode === 'rest' ? b.restWatts - a.restWatts : by === 'cost' ? price(b.cost) - price(a.cost) || b.kwh - a.kwh : b.kwh - a.kwh));
  return out;
}
