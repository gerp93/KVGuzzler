// The top guzzlers: the things that used the most energy, or cost the most, over the period. A ranking of meters has to be of meters that do not hold one
// another, or the same energy would be listed twice (a garage feed and the EV charger inside it): so only a meter with nothing listed inside it is ranked,
// the appliance itself (a dryer, a fridge, a charger, a PC), never a feed or a panel and never the whole house.
import { houseShare, shownMeters, type Meter, type MeterSummary, type Summary } from './lib';

export type GuzzlerBy = 'kwh' | 'cost';

export interface Guzzler {
  meter: Meter;
  kwh: number;
  cost: string | null;
  /** The whole percent of the house's energy this is, or null when that is not known or does not add up (the house has less history than this meter). */
  share: number | null;
}

/** The meters that hold nothing: circuits, plugs and computers' wall plugs, shown, with no shown meter inside them. */
export function guzzlable(meters: Meter[]): Meter[] {
  const shown = shownMeters(meters);
  const holders = new Set(shown.flatMap((m) => (m.parent_id ? [m.parent_id] : [])));
  return shown.filter((m) => (m.scope === 'circuit' || m.scope === 'plug' || m.scope === 'pc') && !holders.has(m.id));
}

const price = (f: MeterSummary): number => (f.cost == null ? -1 : Number(f.cost));

/** The biggest users of the period, first. By cost, a meter with no known price comes after every priced one. Only meters that used energy are listed. */
export function topGuzzlers(meters: Meter[], summary: Summary | undefined, by: GuzzlerBy, houseKwh: number | null | undefined): Guzzler[] {
  if (!summary) return [];
  const figures = new Map(summary.meters.map((f) => [f.meter_id, f]));
  const out: { g: Guzzler; f: MeterSummary }[] = [];
  for (const meter of guzzlable(meters)) {
    const f = figures.get(meter.id);
    if (!f || !(f.kwh > 0)) continue;
    const share = houseShare(f.kwh, houseKwh);
    out.push({ g: { meter, kwh: f.kwh, cost: f.cost ?? null, share: share != null && share <= 100 ? share : null }, f });
  }
  out.sort((a, b) => (by === 'cost' ? price(b.f) - price(a.f) || b.f.kwh - a.f.kwh : b.f.kwh - a.f.kwh));
  return out.map((o) => o.g);
}
