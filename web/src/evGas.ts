// The gas cars an electric vehicle is compared with, drawn against each other: which of them the viewer has switched off, how each one's line looks, what each
// has cost over the odometer readings, and the range the headline tile gives across the ones drawn. Pure, so it is tested apart from the drawing. The sums
// behind it are the server's; this only adds up what it sent, stretch by stretch, to draw.
import { noon, type EvSavings } from './ev';

export type GasComparison = EvSavings['comparisons'][number];
type Stretch = EvSavings['stretches'][number];
type Totals = NonNullable<EvSavings['totals']>;

/** The gas cars a viewer has switched off, as saved in the browser: a list of miles per gallon. Anything else saved is none. */
export function parseHidden(text: string): number[] {
  try {
    const v: unknown = JSON.parse(text);
    return Array.isArray(v) ? v.filter((x): x is number => typeof x === 'number' && Number.isFinite(x)) : [];
  } catch {
    return [];
  }
}

/** The gas cars still drawn: all but the ones switched off. A new one (a custom car just named) is drawn until the viewer switches it off. */
export const visibleComparisons = (comps: GasComparison[], hidden: number[]): GasComparison[] => comps.filter((c) => !hidden.includes(c.mpg));

/**
 * How a gas car's line looks. The presets are one colour at falling strength, so the thirstiest car is the strongest line and the economy car the faintest and
 * they read as a family, in an order that never changes whichever are switched off; the owner's own car is a colour of its own.
 */
export function gasLineStyle(comp: GasComparison, comps: GasComparison[]): { colorVar: string; alpha: number; width: number } {
  if (comp.custom) return { colorVar: '--color-accent-blue', alpha: 1, width: 2 };
  const presets = comps.filter((c) => !c.custom);
  const i = presets.findIndex((c) => c.mpg === comp.mpg);
  const alpha = presets.length > 1 && i >= 0 ? 1 - (0.55 * i) / (presets.length - 1) : 1;
  return { colorVar: '--color-accent-red', alpha, width: 2 };
}

/** The stretches that count: those with the gas price and the electric cost both known, which are the same for every gas car. */
export const countedStretches = (stretches: Stretch[]): Stretch[] => stretches.filter((s) => s.gas_price != null && s.ev_cost != null);

/**
 * What each gas car would have cost and what charging cost, each added up over the counted stretches, one point at each odometer reading, from zero on the day
 * the first counted stretch began. The savings are the difference, added up the same way.
 */
export function cumulativeCosts(savings: Pick<EvSavings, 'comparisons' | 'stretches'>): { x: number[]; ev: number[]; gas: Map<number, number[]>; saved: Map<number, number[]> } {
  const counted = countedStretches(savings.stretches);
  const first = counted[0];
  const gas = new Map<number, number[]>();
  const saved = new Map<number, number[]>();
  if (!first) return { x: [], ev: [], gas, saved };
  const x = [noon(first.from_day)];
  const ev = [0];
  for (const c of savings.comparisons) {
    gas.set(c.mpg, [0]);
    saved.set(c.mpg, [0]);
  }
  for (const s of counted) {
    x.push(noon(s.to_day));
    ev.push(ev[ev.length - 1]! + Number(s.ev_cost));
    for (const c of savings.comparisons) {
      const g = s.gas.find((e) => e.mpg === c.mpg);
      const line = gas.get(c.mpg)!;
      const sav = saved.get(c.mpg)!;
      line.push(line[line.length - 1]! + Number(g?.gas_cost ?? 0));
      sav.push(sav[sav.length - 1]! + Number(g?.savings ?? 0));
    }
  }
  return { x, ev, gas, saved };
}

/** What the headline tile says over the gas cars drawn: the least and the most saved, and the gas cost and cost per mile to put beside the electric one. */
export interface SavingsSummary {
  count: number;
  /** The least and the most saved across the gas cars drawn (negative where the electric car cost more). */
  lowest: number;
  highest: number;
  /** The gas cars' lowest and highest miles per gallon among those drawn. */
  mpgLow: number;
  mpgHigh: number;
  gasLow: number;
  gasHigh: number;
  perMileLow: number;
  perMileHigh: number;
  evCost: number;
  miles: number;
}

export function savingsSummary(totals: Totals | null, visible: number[]): SavingsSummary | null {
  if (!totals) return null;
  const gas = totals.gas.filter((g) => visible.includes(g.mpg));
  if (gas.length === 0) return null;
  const savings = gas.map((g) => Number(g.savings));
  const costs = gas.map((g) => Number(g.gas_cost));
  const perMile = gas.map((g) => Number(g.gas_cost_per_mile ?? 0));
  const mpgs = gas.map((g) => g.mpg);
  return {
    count: gas.length,
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
