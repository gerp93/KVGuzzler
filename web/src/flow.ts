// The energy flow chart's data and layout, with no drawing in it. A flow chart is a tree laid out left to right: the whole on the left, its
// parts to the right, each band as wide as the energy that went through it. The same rule as everywhere else holds: a parent's own reading
// already contains its children, so what is left over is its own band ("Other (not metered)"); nothing is added twice.
import { shownMeters, sumMoney, type Meter, type Summary } from './lib';

export interface FlowNode {
  id: string;
  label: string;
  /** Energy over the period: the meter's own reading. */
  kwh: number;
  cost: string | null;
  /** The meter it stands for, or null for a derived line ("Other (not metered)", the whole house). */
  meterId: string | null;
  /** The line for what a parent's own reading carries beyond its parts ("Other (not metered)"). */
  rest?: boolean;
  children: FlowNode[];
}

/** Parts a meter has that are not meters of their own (a computer's CPU, GPU and memory), by the meter's id. */
export type VirtualParts = Map<string, { label: string; kwh: number; cost: string | null }[]>;

const rest = (id: string, label: string, kwh: number, cost: string | null): FlowNode => ({ id: `rest-${id}`, label, kwh, cost, meterId: null, rest: true, children: [] });

/** The money left after taking some of it away; null unless every figure is known. */
function moneyLeft(whole: string | null | undefined, parts: (string | null)[]): string | null {
  if (whole == null || parts.some((p) => p === null)) return null;
  return Math.max(0, Number(whole) - parts.reduce((t, p) => t + Number(p), 0)).toFixed(2);
}

/** The tree to draw over a period. With a `rootId`, that meter and everything inside it. Without one, the house: the whole-house clamps as the
 * whole, the top-level circuits as its parts and what no circuit accounts for as the remainder; with no whole-house clamp at all, the
 * top-level circuits added up stand in for it. Undefined when there is nothing recorded to draw. */
export function flowTree(meters: Meter[], summary: Summary, rootId?: string, virtual?: VirtualParts): FlowNode | undefined {
  const shown = shownMeters(meters);
  const byId = new Map(shown.map((m) => [m.id, m]));
  const figures = new Map(summary.meters.map((f) => [f.meter_id, f]));

  const build = (m: Meter, path: Set<string>): FlowNode => {
    const f = figures.get(m.id);
    const here = new Set(path).add(m.id);
    const parts = (virtual?.get(m.id) ?? []).map((p): FlowNode => ({ id: `${m.id}:${p.label}`, label: p.label, kwh: p.kwh, cost: p.cost, meterId: null, children: [] }));
    const children = [
      ...shown.filter((k) => k.parent_id === m.id && !here.has(k.id) && figures.has(k.id)).map((k) => build(k, here)),
      ...parts,
    ].sort((a, b) => b.kwh - a.kwh);
    const own = f?.kwh ?? 0;
    if (children.length > 0) {
      const left = own - children.reduce((t, c) => t + c.kwh, 0);
      if (left > 0) children.push(rest(m.id, 'Other (not metered)', left, moneyLeft(f?.cost, children.map((c) => c.cost))));
    }
    return { id: m.id, label: m.label, kwh: own, cost: f?.cost ?? null, meterId: m.id, children };
  };

  if (rootId) {
    const m = byId.get(rootId);
    return m && figures.has(m.id) ? build(m, new Set()) : undefined;
  }
  const top = shown
    .filter((m) => m.scope === 'circuit' && (!m.parent_id || !byId.has(m.parent_id)) && figures.has(m.id))
    .map((m) => build(m, new Set()))
    .sort((a, b) => b.kwh - a.kwh);
  if (top.length === 0) return undefined;
  const inside = top.reduce((t, c) => t + c.kwh, 0);
  if (summary.house_kwh != null) {
    const left = summary.house_kwh - inside;
    const children = left > 0 ? [...top, rest('house', 'Other (not metered)', left, moneyLeft(summary.house_cost, top.map((c) => c.cost)))] : top;
    return { id: 'house', label: 'House', kwh: summary.house_kwh, cost: summary.house_cost ?? null, meterId: null, children };
  }
  return { id: 'all', label: 'All circuits', kwh: inside, cost: sumMoney(top.map((c) => c.cost)), meterId: null, children: top };
}

export interface LNode {
  node: FlowNode;
  depth: number;
  /** What the band is as wide as: the node's own energy, or what is inside it when that is more (rounding, a reading that lagged). */
  value: number;
  x: number;
  y: number;
  h: number;
  /** Where its label sits (centre), pushed down where it would crowd the label above, and how many lines it has. */
  labelY: number;
  lines: 1 | 2;
}

export interface LLink {
  from: LNode;
  to: LNode;
  y0: number;
  y1: number;
  w: number;
}

export interface Layout {
  width: number;
  height: number;
  nodeWidth: number;
  nodes: LNode[];
  links: LLink[];
}

const NODE_W = 14;
const PAD = 8;
const GAP = 8;
const LINE = 7; // half a text line, for spacing labels

/** Lays the tree out in `width` pixels: one column per level, bands sized by energy with one scale for the whole chart, labels to the right of
 * each band (the last column keeps `labelWidth` free for them). */
export function layoutFlow(tree: FlowNode, width: number, labelWidth = 190, scale = 1): Layout {
  const values = new Map<string, number>();
  const valueOf = (n: FlowNode): number => {
    const v = Math.max(n.kwh, n.children.reduce((t, c) => t + valueOf(c), 0));
    values.set(n.id, v);
    return v;
  };
  valueOf(tree);

  const columns: LNode[][] = [];
  const all = new Map<string, LNode>();
  const place = (n: FlowNode, depth: number) => {
    const ln: LNode = { node: n, depth, value: values.get(n.id) ?? 0, x: 0, y: 0, h: 0, labelY: 0, lines: 1 };
    (columns[depth] ??= []).push(ln);
    all.set(n.id, ln);
    n.children.forEach((c) => place(c, depth + 1));
  };
  place(tree, 0);

  const cols = columns.length;
  const step = cols > 1 ? Math.max(40, (width - 2 * PAD - NODE_W - labelWidth) / (cols - 1)) : 0;
  const tallest = Math.max(...columns.map((c) => c.length));
  const height = Math.min(600 * scale, Math.max(240 * scale, 2 * PAD + tallest * 34 * scale));
  const inner = height - 2 * PAD;

  // One scale for the whole chart, the tightest column deciding, so a wide band means the same energy everywhere.
  let k = Infinity;
  for (const col of columns) {
    const sum = col.reduce((t, n) => t + n.value, 0);
    if (sum > 0) k = Math.min(k, (inner - (col.length - 1) * GAP) / sum);
  }
  if (!Number.isFinite(k) || k < 0) k = 0;

  columns.forEach((col, depth) => {
    let y = PAD;
    let labelBottom = -Infinity;
    for (const n of col) {
      n.x = PAD + depth * step;
      n.y = y;
      n.h = Math.max(2, n.value * k);
      y += n.h + GAP;
      n.lines = n.h >= 30 ? 2 : 1;
      const centre = n.y + n.h / 2;
      n.labelY = Math.max(centre, labelBottom + n.lines * LINE + 2);
      labelBottom = n.labelY + n.lines * LINE;
    }
  });

  const links: LLink[] = [];
  for (const parent of all.values()) {
    let offset = parent.y;
    for (const child of parent.node.children) {
      const to = all.get(child.id)!;
      const w = Math.max(1, to.value * k);
      links.push({ from: parent, to, y0: offset + w / 2, y1: to.y + to.h / 2, w });
      offset += to.value * k;
    }
  }
  const bottom = Math.max(...[...all.values()].map((n) => Math.max(n.y + n.h, n.labelY + LINE)));
  return { width, height: Math.max(height, bottom + PAD), nodeWidth: NODE_W, nodes: [...all.values()], links };
}
