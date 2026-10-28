// Pure helpers for the dashboard. Nothing here touches the DOM or the network.
//
// The rule that shapes most of them: scopes are never summed across each other. The house is the `mains` meters
// alone; circuits and plugs are parts of it; the PC's wall plug (`pc`) is a slice of some circuit and is added to nothing.
import type { components } from './api-types';
import type { Fact } from './Facts';

type S = components['schemas'];
export type Meter = S['Meter'];
export type Reading = S['Reading'];
export type Live = S['Live'];
export type Series = S['Series'];
export type Point = S['SeriesPoint'];
export type Summary = S['Summary'];
export type MeterSummary = S['MeterSummary'];
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
export type CarbonSeries = S['CarbonSeries'];
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

export type PeriodKey = '1h' | '3h' | '8h' | '24h' | '3d' | '7d' | '14d' | '30d' | 'ytd' | '1y' | 'all';

const HOUR = 3600_000;
const DAY = 24 * HOUR;

/** `ms` is a rolling span ending now; `ytd` has none, it starts at the first minute of the local year (see `periodRange`). */
export const PERIODS: { key: PeriodKey; label: string; ms: number }[] = [
  { key: '1h', label: 'Hour', ms: HOUR },
  { key: '3h', label: '3 hours', ms: 3 * HOUR },
  { key: '8h', label: '8 hours', ms: 8 * HOUR },
  { key: '24h', label: '24 hours', ms: DAY },
  { key: '3d', label: '3 days', ms: 3 * DAY },
  { key: '7d', label: '7 days', ms: 7 * DAY },
  { key: '14d', label: '14 days', ms: 14 * DAY },
  { key: '30d', label: '30 days', ms: 30 * DAY },
  { key: 'ytd', label: 'Year to date', ms: 0 },
  { key: '1y', label: 'Year', ms: 365 * DAY },
  // Everything ever recorded: it starts long before any data, and a chart narrows it to where the data starts (see `effectiveDomain`).
  { key: 'all', label: 'All time', ms: 25 * 365 * DAY },
];

/** The period ending at the next whole minute after `now`, as the RFC 3339 strings the API wants. Year to date starts at midnight on 1 January, this computer's local time. */
export function periodRange(key: PeriodKey, now: Date): { from: string; to: string } {
  const end = Math.floor(now.getTime() / 60_000) * 60_000 + 60_000;
  if (key === 'ytd') return { from: new Date(now.getFullYear(), 0, 1).toISOString(), to: new Date(end).toISOString() };
  const ms = PERIODS.find((p) => p.key === key)?.ms ?? PERIODS[0]!.ms;
  return { from: new Date(end - ms).toISOString(), to: new Date(end).toISOString() };
}

/** The periods the recorded history reaches back to, for the picker: a longer one would only show the same data again, so it is left out until there is
 *  that much. The first (an hour), "all time" and the one picked are always kept; year to date needs data from before 1 January. With nothing known all are kept. */
export function visiblePeriods(since: string | null | undefined, selected: PeriodKey, now: Date): typeof PERIODS {
  const first = since ? Date.parse(since) : NaN;
  if (!Number.isFinite(first)) return PERIODS;
  const have = now.getTime() - first;
  return PERIODS.filter((p, i) => {
    if (i === 0 || p.key === 'all' || p.key === selected) return true;
    const need = p.key === 'ytd' ? now.getTime() - new Date(now.getFullYear(), 0, 1).getTime() : p.ms;
    return have >= need;
  });
}

/** The period in words, for a line under a figure: "the last 7 days". */
export function periodWords(key: PeriodKey): string {
  switch (key) {
    case '1h':
      return 'the last hour';
    case '3h':
      return 'the last 3 hours';
    case '8h':
      return 'the last 8 hours';
    case '24h':
      return 'the last 24 hours';
    case '3d':
      return 'the last 3 days';
    case '7d':
      return 'the last 7 days';
    case '14d':
      return 'the last 14 days';
    case '30d':
      return 'the last 30 days';
    case 'ytd':
      return 'this year so far';
    case '1y':
      return 'the last year';
    default:
      return 'all recorded time';
  }
}

/** Whether a draw is shown in kilowatts rather than watts: the point at which a figure is big enough to stand out. */
export function isKilowatts(w: number | null | undefined): boolean {
  return w !== null && w !== undefined && Math.abs(w) >= 1000;
}

export function formatWatts(w: number | null | undefined): string {
  if (w === null || w === undefined) return '—';
  return isKilowatts(w) ? `${(w / 1000).toFixed(2)} kW` : `${Math.round(w)} W`;
}

/** Whether an amount of energy is written in kilowatt-hours (not watt-hours, and not nothing at all): the point at which it is shown in the hot colour. */
export function isKilowattHours(k: number | null | undefined): boolean {
  return k !== null && k !== undefined && Math.abs(k) >= 0.01;
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

/** A money string with the currency's sign and two decimals. `estimated` marks a price that rests on a stand-in rate with a leading ≈. An amount that is
 *  more than nothing but under half a cent is "<$0.01", never "$0.00": the server sends it to the sixth decimal, so a real zero is told from a rounded one. */
export function formatMoney(m: string | null | undefined, estimated = false): string {
  if (m === null || m === undefined) return '—';
  const n = Number(m);
  if (!Number.isFinite(n)) return '—';
  const sign = n < 0 ? '-' : '';
  const shown = Math.abs(n).toFixed(2);
  if (n !== 0 && Number(shown) === 0) return `${estimated ? '≈ ' : ''}<${symbol}0.01`;
  return `${estimated ? '≈ ' : ''}${sign}${symbol}${shown}`;
}

/** A cost per mile: three decimals, because a mile of electricity costs a few cents. */
export function formatPerMile(m: string | null | undefined, estimated = false): string {
  if (m === null || m === undefined) return '—';
  const n = Number(m);
  if (!Number.isFinite(n)) return '—';
  return `${estimated ? '≈ ' : ''}${symbol}${Math.abs(n).toFixed(3)}/mi`;
}

/** Above this per kWh a rate is almost certainly cents typed as dollars. */
export const SUSPICIOUSLY_HIGH_RATE = 1;

export interface Row {
  meter: Meter;
  watts: number;
  /** How deep in a sub-panel this circuit sits: 0 for a circuit of the main panel (or one with no panel shown). */
  depth?: number;
  /** A line for what a sub-panel's feed carries that none of its circuits account for. `meter` is the feed; `of` lists its circuits' ids. */
  remainder?: { of: string[] };
}

const byId = (meters: Meter[]) => new Map(meters.map((c) => [c.id, c]));

/** Enabled circuits and plugs with their live watts, highest first. Mains and the PC slice are not in it. */
export function circuitsByWatts(meters: Meter[], readings: Reading[]): Row[] {
  const ch = byId(meters);
  const rows: Row[] = [];
  for (const r of readings) {
    const c = ch.get(r.meter_id);
    if (c && c.enabled && (c.scope === 'circuit' || c.scope === 'plug')) rows.push({ meter: c, watts: r.watts });
  }
  return rows.sort((a, b) => b.watts - a.watts);
}

/** Lays rows out as the panels are wired: a circuit that feeds a sub-panel is followed by the circuits inside it (indented one level, highest
 * first), then by a line for what the feed carries that none of them account for. A circuit whose feed is not among the rows (switched off, no
 * reading) stands on its own. A feed already contains its circuits, so none of this is ever added up. */
export function nestCircuits(rows: Row[]): Row[] {
  const present = new Map(rows.map((r) => [r.meter.id, r]));
  const kids = new Map<string, Row[]>();
  const roots: Row[] = [];
  for (const r of rows) {
    const p = r.meter.parent_id;
    if (p && p !== r.meter.id && present.has(p)) kids.set(p, [...(kids.get(p) ?? []), r]);
    else roots.push(r);
  }
  const out: Row[] = [];
  const seen = new Set<string>();
  const walk = (r: Row, depth: number) => {
    if (seen.has(r.meter.id)) return; // a loop in the wiring cannot happen (the server refuses it), but never spin on one
    seen.add(r.meter.id);
    out.push({ ...r, depth });
    const mine = (kids.get(r.meter.id) ?? []).sort((a, b) => b.watts - a.watts);
    for (const k of mine) walk(k, depth + 1);
    if (mine.length > 0) {
      const inside = mine.reduce((t, k) => t + k.watts, 0);
      out.push({ meter: r.meter, watts: Math.max(0, r.watts - inside), depth: depth + 1, remainder: { of: mine.map((k) => k.meter.id) } });
    }
  };
  for (const r of roots.sort((a, b) => b.watts - a.watts)) walk(r, 0);
  return out;
}

/** Orders meters as the panels are wired: a circuit that feeds a sub-panel is followed by the circuits inside it. The order the meters came
 * in is kept at every level. A meter whose feed is not in the list stands at the top. */
export function arrangeByPanel(list: Meter[]): { meter: Meter; depth: number }[] {
  const ids = new Set(list.map((c) => c.id));
  const kids = new Map<string, Meter[]>();
  const top: Meter[] = [];
  for (const c of list) {
    const p = c.parent_id;
    if (p && p !== c.id && ids.has(p)) kids.set(p, [...(kids.get(p) ?? []), c]);
    else top.push(c);
  }
  const out: { meter: Meter; depth: number }[] = [];
  const seen = new Set<string>();
  const walk = (c: Meter, depth: number) => {
    if (seen.has(c.id)) return;
    seen.add(c.id);
    out.push({ meter: c, depth });
    for (const k of kids.get(c.id) ?? []) walk(k, depth + 1);
  };
  for (const c of top) walk(c, 0);
  return out;
}

/** Puts the whole-house meter on a line of its own with everything else one level beneath it: a circuit with no parent is on the main panel, so it is
 * inside what the house clamps measure. Only when exactly one house meter is shown; with none, or several, nothing says which one a top-level
 * circuit hangs from, so the rows are left as they are. The house is never a parent in the data (the server refuses it): this is only how the tree reads. */
export function underHouse(rows: { meter: Meter; depth: number }[]): { meter: Meter; depth: number }[] {
  const houses = rows.filter((r) => r.meter.scope === 'mains');
  const house = houses[0];
  if (houses.length !== 1 || !house || house.depth !== 0) return rows;
  return [house, ...rows.filter((r) => r !== house).map((r) => ({ ...r, depth: r.depth + 1 }))];
}

/** The name a Refoss input has on the monitor itself (A1 to C6), or null for anything else. */
export function inputName(m: Meter): string | null {
  const n = m.index;
  if (!m.device_id.startsWith('refoss-') || n == null || n < 1 || n > 18) return null;
  return `${'ABC'[Math.floor((n - 1) / 6)]}${((n - 1) % 6) + 1}`;
}

/** What to call a meter's clamp: its input on the monitor, else its own name. */
export const clampName = (m: Meter): string => inputName(m) ?? m.label;

/** The meters a hierarchy row can show: not hidden, and not the second clamp of a breaker (that is part of the first one's row). */
export function shownMeters(all: Meter[]): Meter[] {
  const second = new Set(all.flatMap((m) => (m.pair_with ? [m.pair_with] : [])));
  return all.filter((m) => m.enabled && !second.has(m.id));
}

/** The input that is usually the other half of this one's 240 V breaker on an EM16P: the same number on the other phase (A1 with B1). */
export function usualPartner(m: Meter, candidates: Meter[]): Meter | undefined {
  const n = m.index;
  if (!m.device_id.startsWith('refoss-') || n == null || n < 1 || n > 12) return undefined;
  const other = n <= 6 ? n + 6 : n - 6;
  return candidates.find((c) => c.device_id === m.device_id && c.index === other);
}

/** The ids of a meter and everything inside it, however deep. */
export function withDescendants(id: string, all: Meter[]): Set<string> {
  const inside = new Set<string>([id]);
  for (let grew = true; grew; ) {
    grew = false;
    for (const c of all) {
      if (c.parent_id && inside.has(c.parent_id) && !inside.has(c.id)) {
        inside.add(c.id);
        grew = true;
      }
    }
  }
  return inside;
}

/** Whether `parent` can hold `child`: a shown circuit that is not the child itself or inside it. */
export function canHold(parent: Meter, child: Meter, all: Meter[]): boolean {
  return parent.scope === 'circuit' && parent.enabled && child.scope !== 'mains' && !withDescendants(child.id, all).has(parent.id);
}

/** The meters to offer in a meter's "Panel" (a circuit) or "Circuit" (a plug) list, besides the main panel: for a circuit, the circuits that are sub-panels, by their
 * `type` (the user's choice, or what the server settles from a circuit holding circuits: no name or id is looked at), and the one it is in now so the list can
 * show it; for a plug, any circuit. Nothing that is the meter itself or inside it (`canHold`). */
export function panelChoices(meter: Meter, all: Meter[]): Meter[] {
  return all.filter((c) => canHold(c, meter, all) && (meter.scope !== 'circuit' || c.type === 'sub_panel' || c.id === meter.parent_id));
}

/** Where the "move into the meter above" arrow goes: the row just above at the same level, when it can hold this one. */
export function indentTarget(rows: { meter: Meter; depth: number }[], at: number, all: Meter[]): Meter | undefined {
  const me = rows[at];
  if (!me) return undefined;
  for (let i = at - 1; i >= 0; i--) {
    const r = rows[i]!;
    if (r.depth < me.depth) return undefined;
    if (r.depth === me.depth) return canHold(r.meter, me.meter, all) ? r.meter : undefined;
  }
  return undefined;
}

/** Where the "move out one level" arrow goes: the parent's own parent, or null for the top. Undefined when it is already at the top. */
export function outdentTarget(m: Meter, all: Meter[]): { parent: string | null } | undefined {
  if (!m.parent_id) return undefined;
  const up = all.find((c) => c.id === m.parent_id);
  return { parent: up?.parent_id ?? null };
}

/** The PC's own wall plugs, kept apart from everything else. */
export function pcSlices(meters: Meter[], readings: Reading[]): Row[] {
  const ch = byId(meters);
  const rows: Row[] = [];
  for (const r of readings) {
    const c = ch.get(r.meter_id);
    if (c && c.enabled && c.scope === 'pc') rows.push({ meter: c, watts: r.watts });
  }
  return rows;
}

/** What no circuit clamp accounts for: the house minus the circuits of the main panel, never below zero. Null without a house figure.
 * A circuit inside a sub-panel is already in its feed's figure, so only the top level is taken off. */
export function unmetered(house: number | null | undefined, circuits: Row[]): number | null {
  if (house === null || house === undefined) return null;
  const sum = circuits.reduce((t, r) => t + (r.meter.scope === 'circuit' && !r.remainder && (r.depth ?? 0) === 0 ? r.watts : 0), 0);
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
  circuits: MeterSummary[];
  pc: MeterSummary[];
  houseKwh: number | null;
  unmeteredKwh: number | null;
}

/** Splits a summary by scope without adding any scope to another. */
export function splitSummary(s: Summary): Split {
  return {
    circuits: s.meters.filter((c) => c.scope === 'circuit' || c.scope === 'plug').sort((a, b) => b.kwh - a.kwh),
    pc: s.meters.filter((c) => c.scope === 'pc'),
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

/** A cost worked out from other figures, as text in cents. A sliver of a cent keeps its extra places (six) instead of rounding to "0.00": `formatMoney` shows
 * that as "<$0.01", but would show a rounded "0.00" as no cost at all. Float dust (1e-17 left over from a subtraction) is still nothing. */
export function moneyText(n: number): string {
  const cents = n.toFixed(2);
  return n !== 0 && Number(cents) === 0 ? n.toFixed(6) : cents;
}

/** Adds money strings for display. Null if any is missing (a partly priced total would mislead). */
export function sumMoney(parts: (string | null | undefined)[]): string | null {
  if (parts.length === 0 || parts.some((p) => p === null || p === undefined)) return null;
  return moneyText(parts.reduce((t, p) => t + Number(p), 0));
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
      return { name: p.name, kwh: p.kwh, cost: p.cost ?? null, note: 'CPU and GPU idle power', share: share(p.kwh), kind: 'baseline' };
    if (p.name === UNATTRIBUTED)
      return { name: p.name, kwh: p.kwh, cost: p.cost ?? null, note: 'above idle, no program reported', share: share(p.kwh), kind: 'unattributed' };
    if (p.name === OTHERS)
      return { name: p.name, kwh: p.kwh, cost: p.cost ?? null, note: `many small programs${active > 0 ? ` · active up to ${formatDuration(active)}` : ''}`, share: share(p.kwh), kind: 'program' };
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
      note: 'each too small to list', share: share(kwh), kind: 'rollup',
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
    parts.push(`CPU, GPU, memory ${formatKwh(p.sensors_kwh)}${p.kwh > 0 && p.sensors_kwh > 0 ? ` (${Math.round((p.sensors_kwh / p.kwh) * 100)}%)` : ''}`);
  } else {
    parts.push('Sensors only');
  }
  if (p.asleep_seconds > 0) parts.push(`asleep ${formatDuration(p.asleep_seconds)}`);
  if (p.off_seconds > 0) parts.push(`off ${formatDuration(p.off_seconds)}`);
  if (p.lost_power_seconds > 0) parts.push(`lost power ${formatDuration(p.lost_power_seconds)}`);
  return parts.join(' · ');
}

/** The key of the PC itself among the things a computer's figures can count (the plugs next to it are keyed by meter id). */
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
  detail: Pick<ComputerDetail, 'wall' | 'peripherals'>, off: ReadonlySet<string>, label: (meterId: string) => string,
): CountedTotals {
  const counted: { summary: MeterSummary; name: string }[] = [];
  if (detail.wall && !off.has(COUNT_PC)) counted.push({ summary: detail.wall, name: 'the whole PC at the wall' });
  for (const p of detail.peripherals) if (!off.has(p.meter_id)) counted.push({ summary: p, name: label(p.meter_id) });
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
  { key: '24h', label: 'Last 24 hours', days: 1 },
  { key: '7d', label: 'Last 7 days', days: 7 },
  { key: '30d', label: 'Last 30 days', days: 30 },
  { key: 'all', label: 'All recorded time', days: null },
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
  const covered = short ? `${formatDuration(use.days * 86400)} recorded` : w.label;
  const plugs = `${use.peripheral_count} peripheral${use.peripheral_count === 1 ? '' : 's'}`;
  const what = !use.includes_pc
    ? plugs
    : `${use.whole_pc ? 'whole PC at the wall' : 'sensors only'}${use.peripheral_count > 0 ? ` + ${plugs}` : ''}`;
  return `${covered}: ${formatKwh(use.kwh)}, ${formatKwh(use.kwh_per_day)} a day, avg ${formatWatts(use.average_watts)} · ${what}`;
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

/** What a looked-up price comes to over a period, in words: the rate and what the period averaged to per kWh. With no energy to average over it says
 * only which rate it is. */
const COMPANY_WORDS = new Set(['co', 'co.', 'company', 'corp', 'corp.', 'corporation', 'inc', 'inc.', 'llc', 'lp']);
const SMALL_WORDS = new Set(['of', 'the', 'and', '&']);

/** A utility's name short enough for a tile: what follows " - " (its state or region) and a closing "Company" are dropped, and three words or more become their
 * initials ("Northern States Power Co - Minnesota" gives "NSP"); a short name stays as it is. */
export function shortUtility(name: string): string {
  const words = name.split(' - ')[0]!.trim().split(/\s+/).filter(Boolean);
  while (words.length > 1 && COMPANY_WORDS.has(words[words.length - 1]!.toLowerCase())) words.pop();
  if (words.length < 3) return words.join(' ');
  return words.filter((w) => !SMALL_WORDS.has(w.toLowerCase())).map((w) => w[0]!.toUpperCase()).join('');
}

/** A rate's name short enough for a tile: what follows " - " is dropped and a closing "Service" or "Rate" too, but its code in brackets stays
 * ("Residential Service - Overhead Standard (A01)" gives "Residential (A01)"). */
export function shortRateName(name: string): string {
  const code = /\(([^()]+)\)\s*$/.exec(name)?.[0];
  const words = name.split(' - ')[0]!.replace(/\([^()]*\)\s*$/, '').trim().split(/\s+/).filter(Boolean);
  while (words.length > 1 && ['service', 'rate'].includes(words[words.length - 1]!.toLowerCase())) words.pop();
  return [words.join(' '), code].filter(Boolean).join(' ');
}

export function tariffFacts(t: Pick<TariffInfo, 'utility' | 'name' | 'varies_by_time'>, kwh: number, cost: string | null): Fact[] {
  const avg = cost != null && kwh > 0 ? Number(cost) / kwh : null;
  const full = `${t.utility} · ${t.name}`;
  const short = `${shortUtility(t.utility)} · ${shortRateName(t.name)}`;
  const facts: Fact[] = [short === full ? { label: 'Rate', value: full } : { label: 'Rate', value: short, full }];
  if (avg != null && Number.isFinite(avg)) facts.push({ label: 'Average price', value: `${symbol}${avg.toFixed(4)} per kWh${t.varies_by_time ? ', time-weighted' : ''}` });
  return facts;
}

/** An amount of carbon in words, always approximate: "≈ 412 g CO₂e", "≈ 1.24 kg CO₂e", "≈ 2.1 t CO₂e". */
export function formatCarbon(grams: number): string {
  const g = Math.abs(grams);
  if (g < 1000) return `≈ ${Math.round(grams)} g CO₂e`;
  if (g < 1_000_000) return `≈ ${(grams / 1000).toFixed(g < 10_000 ? 2 : 1)} kg CO₂e`;
  return `≈ ${(grams / 1_000_000).toFixed(1)} t CO₂e`;
}

/** The carbon figures of a page: the amount and the energy-weighted grid average it came to. Empty when there is no carbon to show. */
export function carbonFacts(carbonG: number | null | undefined, kwh: number | null | undefined): Fact[] {
  if (carbonG == null || kwh == null) return [];
  const facts: Fact[] = [{ label: 'Carbon', value: formatCarbon(carbonG) }];
  if (kwh > 0) facts.push({ label: 'Grid average', value: `${Math.round(carbonG / kwh)} g/kWh` });
  return facts;
}

/** Where a house "my usage" estimate comes from, in words: the window and that every hour is in it. */
export function houseUsageBasis(use: AverageUse, windowKey: string): string {
  const w = ESTIMATE_WINDOWS.find((x) => x.key === windowKey) ?? ESTIMATE_WINDOWS[1]!;
  const short = w.days == null || use.days < w.days * 0.95;
  const covered = short ? `${formatDuration(use.days * 86400)} recorded` : w.label;
  return `${covered}: ${formatKwh(use.kwh)}, ${formatKwh(use.kwh_per_day)} a day, avg ${formatWatts(use.average_watts)} · mains clamps`;
}

/** One meter's button in the History tab's compare list. */
export interface CompareEntry {
  meter: Meter;
  /** False when the meter drew nothing in the period (nothing recorded, or an average that rounds to 0 W): it has no line worth drawing. */
  usable: boolean;
  kwh: number;
  /** The average draw while it was recorded, in watts; null while the period's figures are still being read. */
  avgWatts: number | null;
  cost: string | null;
  costEstimated: boolean;
}

/** The meters to compare with the house, as the list shows them: the ones with something recorded in the period first, the biggest user first, then the ones
 * that drew nothing (greyed out), by name. Until the period's figures arrive every meter is usable and keeps the order it came in. */
export function compareOrder(meters: Meter[], summary: Summary | undefined): CompareEntry[] {
  if (!summary) return meters.map((meter) => ({ meter, usable: true, kwh: 0, avgWatts: null, cost: null, costEstimated: false }));
  const figures = new Map(summary.meters.map((f) => [f.meter_id, f]));
  const entries = meters.map((meter): CompareEntry => {
    const f = figures.get(meter.id);
    const hours = f?.monitored_hours ?? 0;
    const avgWatts = hours > 0 ? ((f?.kwh ?? 0) * 1000) / hours : null;
    return { meter, usable: avgWatts != null && avgWatts >= 0.5, kwh: f?.kwh ?? 0, avgWatts, cost: f?.cost ?? null, costEstimated: summary.cost_estimated ?? false };
  });
  return entries.sort((a, b) => Number(b.usable) - Number(a.usable) || b.kwh - a.kwh || a.meter.label.localeCompare(b.meter.label));
}

/** The share of the house a part of it used, as a whole-number percent; null when the house figure is missing or zero, or when the part is more than
 * the whole (the house was not recorded for all the time the part was, as over "all time" with a monitor added later: no share is true then). */
export function houseShare(kwh: number, houseKwh: number | null | undefined): number | null {
  if (houseKwh == null || houseKwh <= 0) return null;
  const share = Math.round((kwh / houseKwh) * 100);
  return share > 100 ? null : share;
}

/** The share as the table writes it: "17%", "<1%" for a part that used something but under half a percent (a plain "0%" would say it used nothing), "0%" only for
 * a part that used nothing, and nothing when houseShare has none. */
export function formatShare(kwh: number, houseKwh: number | null | undefined): string {
  const share = houseShare(kwh, houseKwh);
  if (share == null) return '';
  return share === 0 && kwh > 0 ? '<1%' : `${share}%`;
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
