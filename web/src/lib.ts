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
