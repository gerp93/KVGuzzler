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
export type PcProgramTotal = S['PcProgramTotal'];
export type PcSeries = S['PcSeries'];
export type ComputerHistory = S['ComputerHistory'];
export type ComputerEstimate = S['ComputerEstimate'];
export type HouseHistory = S['HouseHistory'];
export type HouseHistoryPeriod = S['HouseHistoryPeriod'];
export type TariffInfo = S['TariffInfo'];
export type CarbonConfig = S['CarbonConfig'];
export type TariffCandidate = S['TariffCandidate'];
export type OpenEiStatus = S['OpenEiStatus'];
export type AverageUse = S['AverageUse'];
export type PcHistoryPeriod = S['PcHistoryPeriod'];
export type PcSeriesLine = S['PcSeriesLine'];
export type Device = S['Device'];
export type NewDevice = S['NewDevice'];
export type RateConfig = S['RateConfig'];
export type Bill = S['Bill'];
export type FixedFee = S['FixedFee'];

export type PeriodKey = '1h' | '24h' | '7d' | '30d' | '1y' | 'all';

export const PERIODS: { key: PeriodKey; label: string; ms: number }[] = [
  { key: '1h', label: 'Hour', ms: 3600_000 },
  { key: '24h', label: '24 hours', ms: 24 * 3600_000 },
  { key: '7d', label: '7 days', ms: 7 * 24 * 3600_000 },
  { key: '30d', label: '30 days', ms: 30 * 24 * 3600_000 },
  { key: '1y', label: 'Year', ms: 365 * 24 * 3600_000 },
  // Everything ever recorded: it starts long before any data, and a chart narrows it to where the data starts (see `effectiveDomain`).
  { key: 'all', label: 'All time', ms: 25 * 365 * 24 * 3600_000 },
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

/** The sign shown before an amount: the common currencies, else the three-letter code and a space. */
export function currencySymbol(code: string | null | undefined): string {
  switch (code) {
    case undefined:
    case null:
    case '':
    case 'USD':
    case 'CAD':
    case 'AUD':
    case 'NZD':
    case 'MXN':
      return '$';
    case 'EUR':
      return '€';
    case 'GBP':
      return '£';
    case 'JPY':
    case 'CNY':
      return '¥';
    case 'INR':
      return '₹';
    default:
      return `${code} `;
  }
}

let symbol = '$';

/** Sets the currency every amount is shown in (the server's rate settings say which). Called when the settings are read. */
export function setCurrencyCode(code: string | null | undefined): void {
  symbol = currencySymbol(code);
}

/** A money string with the currency's sign and two decimals. `estimated` marks a price that rests on a stand-in rate with a leading ≈. */
export function formatMoney(m: string | null | undefined, estimated = false): string {
  if (m === null || m === undefined) return '—';
  const n = Number(m);
  if (!Number.isFinite(n)) return '—';
  return `${estimated ? '≈ ' : ''}${n < 0 ? '-' : ''}${symbol}${Math.abs(n).toFixed(2)}`;
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

/** How a line of the inside-the-PC chart is drawn: the wall in the text colour, sensors by kind, meters dashed. The colour is a theme
 * custom property so every theme gets lines it can show. `index` is the line's place among those of its group. */
export function pcLineStyle(line: Pick<PcSeriesLine, 'scope' | 'kind'>, index: number): { colorVar: string; dashed: boolean } {
  if (line.scope === 'wall') return { colorVar: '--color-text', dashed: false };
  if (line.scope === 'peripheral') return { colorVar: index % 2 === 0 ? '--color-accent-muted' : '--color-accent-red', dashed: true };
  if (line.kind === 'cpu') return { colorVar: '--color-accent-blue', dashed: false };
  if (line.kind === 'gpu') return { colorVar: '--color-accent-green', dashed: false };
  return { colorVar: ['--color-accent-red', '--color-accent-muted'][index % 2]!, dashed: false };
}

/** The gaps of a PC series as the seconds-since-epoch stretches the chart shades. */
export function gapShades(gaps: Pick<PcGap, 'from' | 'to'>[]): { from: number; to: number }[] {
  return gaps.map((g) => ({ from: Date.parse(g.from) / 1000, to: Date.parse(g.to) / 1000 })).filter((g) => g.to > g.from);
}

/** The names the PC's collector gives the energy that belongs to no program. */
export const BASELINE = '(system baseline)';
export const UNATTRIBUTED = '(not attributed)';
export const OTHERS = '(other small programs)';

/** A program that caused less than this (0.05 Wh) over the period is not listed on its own. */
export const MIN_LISTED_KWH = 0.00005;
export const MAX_LISTED_PROGRAMS = 25;

export interface ProgramRow {
  name: string;
  kwh: number;
  cost: string | null;
  /** What the row is, in words under its name. */
  note: string;
  /** Share of the yardstick, 0 to 1. */
  share: number;
  kind: 'program' | 'baseline' | 'unattributed' | 'rollup';
}

const isSpecial = (p: Pick<PcProgramTotal, 'name'>) => p.name === BASELINE || p.name === UNATTRIBUTED;

/** Programs biggest first (those under 0.05 Wh rolled into one line), then the two rows that belong to no program. A share is of the
 * whole PC at the wall when that is known, else of the CPU and GPU energy the programs were shared out of. */
export function programRows(programs: PcProgramTotal[], wholeKwh: number | null | undefined, max = MAX_LISTED_PROGRAMS): ProgramRow[] {
  const sum = (list: PcProgramTotal[]) => list.reduce((t, p) => t + p.kwh, 0);
  const yardstick = wholeKwh != null && wholeKwh > 0 ? wholeKwh : sum(programs);
  const share = (kwh: number) => (yardstick > 0 ? Math.min(1, Math.max(0, kwh / yardstick)) : 0);
  const both = (p: PcProgramTotal) => `GPU ${formatKwh(p.gpu_kwh)} · CPU ${formatKwh(p.cpu_kwh)}`;

  const row = (p: PcProgramTotal): ProgramRow => {
    const active = p.active_seconds ?? 0;
    if (p.name === BASELINE)
      return { name: p.name, kwh: p.kwh, cost: p.cost ?? null, note: "the CPU's and GPU's idle power, not any program's doing", share: share(p.kwh), kind: 'baseline' };
    if (p.name === UNATTRIBUTED)
      return { name: p.name, kwh: p.kwh, cost: p.cost ?? null, note: 'power above idle that no program reported using', share: share(p.kwh), kind: 'unattributed' };
    if (p.name === OTHERS)
      return { name: p.name, kwh: p.kwh, cost: p.cost ?? null, note: `many small programs together${active > 0 ? ` · active up to ${formatDuration(active)}` : ''}`, share: share(p.kwh), kind: 'program' };
    return { name: p.name, kwh: p.kwh, cost: p.cost ?? null, note: both(p) + (active > 0 ? ` · active ${formatDuration(active)}` : ''), share: share(p.kwh), kind: 'program' };
  };

  const ranked = programs.filter((p) => !isSpecial(p)).sort((a, b) => b.kwh - a.kwh);
  const shown = ranked.filter((p) => p.kwh >= MIN_LISTED_KWH).slice(0, max);
  const rest = ranked.slice(shown.length);
  const rows = shown.map(row);
  if (rest.length > 0) {
    const kwh = sum(rest);
    rows.push({
      name: `${rest.length} smaller program${rest.length === 1 ? '' : 's'}`, kwh, cost: sumMoney(rest.map((p) => p.cost)),
      note: 'none of them caused enough to list on its own', share: share(kwh), kind: 'rollup',
    });
  }
  rows.push(...programs.filter(isSpecial).map(row));
  return rows;
}

export interface SplitPart {
  label: string;
  kwh: number;
  kind: 'programs' | 'baseline' | 'unattributed' | 'rest';
}

/** The CPU and GPU energy split into the programs, the idle baseline and what no program claimed; with the whole PC known, the rest of it
 * (fans, drives, memory, power-supply loss) is the last piece. Parts that are nothing are left out. */
export function programSplit(programs: PcProgramTotal[], wholeKwh: number | null | undefined): SplitPart[] {
  const total = (pred: (p: PcProgramTotal) => boolean) => programs.filter(pred).reduce((t, p) => t + p.kwh, 0);
  const named = total((p) => !isSpecial(p));
  const baseline = total((p) => p.name === BASELINE);
  const unattributed = total((p) => p.name === UNATTRIBUTED);
  const parts: SplitPart[] = [
    { label: 'Programs', kwh: named, kind: 'programs' },
    { label: 'Idle baseline', kwh: baseline, kind: 'baseline' },
    { label: 'Not attributed', kwh: unattributed, kind: 'unattributed' },
  ];
  // The sensors and the plug are read a moment apart, so the rest can come out a hair negative: show it only when it is real.
  const rest = wholeKwh != null ? wholeKwh - (named + baseline + unattributed) : 0;
  if (rest > 0) parts.push({ label: 'Everything else', kwh: rest, kind: 'rest' });
  return parts.filter((p) => p.kwh > 0);
}

/** The second line of an energy-history row: what part of the figure the sensors account for, and every stretch nothing was recorded, by
 * reason, so nothing is silently missing. */
export function historyDetail(p: PcHistoryPeriod): string {
  const parts: string[] = [];
  if (p.basis === 'wall') {
    parts.push(`of which CPU, GPU and memory ${formatKwh(p.sensors_kwh)}${p.kwh > 0 && p.sensors_kwh > 0 ? ` (${Math.round((p.sensors_kwh / p.kwh) * 100)}% of the whole PC)` : ''}`);
  } else {
    parts.push('CPU, GPU and memory sensors only');
  }
  if (p.asleep_seconds > 0) parts.push(`asleep ${formatDuration(p.asleep_seconds)}`);
  if (p.off_seconds > 0) parts.push(`off ${formatDuration(p.off_seconds)}`);
  if (p.lost_power_seconds > 0) parts.push(`lost power or crashed ${formatDuration(p.lost_power_seconds)}`);
  return parts.join(' · ');
}

/** The key of the PC itself among the things a computer's figures can count (the plugs next to it are keyed by channel id). */
export const COUNT_PC = 'pc';

/** The things left out of a computer's figures, as saved: a comma list. A new plug counts until it is switched off. */
export function parseCountOff(text: string): Set<string> {
  return new Set(text.split(',').filter(Boolean));
}

export function toggleCount(off: ReadonlySet<string>, key: string): Set<string> {
  const next = new Set(off);
  if (!next.delete(key)) next.add(key);
  return next;
}

export interface CountedTotals {
  /** Null when nothing counted has a figure. */
  kwh: number | null;
  /** Null when any counted part has no price: a partly priced total would mislead. */
  cost: string | null;
  /** The parts' highest draws added: an upper bound when more than one part counts, as they need not peak together. */
  peak: number | null;
  peakIsBound: boolean;
  /** What is counted, in words: "the whole PC at the wall", "Monitor". */
  parts: string[];
  /** The carbon of what is counted, in grams of CO2e: each part's own figure added. Null when no counted part has one. */
  carbonG: number | null;
  /** The part of the counted energy whose carbon was worked out at an average (its own hour had no saved intensity). */
  carbonEstimatedKwh: number;
}

/** What a computer's figures add up to for the things ticked: the PC at its wall plug and the plugs next to it, each a separate device
 * measured on its own (the PC's figure never contains them). */
export function countedTotals(
  detail: Pick<ComputerDetail, 'wall' | 'peripherals'>, off: ReadonlySet<string>, label: (channelId: string) => string,
): CountedTotals {
  const counted: { summary: ChannelSummary; name: string }[] = [];
  if (detail.wall && !off.has(COUNT_PC)) counted.push({ summary: detail.wall, name: 'the whole PC at the wall' });
  for (const p of detail.peripherals) if (!off.has(p.channel_id)) counted.push({ summary: p, name: label(p.channel_id) });
  if (counted.length === 0) return { kwh: null, cost: null, peak: null, peakIsBound: false, parts: [], carbonG: null, carbonEstimatedKwh: 0 };

  const peaks = counted.map((c) => c.summary.peak_watts).filter((w): w is number => w != null);
  return {
    kwh: counted.reduce((t, c) => t + c.summary.kwh, 0),
    cost: sumMoney(counted.map((c) => c.summary.cost)),
    peak: peaks.length > 0 ? peaks.reduce((t, w) => t + w, 0) : null,
    peakIsBound: peaks.length > 1,
    parts: counted.map((c) => c.name),
    carbonG: counted.some((c) => c.summary.carbon_g != null) ? counted.reduce((t, c) => t + (c.summary.carbon_g ?? 0), 0) : null,
    carbonEstimatedKwh: counted.reduce((t, c) => t + (c.summary.carbon_estimated_kwh ?? 0), 0),
  };
}

/** The draw right now of what is counted: the PC (when ticked and known) plus each ticked plug that has a reading. Null with none. */
export function countedWatts(pcWatts: number | null, plugs: { key: string; watts: number | null }[], off: ReadonlySet<string>): number | null {
  const parts: number[] = [];
  if (pcWatts != null && !off.has(COUNT_PC)) parts.push(pcWatts);
  for (const p of plugs) if (p.watts != null && !off.has(p.key)) parts.push(p.watts);
  return parts.length > 0 ? parts.reduce((t, w) => t + w, 0) : null;
}

export type EstimateMode = 'constant' | 'now' | 'usage';

export const ESTIMATE_WINDOWS: { key: '24h' | '7d' | '30d' | 'all'; label: string; days: number | null }[] = [
  { key: '24h', label: 'the last 24 hours', days: 1 },
  { key: '7d', label: 'the last 7 days', days: 7 },
  { key: '30d', label: 'the last 30 days', days: 30 },
  { key: 'all', label: 'all recorded time', days: null },
];

export interface EstimateLine {
  label: string;
  kwh: number;
  /** A money string, or null without a price. */
  cost: string | null;
}

/** The figures under the estimator: what the same energy per day would be per hour (not for real usage), day, 30 days and year. */
export function estimateLines(kwhPerDay: number, rate: string | null, perHour: boolean): EstimateLine[] {
  const line = (label: string, days: number): EstimateLine => {
    const kwh = kwhPerDay * days;
    const price = rate != null ? Number(rate) : NaN;
    return { label, kwh, cost: Number.isFinite(price) ? (kwh * price).toFixed(4) : null };
  };
  return [...(perHour ? [line('Per hour', 1 / 24)] : []), line('Per day', 1), line('Per 30 days', 30), line('Per year', 365)];
}

/** Where a "my usage" estimate comes from, in words: the window, what was counted, and that off and asleep time is in it. */
export function usageBasis(use: AverageUse, windowKey: string): string {
  const w = ESTIMATE_WINDOWS.find((x) => x.key === windowKey) ?? ESTIMATE_WINDOWS[1]!;
  const short = w.days == null || use.days < w.days * 0.95;
  const covered = short ? `the ${formatDuration(use.days * 86400)} recorded so far` : w.label;
  const plugs = `${use.peripheral_count} peripheral meter${use.peripheral_count === 1 ? '' : 's'}`;
  const what = !use.includes_pc
    ? `That is the ${plugs} only, not the PC.`
    : `${use.whole_pc ? 'That is the whole PC at the wall' : 'That is the CPU, GPU and memory sensors only (a wall plug would show the whole PC)'}${use.peripheral_count > 0 ? `, plus ${plugs}.` : '.'}`;
  return `The PC used ${formatKwh(use.kwh)} over ${covered}: ${formatKwh(use.kwh_per_day)} a day, an average of ${formatWatts(use.average_watts)} counting the time it was off or asleep. ${what}`;
}

/** A file size in words: "812 B", "212 KB", "3.4 MB". */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let v = bytes / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

export const PROJECT_URL = 'https://github.com/gerp93/KVGuzzler';
export const ISSUES_URL = 'https://github.com/gerp93/KVGuzzler/issues';
/** The viewer's installers are pre-releases tagged viewer-vX.Y.Z (see .github/workflows/release-viewer.yml). */
export const VIEWER_RELEASES_URL = 'https://github.com/gerp93/KVGuzzler/releases?q=viewer-v';

/** What a looked-up price comes to over a period, in words: the rate, what the period averaged to per kWh and what is left out. With no
 * energy to average over the line says only which rate it is and what is left out. */
export function tariffNote(t: Pick<TariffInfo, 'utility' | 'name' | 'tiers' | 'fixed_charge' | 'varies_by_time'>, kwh: number, cost: string | null): string {
  const avg = cost != null && kwh > 0 ? Number(cost) / kwh : null;
  const parts = [`Priced at ${t.utility}: ${t.name}`];
  if (avg != null && Number.isFinite(avg)) parts.push(`this period averaged ${symbol}${avg.toFixed(4)} per kWh${t.varies_by_time ? ', weighted by when the power is used' : ''}`);
  const left = ['The energy charge only'];
  if (t.fixed_charge) left.push(`the fixed charge (${t.fixed_charge}) is not in it`);
  if (t.tiers > 1) left.push('only the first usage tier is used');
  return `${parts.join('; ')}. ${left.join('; ')}.`;
}

/** An amount of carbon in words, always approximate: "≈ 412 g CO₂e", "≈ 1.24 kg CO₂e", "≈ 2.1 t CO₂e". */
export function formatCarbon(grams: number): string {
  const g = Math.abs(grams);
  if (g < 1000) return `≈ ${Math.round(grams)} g CO₂e`;
  if (g < 1_000_000) return `≈ ${(grams / 1000).toFixed(g < 10_000 ? 2 : 1)} kg CO₂e`;
  return `≈ ${(grams / 1_000_000).toFixed(1)} t CO₂e`;
}

/** The carbon line under a computer's figures: the amount, the energy-weighted grid average it came to, and how much was valued at an average
 * for want of its own hour's figure. Empty when there is no carbon to show. */
export function carbonNote(carbonG: number | null | undefined, kwh: number | null | undefined, estimatedKwh: number): string {
  if (carbonG == null || kwh == null) return '';
  const parts = [`${formatCarbon(carbonG)} (the grid's average, not what one more kWh would cause)`];
  if (kwh > 0) {
    parts.push(`averaging ${Math.round((carbonG / kwh))} g per kWh`);
    const share = Math.round(Math.min(1, Math.max(0, estimatedKwh / kwh)) * 100);
    if (share > 0) parts.push(`${share}% of the energy valued at an average for want of its own hour's figure`);
  }
  return parts.join('; ') + '.';
}


/** Where a house "my usage" estimate comes from, in words: the window and that every hour is in it. */
export function houseUsageBasis(use: AverageUse, windowKey: string): string {
  const w = ESTIMATE_WINDOWS.find((x) => x.key === windowKey) ?? ESTIMATE_WINDOWS[1]!;
  const short = w.days == null || use.days < w.days * 0.95;
  const covered = short ? `the ${formatDuration(use.days * 86400)} recorded so far` : w.label;
  return `The house used ${formatKwh(use.kwh)} over ${covered}: ${formatKwh(use.kwh_per_day)} a day, an average of ${formatWatts(use.average_watts)} counting every hour. That is the mains clamps.`;
}

/** The share of the house a part of it used, as a whole-number percent; null when the house figure is missing or zero. */
export function houseShare(kwh: number, houseKwh: number | null | undefined): number | null {
  return houseKwh != null && houseKwh > 0 ? Math.round((kwh / houseKwh) * 100) : null;
}

/** A price per kWh in the saved currency, four decimals: "$0.1558". */
export function formatRate(perKwh: number): string {
  return `${symbol}${perKwh.toFixed(4)}`;
}

/** What a rate costs, short: "$0.0727 per kWh" or "$0.1558 to $0.1745 per kWh by time of day". */
export function rateRange(t: Pick<TariffInfo, 'cheapest' | 'dearest' | 'varies_by_time'>): string {
  return t.varies_by_time ? `${formatRate(t.cheapest)} to ${formatRate(t.dearest)} per kWh by time of day` : `${formatRate(t.cheapest)} per kWh`;
}

/** The rates whose name or utility has every word typed (any case); everything for an empty filter. */
export function filterTariffs<T extends Pick<TariffInfo, 'name' | 'utility'>>(list: T[], text: string): T[] {
  const words = text.toLowerCase().split(/\s+/).filter(Boolean);
  return words.length === 0 ? list : list.filter((t) => words.every((w) => `${t.utility} ${t.name}`.toLowerCase().includes(w)));
}

/** How many rates are listed before "Show all": the first few are the likeliest (the server sorts the most common and expert-checked first). */
export const TARIFFS_SHOWN = 5;

/** The utility that most of the rates share, so it can be said once instead of on every row; null when they are from several. */
export function sharedUtility(list: Pick<TariffInfo, 'utility'>[]): string | null {
  const first = list[0]?.utility;
  return first !== undefined && list.every((t) => t.utility === first) ? first : null;
}
