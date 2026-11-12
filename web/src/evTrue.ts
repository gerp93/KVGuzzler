// The truest cost of a mile, charge to charge. Charging buys energy before the miles that use it are driven (as a tank is filled before the gas is burned), so
// counting the energy of a stretch against that stretch's miles is wrong in the short run. Here every figure is cut at the same moments instead: the ends of
// charges, where the car reported its battery's charge. Between two of them the battery's own change is taken out of what was bought, so what is left is what
// the miles driven in between used; the gas cars are priced for the same miles. Pure, so it is tested apart from the drawing; the prices are the server's.
import type { EvCharge, EvChargesAnswer, EvSavings } from './ev';
import { compKey, type SavingsSummary } from './evGas';

type GasComparison = EvSavings['comparisons'][number];
type Stretch = EvSavings['stretches'][number];

const DAY_S = 86_400;
const DAY_MS = 86_400_000;

/** A charge whose end the car's battery report can be trusted at: it ended, and the car reported after the end. Preconditioning and top-ups count: they end at a known level too. */
export const hasEndLevel = (c: Pick<EvCharge, 'ended_at' | 'end_pct' | 'pct_status'>): boolean =>
  c.ended_at != null && c.end_pct != null && (c.pct_status === 'ok' || c.pct_status === 'small_gain');

/** A moment as a place on the calendar of days, counted in days from the epoch in this device's own time: the day it falls on, and how far through it. */
export function dayPosition(iso: string): number {
  const d = new Date(iso);
  return Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()) / DAY_MS + (d.getHours() * 3600 + d.getMinutes() * 60 + d.getSeconds()) / DAY_S;
}

interface Day {
  /** The start of the day, in days from the epoch. */
  at: number;
  miles: number;
  /** What each car would have burned that day (its stretch's cost shared evenly over the stretch's days); a car with no cost is missing. */
  gas: Map<string, number>;
}

/** The odometer's days: a stretch between two readings is shared evenly over its days, as the rolling-week charts do (the odometer is only read now and then). */
function daysOf(stretches: Stretch[], comparisons: GasComparison[]): Day[] {
  const days: Day[] = [];
  for (const s of stretches) {
    const from = Date.parse(`${s.from_day}T00:00:00Z`);
    const n = Math.round((Date.parse(`${s.to_day}T00:00:00Z`) - from) / DAY_MS);
    if (!(n >= 1)) continue;
    const gas = new Map<string, number>();
    for (const c of comparisons) {
      const g = s.gas.find((e) => e.fuel === c.fuel && e.mpg === c.mpg);
      if (g?.gas_cost != null) gas.set(compKey(c), Number(g.gas_cost) / n);
    }
    for (let i = 0; i < n; i++) days.push({ at: from / DAY_MS + i, miles: s.miles / n, gas });
  }
  return days;
}

/** The miles driven and what each car would have burned between two places on the calendar; null when the odometer's days do not cover all of it or a car has no price there. */
function between(days: Day[], a: number, b: number, keys: string[]): { miles: number; gas: Map<string, number> } | null {
  let covered = 0;
  let miles = 0;
  const gas = new Map<string, number>(keys.map((k) => [k, 0]));
  for (const d of days) {
    const share = Math.min(b, d.at + 1) - Math.max(a, d.at);
    if (share <= 0) continue;
    covered += share;
    miles += d.miles * share;
    for (const k of keys) {
      const g = d.gas.get(k);
      if (g == null) return null;
      gas.set(k, gas.get(k)! + g * share);
    }
  }
  return covered >= b - a - 1e-6 ? { miles, gas } : null;
}

/** Everything that happened between two charges whose ends the car reported. */
export interface Cycle {
  /** When the charge before it ended, in seconds from the epoch. */
  from: number;
  /** When the closing charge ended. */
  at: number;
  miles: number;
  /** What the wall gave in the charges of the cycle. */
  boughtKwh: number;
  /** What the miles used: what the wall gave, less the energy the battery gained over the cycle (negative gain: plus the energy it drew). */
  usedKwh: number;
  /** What `usedKwh` cost, at the average price of the cycle's charges. */
  usedCost: number;
  /** What each gas car would have burned over the same miles, by `compKey`. */
  gas: Map<string, number>;
}

export interface TrueCost {
  cycles: Cycle[];
  /** Added up from the first counted cycle to each one: one point per cycle, the first at zero (the end of the first charge). */
  cumulative: { at: number[]; miles: number[]; ev: number[]; gas: Map<string, number[]> };
  /** The wall energy each percent of the battery takes, which the battery's change is set against. */
  kwhPerPct: number | null;
  /** Why there are no cycles, in a few words; null when there are some. */
  reason: string | null;
}

/**
 * The cycles between the ends of charges. The first charge whose end level is known is the starting line; from there every charge whose end level is known closes
 * a cycle (charges in between with none are folded into the cycle that follows them). A cycle is counted only when every price is known, the odometer's days
 * cover it, the miles are not negative and what the miles used is not negative, so a noisy battery report cannot make a free or paying mile out of nothing; a
 * counted cycle is counted for every car and charging alike.
 */
export function trueCost(input: { charges: EvChargesAnswer | undefined; savings: EvSavings | undefined }): TrueCost {
  const { charges, savings } = input;
  const empty = (reason: string): TrueCost => ({ cycles: [], cumulative: { at: [], miles: [], ev: [], gas: new Map() }, kwhPerPct: null, reason });
  if (!savings?.configured || savings.comparisons.length === 0) return empty('Needs the gas comparison set up.');
  const kwhPerPct = charges?.consistency?.kwh_per_pct ?? null;
  if (kwhPerPct == null || kwhPerPct <= 0) return empty('Needs a charge with the car’s battery reported before and after it, to know what each percent costs.');
  const list = [...(charges?.charges ?? [])].filter((c) => c.ended_at != null).sort((a, b) => a.ended_at!.localeCompare(b.ended_at!));
  const days = daysOf(savings.stretches, savings.comparisons);
  const keys = savings.comparisons.map(compKey);

  const cycles: Cycle[] = [];
  let anchor: EvCharge | null = null;
  let kwh = 0;
  let cost = 0;
  let priced = true;
  for (const c of list) {
    if (anchor != null) {
      kwh += c.kwh;
      if (c.cost == null) priced = false;
      else cost += Number(c.cost);
    }
    if (!hasEndLevel(c)) continue;
    if (anchor == null) {
      anchor = c;
      continue;
    }
    const span = between(days, dayPosition(anchor.ended_at!), dayPosition(c.ended_at!), keys);
    const used = kwh - (c.end_pct! - anchor.end_pct!) * kwhPerPct;
    if (span != null && priced && kwh > 0 && used >= 0) {
      cycles.push({ from: Date.parse(anchor.ended_at!) / 1000, at: Date.parse(c.ended_at!) / 1000, miles: span.miles, boughtKwh: kwh, usedKwh: used, usedCost: (cost * used) / kwh, gas: span.gas });
    }
    anchor = c;
    kwh = 0;
    cost = 0;
    priced = true;
  }
  if (cycles.length === 0) return empty(anchor == null ? 'Needs a charge whose end the car reported.' : 'Needs a second charge, and odometer readings that cover the time between them.');

  const first = cycles[0]!;
  const at = [first.from];
  const miles = [0];
  const ev = [0];
  const gas = new Map<string, number[]>(keys.map((k) => [k, [0]]));
  for (const c of cycles) {
    at.push(c.at);
    miles.push(miles[miles.length - 1]! + c.miles);
    ev.push(ev[ev.length - 1]! + c.usedCost);
    for (const k of keys) gas.get(k)!.push(gas.get(k)![gas.get(k)!.length - 1]! + (c.gas.get(k) ?? 0));
  }
  return { cycles, cumulative: { at, miles, ev, gas }, kwhPerPct, reason: null };
}

/** A cost over miles at each point of a running total; null while no mile is driven (nothing to divide by). */
export const perMileSoFar = (cost: number[], miles: number[]): (number | null)[] => cost.map((c, i) => (miles[i]! > 0 ? c / miles[i]! : null));

/** Everything the counted cycles add up to. */
export interface TrueTotals {
  miles: number;
  /** What the wall gave for those miles, cut at the battery's change. */
  usedKwh: number;
  /** What `usedKwh` cost. */
  evCost: number;
  /** What each gas car would have burned over the same miles, by `compKey`. */
  gas: Map<string, number>;
}

/** The counted cycles added up; null when none is counted or no mile was driven in them (nothing to divide by). */
export function trueTotals(t: TrueCost): TrueTotals | null {
  const miles = t.cycles.reduce((s, c) => s + c.miles, 0);
  if (t.reason != null || miles <= 0) return null;
  const gas = new Map<string, number>();
  for (const [k, line] of t.cumulative.gas) gas.set(k, line[line.length - 1] ?? 0);
  return { miles, usedKwh: t.cycles.reduce((s, c) => s + c.usedKwh, 0), evCost: t.cumulative.ev[t.cumulative.ev.length - 1] ?? 0, gas };
}

/** What the headline tiles say over the cars drawn, from the counted cycles: the same shape as the stretch-based summary, so the tiles read either. `visible` are the `compKey`s drawn. */
export function trueSummary(t: TrueCost, comparisons: GasComparison[], visible: string[]): SavingsSummary | null {
  const totals = trueTotals(t);
  const cars = comparisons.filter((c) => visible.includes(compKey(c)) && totals?.gas.has(compKey(c)));
  if (!totals || cars.length === 0) return null;
  const gas = cars.map((c) => totals.gas.get(compKey(c))!);
  const mpgs = cars.map((c) => c.mpg);
  return {
    count: cars.length,
    gasCount: cars.filter((c) => c.fuel === 'gas').length,
    dieselCount: cars.filter((c) => c.fuel === 'diesel').length,
    lowest: Math.min(...gas) - totals.evCost,
    highest: Math.max(...gas) - totals.evCost,
    mpgLow: Math.min(...mpgs),
    mpgHigh: Math.max(...mpgs),
    gasLow: Math.min(...gas),
    gasHigh: Math.max(...gas),
    perMileLow: Math.min(...gas) / totals.miles,
    perMileHigh: Math.max(...gas) / totals.miles,
    evCost: totals.evCost,
    miles: totals.miles,
  };
}
