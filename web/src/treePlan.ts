// What the Hierarchy tab lists, in order, as one plan that the tree and the diagram both draw, so they cannot disagree. With one whole-house meter the plan
// reads as the Live page's circuits table does: the house, the circuits clamped on the main panel (sub-panels and what is inside them), then "Everything
// else" (the house less those circuits) holding the plugs and computers that name no circuit they are on, and last, inside it, "Not individually metered"
// (what none of them explains). Without one house meter the rows are left as they are. A lump is read-only and never added to anything.
import type { Meter, Summary } from './lib';
import { notMeteredRows } from './notMetered';
import type { PcPart } from './pcparts';
import type { RestRow, TreeRow } from './treeRest';

/** A line that stands for no meter of its own: a figure worked out from others. */
export interface LumpEntry {
  kind: 'lump';
  id: string;
  label: string;
  title: string;
  tag: string;
  depth: number;
  /** Power right now; null when it is not known. */
  watts: number | null;
  kwh: number | null;
  cost: string | null;
}

export type Entry =
  | { kind: 'row'; id: string; meter: Meter; /** Its place in the rows the tree logic (move in, move out) reads. */ at: number; depth: number }
  | { kind: 'part'; id: string; part: PcPart; owner: Meter; href: string; depth: number }
  | LumpEntry;

export interface PlanArgs {
  rows: TreeRow[];
  watts: Map<string, number>;
  houseWatts: number | null | undefined;
  summary: Summary | undefined;
  rests: RestRow[];
  /** A computer's CPU, GPU and memory under its wall plug, and the page they open. */
  partsOf: (m: Meter) => { parts: PcPart[]; href: string } | undefined;
}

const ELSE_TITLE = 'The house less the circuits clamped on the main panel. The plugs and computers listed inside it are already in it, never added to it';
const LEFT_TITLE = 'What none of the plugs and computers listed above accounts for';

export function planTree({ rows, watts, houseWatts, summary, rests, partsOf }: PlanArgs): Entry[] {
  const out: Entry[] = [];
  const first = rows[0];
  const house = first && first.meter.scope === 'mains' && first.depth === 0 && rows.filter((r) => r.meter.scope === 'mains').length === 1 ? first.meter : undefined;

  const restsAfter = new Map<number, RestRow[]>();
  for (const r of rests) if (!(house && r.parent.id === house.id)) restsAfter.set(r.after, [...(restsAfter.get(r.after) ?? []), r]);

  /** The rows `from` to `to`, each with what hangs under it, `shift` levels deeper than the tree logic reads them. */
  const emit = (from: number, to: number, shift: number) => {
    for (let i = from; i <= to; i++) {
      const { meter, depth } = rows[i]!;
      out.push({ kind: 'row', id: `row:${meter.id}`, meter, at: i, depth: depth + shift });
      const under = partsOf(meter);
      for (const part of under?.parts ?? []) out.push({ kind: 'part', id: `part:${meter.id}:${part.key}`, part, owner: meter, href: under!.href, depth: depth + shift + 1 });
      for (const r of restsAfter.get(i) ?? []) {
        const title = r.parent.scope === 'mains'
          ? 'What the whole house used that none of the circuits and plugs listed under it accounts for'
          : `What ${r.parent.label} carried beyond the meters listed inside it`;
        out.push({ kind: 'lump', id: `rest:${r.parent.id}`, label: 'Everything else', title, tag: 'remainder', depth: r.depth + shift, watts: r.watts, kwh: r.kwh, cost: r.cost });
      }
    }
  };

  if (!house) {
    emit(0, rows.length - 1, 0);
    return out;
  }

  // Each top-level block is a row and everything inside it. A block whose head is a clamped circuit stays; the rest (plugs, a computer's wall plug) go under "Everything else".
  const clamped: [number, number][] = [];
  const loose: [number, number][] = [];
  for (let i = 1; i < rows.length; ) {
    let to = i;
    while (rows[to + 1] && rows[to + 1]!.depth > rows[i]!.depth) to++;
    (rows[i]!.meter.scope === 'circuit' ? clamped : loose).push([i, to]);
    i = to + 1;
  }
  const head = ([i]: [number, number]) => ({ meter: rows[i]!.meter, watts: watts.get(rows[i]!.meter.id) ?? 0, depth: 0 });
  const lumps = notMeteredRows({ house: houseWatts ?? 0, clamped: clamped.map(head), plugs: loose.map(head), summary, host: house });
  const now = (w: number) => (houseWatts == null ? null : w);

  emit(0, 0, 0);
  for (const [from, to] of clamped) emit(from, to, 0);
  const group = lumps[0]!;
  out.push({ kind: 'lump', id: 'else', label: group.lump!.label, title: ELSE_TITLE, tag: 'house less circuits', depth: 1, watts: now(group.watts), kwh: group.lump!.kwh, cost: group.lump!.cost });
  for (const [from, to] of loose) emit(from, to, 1);
  const left = lumps[lumps.length - 1]!;
  if (lumps.length > 1 && left.lump) {
    out.push({ kind: 'lump', id: 'left', label: left.lump.label, title: LEFT_TITLE, tag: 'remainder', depth: 2, watts: now(left.watts), kwh: left.lump.kwh, cost: left.lump.cost });
  }
  return out;
}
