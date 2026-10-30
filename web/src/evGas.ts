// The cars (gas and diesel) an electric vehicle is compared with, drawn against each other: which of them the viewer has ticked, how each one's line looks,
// what each has cost over the odometer readings, and the range the headline tile gives across the ones drawn. Pure, so it is tested apart from the drawing. The
// sums behind it are the server's; this only adds up what it sent, stretch by stretch, to draw.
import { noon, type EvSavings } from './ev';

export type GasComparison = EvSavings['comparisons'][number];
type Stretch = EvSavings['stretches'][number];
type Totals = NonNullable<EvSavings['totals']>;
export type Fuel = GasComparison['fuel'];

/** A car by what it burns and its miles per gallon, which is how the viewer's choices name it. */
export const compKey = (c: { fuel: Fuel; mpg: number }): string => `${c.fuel}:${c.mpg}`;

/** The key of the diesel line on the price chart, among the viewer's choices. */
export const DIESEL_PRICE = 'diesel:price';

/**
 * What the viewer has chosen to draw, kept in the browser. Gas cars are drawn unless switched off (`off`, so a car named later is drawn at first); diesel is
 * optional and drawn only when switched on (`on`), as is the diesel line of the price chart.
 */
export interface Picks {
  off: string[];
  on: string[];
}

const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);

/** The choices as saved. Anything else saved is none: all the gas cars, no diesel. */
export function parsePicks(text: string): Picks {
  try {
    const v: unknown = JSON.parse(text);
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      const o = v as { off?: unknown; on?: unknown };
      return { off: strings(o.off), on: strings(o.on) };
    }
  } catch {
    // not JSON: none
  }
  return { off: [], on: [] };
}

/** What was saved before there was diesel: a list of the gas cars (by miles per gallon) switched off. */
export function picksFromOldHidden(text: string): Picks {
  try {
    const v: unknown = JSON.parse(text);
    if (Array.isArray(v)) return { off: v.filter((x): x is number => typeof x === 'number' && Number.isFinite(x)).map((m) => `gas:${m}`), on: [] };
  } catch {
    // not JSON: none
  }
  return { off: [], on: [] };
}

/** Whether a car is drawn: a gas car unless switched off, a diesel car only if switched on. */
export const isShown = (c: { fuel: Fuel; mpg: number }, picks: Picks): boolean => (c.fuel === 'diesel' ? picks.on.includes(compKey(c)) : !picks.off.includes(compKey(c)));

/** The choices with one box ticked or unticked: a gas car's by the list switched off, anything diesel's by the list switched on. */
export function togglePick(picks: Picks, key: string): Picks {
  const flip = (list: string[]): string[] => (list.includes(key) ? list.filter((k) => k !== key) : [...list, key]);
  return key.startsWith('diesel:') ? { off: picks.off, on: flip(picks.on) } : { off: flip(picks.off), on: picks.on };
}

/** The cars still drawn. */
export const visibleComparisons = (comps: GasComparison[], picks: Picks): GasComparison[] => comps.filter((c) => isShown(c, picks));

/** A car's name on a line and a bar: "25 mpg", and "25 mpg diesel" for a diesel one. */
export const carName = (c: { fuel: Fuel; mpg: number }): string => (c.fuel === 'diesel' ? `${c.mpg} mpg diesel` : `${c.mpg} mpg`);

/**
 * How a car's line looks. The presets of a fuel are one colour at falling strength, so the thirstiest car is the strongest line and the economy car the faintest and
 * they read as a family, in an order that never changes whichever are ticked; the owner's own car is a colour of its own. A diesel car is the same look but dashed,
 * so the fuels are told apart by the dash and never by a second colour to match to a key.
 */
export function gasLineStyle(comp: GasComparison, comps: GasComparison[]): { colorVar: string; alpha: number; width: number; dashed: boolean } {
  const dashed = comp.fuel === 'diesel';
  if (comp.custom) return { colorVar: '--color-accent-blue', alpha: 1, width: 2, dashed };
  const presets = comps.filter((c) => !c.custom && c.fuel === comp.fuel);
  const i = presets.findIndex((c) => c.mpg === comp.mpg);
  const alpha = presets.length > 1 && i >= 0 ? 1 - (0.55 * i) / (presets.length - 1) : 1;
  return { colorVar: '--color-accent-red', alpha, width: 2, dashed };
}

/** The stretches that count: those with every price and the electric cost known, which are the same for every car. */
export const countedStretches = (stretches: Stretch[]): Stretch[] => stretches.filter((s) => s.gas_price != null && s.ev_cost != null && (s.diesel_price != null || s.gas.every((g) => g.fuel === 'gas')));

/**
 * What each car would have cost and what charging cost, each added up over the counted stretches, one point at each odometer reading, from zero on the day the
 * first counted stretch began. The savings are the difference, added up the same way. Keyed by `compKey`.
 */
export function cumulativeCosts(savings: Pick<EvSavings, 'comparisons' | 'stretches'>): { x: number[]; ev: number[]; gas: Map<string, number[]>; saved: Map<string, number[]> } {
  const counted = countedStretches(savings.stretches);
  const first = counted[0];
  const gas = new Map<string, number[]>();
  const saved = new Map<string, number[]>();
  if (!first) return { x: [], ev: [], gas, saved };
  const x = [noon(first.from_day)];
  const ev = [0];
  for (const c of savings.comparisons) {
    gas.set(compKey(c), [0]);
    saved.set(compKey(c), [0]);
  }
  for (const s of counted) {
    x.push(noon(s.to_day));
    ev.push(ev[ev.length - 1]! + Number(s.ev_cost));
    for (const c of savings.comparisons) {
      const g = s.gas.find((e) => e.fuel === c.fuel && e.mpg === c.mpg);
      const line = gas.get(compKey(c))!;
      const sav = saved.get(compKey(c))!;
      line.push(line[line.length - 1]! + Number(g?.gas_cost ?? 0));
      sav.push(sav[sav.length - 1]! + Number(g?.savings ?? 0));
    }
  }
  return { x, ev, gas, saved };
}

/** What the headline tile says over the cars drawn: the least and the most saved, and the fuel cost and cost per mile to put beside the electric one. */
export interface SavingsSummary {
  count: number;
  /** How many of the cars drawn burn each fuel. */
  gasCount: number;
  dieselCount: number;
  /** The least and the most saved across the cars drawn (negative where the electric car cost more). */
  lowest: number;
  highest: number;
  /** The lowest and highest miles per gallon among the cars drawn. */
  mpgLow: number;
  mpgHigh: number;
  gasLow: number;
  gasHigh: number;
  perMileLow: number;
  perMileHigh: number;
  evCost: number;
  miles: number;
}

/** `visible` are the `compKey`s of the cars drawn. */
export function savingsSummary(totals: Totals | null, visible: string[]): SavingsSummary | null {
  if (!totals) return null;
  const cars = totals.gas.filter((g) => visible.includes(compKey(g)));
  if (cars.length === 0) return null;
  const savings = cars.map((g) => Number(g.savings));
  const costs = cars.map((g) => Number(g.gas_cost));
  const perMile = cars.map((g) => Number(g.gas_cost_per_mile ?? 0));
  const mpgs = cars.map((g) => g.mpg);
  return {
    count: cars.length,
    gasCount: cars.filter((g) => g.fuel === 'gas').length,
    dieselCount: cars.filter((g) => g.fuel === 'diesel').length,
    lowest: Math.min(...savings),
    highest: Math.max(...savings),
    mpgLow: Math.min(...mpgs),
    mpgHigh: Math.max(...mpgs),
    gasLow: Math.min(...costs),
    gasHigh: Math.max(...costs),
    perMileLow: Math.min(...perMile),
    perMileHigh: Math.max(...perMile),
    evCost: Number(totals.ev_cost),
    miles: totals.miles,
  };
}

/** What kind of cars a summary stands for, in a phrase: "gas", "diesel" or "gas and diesel". */
export const fuelWords = (s: Pick<SavingsSummary, 'gasCount' | 'dieselCount'>): string => (s.dieselCount === 0 ? 'gas' : s.gasCount === 0 ? 'diesel' : 'gas and diesel');
