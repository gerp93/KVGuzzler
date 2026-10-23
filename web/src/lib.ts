// Pure helpers for the dashboard. Nothing here touches the DOM or the network.
//
// The rule that shapes most of them: scopes are never summed across each other. The house is the `mains` channels
// alone; circuits and plugs are parts of it; the PC's wall plug (`pc`) is a slice of some circuit and is added to nothing.
import type { components } from './api-types';

type S = components['schemas'];
export type Channel = S['Channel'];
export type Reading = S['Reading'];
export type Live = S['Live'];
export type Series = S['Series'];
export type Point = S['SeriesPoint'];
export type Summary = S['Summary'];
export type ChannelSummary = S['ChannelSummary'];
export type Status = S['Status'];
export type Computer = S['Computer'];
export type ApplianceGroup = S['ApplianceGroup'];
export type GroupDetail = S['GroupDetail'];
export type ComputerDetail = S['ComputerDetail'];
export type PcSource = S['PcSource'];
export type PcComponent = S['PcComponent'];
export type PcGap = S['PcGap'];
export type PcLive = S['PcLive'];
export type PcLiveReading = S['PcLiveReading'];
export type PcLiveProgram = S['PcLiveProgram'];
export type PcRun = S['PcRun'];
export type Device = S['Device'];
export type NewDevice = S['NewDevice'];
export type RateConfig = S['RateConfig'];
export type Bill = S['Bill'];
export type FixedFee = S['FixedFee'];

export type PeriodKey = '1h' | '24h' | '7d' | '30d';

export const PERIODS: { key: PeriodKey; label: string; ms: number }[] = [
  { key: '1h', label: 'Hour', ms: 3600_000 },
  { key: '24h', label: '24 hours', ms: 24 * 3600_000 },
  { key: '7d', label: '7 days', ms: 7 * 24 * 3600_000 },
  { key: '30d', label: '30 days', ms: 30 * 24 * 3600_000 },
];

/** The period ending at the next whole minute after `now`, as the RFC 3339 strings the API wants. */
export function periodRange(key: PeriodKey, now: Date): { from: string; to: string } {
  const ms = PERIODS.find((p) => p.key === key)?.ms ?? PERIODS[0]!.ms;
  const end = Math.floor(now.getTime() / 60_000) * 60_000 + 60_000;
  return { from: new Date(end - ms).toISOString(), to: new Date(end).toISOString() };
}

export function formatWatts(w: number | null | undefined): string {
  if (w === null || w === undefined) return '—';
  return Math.abs(w) >= 1000 ? `${(w / 1000).toFixed(2)} kW` : `${Math.round(w)} W`;
}

export function formatKwh(k: number | null | undefined): string {
  if (k === null || k === undefined) return '—';
  if (k !== 0 && Math.abs(k) < 0.01) return `${(k * 1000).toFixed(1)} Wh`;
  return `${k < 10 ? k.toFixed(2) : k.toFixed(1)} kWh`;
}

/** A money string as dollars and cents. `estimated` marks a price that rests on a stand-in rate with a leading ≈. */
export function formatMoney(m: string | null | undefined, estimated = false): string {
  if (m === null || m === undefined) return '—';
  const n = Number(m);
  if (!Number.isFinite(n)) return '—';
  return `${estimated ? '≈ ' : ''}${n < 0 ? '-' : ''}$${Math.abs(n).toFixed(2)}`;
}

/** Above this per kWh a rate is almost certainly cents typed as dollars. */
export const SUSPICIOUSLY_HIGH_RATE = 1;

export interface Row {
  channel: Channel;
  watts: number;
}

const byId = (channels: Channel[]) => new Map(channels.map((c) => [c.id, c]));

/** Enabled circuits and plugs with their live watts, highest first. Mains and the PC slice are not in it. */
export function circuitsByWatts(channels: Channel[], readings: Reading[]): Row[] {
  const ch = byId(channels);
  const rows: Row[] = [];
  for (const r of readings) {
    const c = ch.get(r.channel_id);
    if (c && c.enabled && (c.scope === 'circuit' || c.scope === 'plug')) rows.push({ channel: c, watts: r.watts });
  }
  return rows.sort((a, b) => b.watts - a.watts);
}

/** The PC's own wall plugs, kept apart from everything else. */
export function pcSlices(channels: Channel[], readings: Reading[]): Row[] {
  const ch = byId(channels);
  const rows: Row[] = [];
  for (const r of readings) {
    const c = ch.get(r.channel_id);
    if (c && c.enabled && c.scope === 'pc') rows.push({ channel: c, watts: r.watts });
  }
  return rows;
}

/** What no circuit clamp accounts for: the house minus the circuits, never below zero. Null without a house figure. */
export function unmetered(house: number | null | undefined, circuits: Row[]): number | null {
  if (house === null || house === undefined) return null;
  const sum = circuits.reduce((t, r) => t + (r.channel.scope === 'circuit' ? r.watts : 0), 0);
  return Math.max(0, house - sum);
}

/** Adds series point by point (same bucket start). Only ever called with `mains` series. */
export function sumByTime(list: Series[]): Point[] {
  const byT = new Map<string, Point>();
  for (const s of list) {
    for (const p of s.points) {
      const have = byT.get(p.t);
      if (have) {
        have.avg_watts += p.avg_watts;
        have.max_watts += p.max_watts; // an upper bound: the legs' peaks need not coincide
        have.kwh += p.kwh;
      } else {
        byT.set(p.t, { ...p });
      }
    }
  }
  return [...byT.values()].sort((a, b) => a.t.localeCompare(b.t));
}

export interface Split {
  circuits: ChannelSummary[];
  pc: ChannelSummary[];
  houseKwh: number | null;
  unmeteredKwh: number | null;
}

/** Splits a summary by scope without adding any scope to another. */
export function splitSummary(s: Summary): Split {
  return {
    circuits: s.channels.filter((c) => c.scope === 'circuit' || c.scope === 'plug').sort((a, b) => b.kwh - a.kwh),
    pc: s.channels.filter((c) => c.scope === 'pc'),
    houseKwh: s.house_kwh ?? null,
    unmeteredKwh: s.unmetered_kwh ?? null,
  };
}

export const THEMES = [
  'blue-oval', 'bubblegum', 'commander-keen', 'electric-lime', 'flambeau', 'flambeau-inverse', 'green-acres',
  'hacker', 'hawkeye', 'lava', 'merica', 'neon', 'red-barn', 'retrowave',
] as const;
export type Theme = (typeof THEMES)[number];
export const DEFAULT_THEME: Theme = 'blue-oval';

export function isTheme(v: unknown): v is Theme {
  return typeof v === 'string' && (THEMES as readonly string[]).includes(v);
}

export interface PcGroups {
  wall: PcComponent[];
  sensors: PcComponent[];
  inside: PcComponent[];
  meters: PcComponent[];
}

/** Sorts the PC's readings into the groups the PC tab shows. The groups are never summed with each other: the wall figure
 * already contains the sensors, the "inside" readings are parts of a sensor, and the meters are separate devices. */
export function groupComponents(components: PcComponent[]): PcGroups {
  const by = (scope: string) => components.filter((c) => c.scope === scope).sort((a, b) => b.kwh - a.kwh);
  return { wall: by('wall'), sensors: by('component'), inside: by('breakdown'), meters: by('peripheral') };
}

export function gapLabel(reason: string): string {
  switch (reason) {
    case 'asleep':
      return 'Asleep';
    case 'off':
      return 'Off or stopped';
    case 'lost_power':
      return 'Lost power';
    default:
      return reason;
  }
}

/** A length of time in words: "45 s", "12 min", "4 h 30 min". */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return `${s} s`;
  const minutes = Math.round(s / 60);
  if (minutes < 60) return `${minutes} min`;
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m === 0 ? `${h} h` : `${h} h ${m} min`;
}

/** One row of the Appliances list: an ordinary appliance (a circuit or a plug), or a group of plugs shown as one. */
export interface ApplianceRow {
  kind: 'channel' | 'group';
  /** The channel's id, or the group's. */
  id: string;
  label: string;
  kwh: number;
  /** The cost as a money string; null when any part of it has no price. */
  cost: string | null;
}

export interface ComputerRow {
  computer: Computer;
  /** The whole PC at its wall plug, when it has one. */
  wall?: ChannelSummary;
}

export interface ApplianceList {
  /** Circuits, plugs and groups that belong to no computer, biggest first. */
  appliances: ApplianceRow[];
  /** Computers, with the figure of the plug they are on. */
  computers: ComputerRow[];
}

/** Adds money strings for display. Null if any is missing (a partly priced total would mislead). */
export function sumMoney(parts: (string | null | undefined)[]): string | null {
  if (parts.length === 0 || parts.some((p) => p === null || p === undefined)) return null;
  return parts.reduce((t, p) => t + Number(p), 0).toFixed(2);
}

/** Splits the summary into what the Appliances list shows. A plug that belongs to a computer is listed under that computer and never
 * alone; a plug in a group is listed once, as the group (all ordinary plugs, so their energy is added); a computer's figure is its wall
 * plug's. No scope is added to another. */
export function applianceList(summary: Summary, channels: Channel[], computers: Computer[], groups: ApplianceGroup[] = []): ApplianceList {
  const label = new Map(channels.map((c) => [c.id, c.label]));
  const byChannel = new Map(summary.channels.map((c) => [c.channel_id, c]));

  const rows: ApplianceRow[] = summary.channels
    .filter((c) => (c.scope === 'circuit' || c.scope === 'plug') && !c.computer_id && !c.group_id)
    .map((c) => ({ kind: 'channel' as const, id: c.channel_id, label: label.get(c.channel_id) ?? c.channel_id, kwh: c.kwh, cost: c.cost ?? null }));
  for (const g of groups) {
    const members = summary.channels.filter((c) => c.group_id === g.id);
    if (members.length === 0) continue;
    rows.push({
      kind: 'group', id: g.id, label: g.name,
      kwh: members.reduce((t, m) => t + m.kwh, 0),
      cost: sumMoney(members.map((m) => m.cost)),
    });
  }
  rows.sort((a, b) => b.kwh - a.kwh);

  const comps = computers.map((computer) => ({
    computer,
    wall: computer.wall_channel_id ? byChannel.get(computer.wall_channel_id) : undefined,
  }));
  comps.sort((a, b) => (b.wall?.kwh ?? -1) - (a.wall?.kwh ?? -1));
  return { appliances: rows, computers: comps };
}

/** A PC look older than this is shown as "not answering" rather than as live. */
export const LIVE_STALE_SECONDS = 30;

export function isLiveStale(live: Pick<PcLive, 'age_seconds'>): boolean {
  return (live.age_seconds ?? 0) > LIVE_STALE_SECONDS;
}

/** "3 s ago", "4 min ago": how old a look is. */
export function formatAge(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  return s < 5 ? 'just now' : `${formatDuration(s)} ago`;
}

export interface LiveSplit {
  /** Separate pieces of hardware, biggest first. These are what is summed. */
  sensors: PcLiveReading[];
  /** Parts of a sensor above (CPU cores inside the package): shown, never added. */
  inside: PcLiveReading[];
  /** The PC's own reading of its wall plug, when it has one. */
  wall: PcLiveReading | undefined;
  /** Other plugs the PC itself reads (monitor, speakers): separate devices, added to nothing. */
  meters: PcLiveReading[];
  sensorWatts: number;
  /** What each share is a share of: the whole PC at the wall when it is measured, else the sensors' total. */
  baseWatts: number;
  baseIsWall: boolean;
  /** The wall minus the sensors (fans, drives, power-supply loss), never below zero; null without a wall reading. */
  otherWatts: number | null;
}

const byWatts = (a: PcLiveReading, b: PcLiveReading) => b.watts - a.watts;

/** Sorts a look into the groups the page shows. Wall, sensors, inside and meters are never summed with each other. */
export function liveSplit(live: Pick<PcLive, 'readings'>): LiveSplit {
  const by = (scope: string) => (live.readings ?? []).filter((r) => r.scope === scope).sort(byWatts);
  const sensors = by('component');
  const wall = by('wall')[0];
  const sensorWatts = sensors.reduce((t, r) => t + r.watts, 0);
  return {
    sensors,
    inside: by('breakdown'),
    wall,
    meters: by('peripheral'),
    sensorWatts,
    baseWatts: wall ? wall.watts : sensorWatts,
    baseIsWall: !!wall,
    otherWatts: wall ? Math.max(0, wall.watts - sensorWatts) : null,
  };
}

/** A share as a whole-number percent; null when there is nothing to share of. */
export function sharePercent(watts: number, base: number): number | null {
  return base > 0 ? Math.round((watts / base) * 100) : null;
}

export interface ProgramNow {
  name: string;
  cpuWatts: number;
  gpuWatts: number;
  watts: number;
}

/** The live per-program split, biggest first, leaving out programs causing next to nothing. */
export function programsNow(live: Pick<PcLive, 'programs'>, minWatts = 0.5): ProgramNow[] {
  return (live.programs ?? [])
    .map((p) => ({ name: p.name, cpuWatts: p.cpu_watts, gpuWatts: p.gpu_watts, watts: p.cpu_watts + p.gpu_watts }))
    .filter((p) => p.watts >= minWatts)
    .sort((a, b) => b.watts - a.watts);
}

/** In words, what the per-program estimate is measured from. */
export function idleText(live: Pick<PcLive, 'idle_cpu_watts' | 'idle_gpu_watts'>): string {
  const part = (label: string, w: number | null | undefined) => (w == null ? `${label} not learned yet` : `${label} ${formatWatts(w)}`);
  return `${part('CPU', live.idle_cpu_watts)}, ${part('GPU', live.idle_gpu_watts)}`;
}

export function runKwh(run: Pick<PcRun, 'cpu_kwh' | 'gpu_kwh'>): number {
  return run.cpu_kwh + run.gpu_kwh;
}

/** How long a run lasted, start to end. */
export function runDuration(run: Pick<PcRun, 'start' | 'end'>): number {
  return Math.max(0, (Date.parse(run.end) - Date.parse(run.start)) / 1000);
}
