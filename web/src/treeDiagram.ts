// The layout of the Hierarchy diagram, with no drawing in it: the tree left to right, one column per level, every leaf in a row of its own and every parent
// centred on what is inside it. The input is the tree's own list (each line and how deep it is), so the diagram is the same tree in another shape.

export interface Sizes {
  boxW: number;
  boxH: number;
  gapX: number;
  slot: number;
  pad: number;
}

export interface PlacedNode {
  id: string;
  depth: number;
  x: number;
  y: number;
}

export interface PlacedLink {
  from: string;
  to: string;
  x0: number;
  y0: number;
  x1: number;
  y1: number;
}

export interface DiagramLayout {
  width: number;
  height: number;
  nodes: PlacedNode[];
  links: PlacedLink[];
}

interface Work {
  id: string;
  depth: number;
  children: Work[];
  y: number;
}

/** Lays out `items` (in tree order, with their depth): a line is inside the nearest line above it that is one level shallower. Lines at the top level
 * have no parent and are stacked one under another with no link between them. */
export function layoutDiagram(items: { id: string; depth: number }[], s: Sizes): DiagramLayout {
  const root: Work = { id: '', depth: -1, children: [], y: 0 };
  const stack: Work[] = [root];
  const all: Work[] = [];
  for (const it of items) {
    const node: Work = { id: it.id, depth: it.depth, children: [], y: 0 };
    while (stack.length > 1 && stack[stack.length - 1]!.depth >= it.depth) stack.pop();
    stack[stack.length - 1]!.children.push(node);
    stack.push(node);
    all.push(node);
  }

  let leaves = 0;
  const place = (n: Work) => {
    if (n.children.length === 0) {
      n.y = s.pad + leaves * s.slot;
      leaves++;
      return;
    }
    n.children.forEach(place);
    n.y = (n.children[0]!.y + n.children[n.children.length - 1]!.y) / 2;
  };
  root.children.forEach(place);

  const deepest = Math.max(0, ...all.map((n) => n.depth));
  const xOf = (depth: number) => s.pad + depth * (s.boxW + s.gapX);
  const links: PlacedLink[] = [];
  const walk = (n: Work) => {
    for (const c of n.children) {
      if (n !== root) links.push({ from: n.id, to: c.id, x0: xOf(n.depth) + s.boxW, y0: n.y + s.boxH / 2, x1: xOf(c.depth), y1: c.y + s.boxH / 2 });
      walk(c);
    }
  };
  walk(root);

  return {
    width: 2 * s.pad + (deepest + 1) * s.boxW + deepest * s.gapX,
    height: 2 * s.pad + Math.max(0, leaves - 1) * s.slot + s.boxH,
    nodes: all.map((n) => ({ id: n.id, depth: n.depth, x: xOf(n.depth), y: n.y })),
    links,
  };
}
