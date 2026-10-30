// The "Everything else" lines of the Hierarchy tree: under every meter that holds others, what its own reading carries beyond the meters listed inside
// it (and, under the whole house, beyond the circuits and plugs listed beneath it). Its energy and cost are the flow chart's own figures for the same
// period, so the tree and the chart above it never disagree; right now is the meter's draw less what is listed inside it. It is never added to anything.
import type { FlowNode } from './flow';
import type { Meter } from './lib';

export interface TreeRow {
  meter: Meter;
  depth: number;
}

export interface RestRow {
  /** The meter it is left over from; the whole-house meter's own id, with `house: true`. */
  parent: Meter;
  /** The row it is drawn after: the last row of the parent's subtree. */
  after: number;
  depth: number;
  /** Power right now (the parent's less what is listed inside it, never below zero); null when the parent has no reading. */
  watts: number | null;
  kwh: number;
  cost: string | null;
}

/** The flow chart's nodes by the meter they stand for (`house` for the whole house), each with the remainder node under it, when it has one. */
function flowNodes(flow: FlowNode | undefined): Map<string, { node: FlowNode; left: FlowNode | undefined }> {
  const out = new Map<string, { node: FlowNode; left: FlowNode | undefined }>();
  const walk = (n: FlowNode) => {
    // Under the whole house the Hierarchy tree lists the plugs beside the circuits, so what it leaves over is the chart's "Not individually metered" line (or the
    // whole "Everything else" line when no plug is listed inside it).
    const group = n.children.find((c) => c.group);
    const left = group ? (group.children.find((c) => c.rest) ?? (group.children.length === 0 ? group : undefined)) : n.children.find((c) => c.rest);
    out.set(n.meterId ?? n.id, { node: n, left });
    n.children.forEach(walk);
  };
  if (flow) walk(flow);
  return out;
}

/** What to draw after the rows, one per row that holds others: the rows are in tree order, `depth` says how deep each is. `flow` is the flow chart's
 * tree for the same period, `watts` each meter's reading now and `houseWatts` the house's. */
export function restRows(rows: TreeRow[], flow: FlowNode | undefined, watts: Map<string, number>, houseWatts: number | null | undefined): RestRow[] {
  const nodes = flowNodes(flow);
  const out: RestRow[] = [];
  rows.forEach(({ meter, depth }, i) => {
    if (!((rows[i + 1]?.depth ?? 0) > depth)) return; // nothing is listed inside it
    let end = i;
    while (rows[end + 1] && rows[end + 1]!.depth > depth) end++;
    const house = meter.scope === 'mains';
    const here = nodes.get(house ? 'house' : meter.id);
    const parentWatts = house ? houseWatts : watts.get(meter.id);
    const inside = rows.slice(i + 1, end + 1).filter((r) => r.depth === depth + 1).reduce((t, r) => t + (watts.get(r.meter.id) ?? 0), 0);
    out.push({
      parent: meter,
      after: end,
      depth: depth + 1,
      watts: parentWatts == null ? null : Math.max(0, parentWatts - inside),
      // With no remainder drawn by the chart (nothing is left over, or nothing was recorded) it is nothing, and a free nothing only when a price is known.
      kwh: here?.left?.kwh ?? 0,
      cost: here?.left ? here.left.cost : here?.node.cost != null ? '0.00' : null,
    });
  });
  // Where several end on the same row, the deepest first: it closes before the one around it.
  return out.sort((a, b) => a.after - b.after || b.depth - a.depth);
}
