// The parts of a computer that its own collector reports (CPU, GPU, memory), shown under the computer's wall plug in the hierarchy and in the flow
// chart. They are not stored meters: they come from the computer's detail for the period. The wall plug's reading already contains them, so what
// they do not account for (fans, drives, the motherboard, power-supply losses, and any time the sensors were not sampling) is "Everything else":
// the wall plug less the parts, never the parts added to the wall plug.
import { api } from './api';
import { usePoll } from './hooks';
import { isLiveStale, moneyText, type ComputerDetail, type Meter } from './lib';

export interface PcPart {
  key: string;
  label: string;
  kwh: number;
  /** Apportioned from the wall plug's own cost at its average price over the period, so shown as approximate; null when the wall has none. */
  cost: string | null;
  /** The part's reading right now, when its collector's latest look is fresh. */
  watts: number | null;
  /** The line for what the parts do not account for. */
  remainder?: boolean;
}

const money = (kwh: number, rate: number | null) => (rate == null ? null : moneyText(kwh * rate));

/** The parts to show under a computer's wall plug: each sensor (biggest first), then "Everything else". Empty when the computer reports no sensors or has
 * no wall plug. `wallWatts` is the wall plug's reading right now, if known. */
export function pcParts(detail: ComputerDetail, wallWatts: number | null): PcPart[] {
  const wall = detail.wall;
  const sensors = (detail.pc?.components ?? []).filter((c) => c.scope === 'component').sort((a, b) => b.kwh - a.kwh);
  if (!wall || sensors.length === 0) return [];
  const rate = wall.cost != null && wall.kwh > 0 ? Number(wall.cost) / wall.kwh : null;
  const live = detail.pc?.live;
  const fresh = live && !isLiveStale(live) ? (live.readings ?? []) : [];
  const nowOf = (name: string) => fresh.find((r) => r.scope === 'component' && r.name === name)?.watts ?? null;

  const parts: PcPart[] = sensors.map((c) => ({ key: c.name, label: c.name, kwh: c.kwh, cost: money(c.kwh, rate), watts: nowOf(c.name) }));
  const left = Math.max(0, wall.kwh - parts.reduce((t, p) => t + p.kwh, 0));
  const nowAll = parts.every((p) => p.watts != null);
  parts.push({
    key: 'not-metered',
    label: 'Everything else',
    kwh: left,
    cost: money(left, rate),
    watts: wallWatts != null && nowAll ? Math.max(0, wallWatts - parts.reduce((t, p) => t + (p.watts ?? 0), 0)) : null,
    remainder: true,
  });
  return parts;
}

/** The detail of every computer whose wall plug is among `meters`, over [from, to), by the wall plug's meter id. Read once a minute. */
export function usePcDetails(meters: Meter[], from: string | undefined, to: string | undefined): Map<string, ComputerDetail> {
  const walls = meters.filter((m) => m.type === 'computer' && m.enabled).map((m) => m.id);
  const key = walls.join(',');
  const { data } = usePoll<Map<string, ComputerDetail>>(
    async () => {
      if (!from || !to || walls.length === 0) return new Map();
      const computers = (await api.computers()).filter((c) => c.wall_meter_id && walls.includes(c.wall_meter_id));
      const details = await Promise.all(computers.map((c) => api.computerDetail(c.id, from, to)));
      return new Map(computers.map((c, i) => [c.wall_meter_id!, details[i]!]));
    },
    60_000,
    [from, to, key],
  );
  return data ?? new Map();
}
