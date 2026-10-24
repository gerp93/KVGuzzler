// The flow chart: the whole on the left, its parts to the right, each band as wide as the energy through it over the period. Drawn as SVG at
// the width it has, so the text stays the size of the page's text on a phone as well. Each meter's name opens its page.
import { useEffect, useRef, useState } from 'preact/hooks';
import { meterPath } from './Bars';
import { flowTree, layoutFlow, type LNode, type VirtualParts } from './flow';
import { formatKwh, formatMoney, type Meter, type Summary } from './lib';
import { pcParts, usePcDetails } from './pcparts';

const SHADES = ['n0', 'n1', 'n2', 'n3'];

export function FlowChart({ meters, summary, root }: { meters: Meter[]; summary: Summary; root?: string }) {
  const box = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(720);
  useEffect(() => {
    const el = box.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const watch = new ResizeObserver(() => setWidth(Math.max(280, Math.floor(el.clientWidth))));
    watch.observe(el);
    setWidth(Math.max(280, Math.floor(el.clientWidth)));
    return () => watch.disconnect();
  }, []);

  // A computer's CPU, GPU and memory are drawn as parts of its wall plug (they are its collector's, not meters of their own).
  const details = usePcDetails(meters, summary.from, summary.to);
  const virtual: VirtualParts = new Map(
    [...details].map(([wall, d]) => [wall, pcParts(d, null).filter((p) => !p.remainder).map((p) => ({ label: p.label, kwh: p.kwh, cost: p.cost }))]),
  );
  const tree = flowTree(meters, summary, root, virtual);
  if (!tree || tree.kwh <= 0) return <p class="note">Nothing recorded in this period to draw.</p>;
  const narrow = width < 520;
  const layout = layoutFlow(tree, width, narrow ? 132 : 230);
  const byId = new Map(meters.map((m) => [m.id, m]));
  const shade = (n: LNode) => (n.node.rest ? 'rest' : (SHADES[Math.min(n.depth, SHADES.length - 1)] ?? 'n3'));
  const href = (n: LNode) => {
    const m = n.node.meterId ? byId.get(n.node.meterId) : undefined;
    return m ? meterPath(m) : undefined;
  };
  const figure = (n: LNode) => [formatKwh(n.node.kwh), n.node.cost != null ? formatMoney(n.node.cost, summary.cost_estimated) : ''].filter(Boolean).join(' · ');

  return (
    <div ref={box} class="flow">
      <svg viewBox={`0 0 ${layout.width} ${layout.height}`} width={layout.width} height={layout.height} role="img" aria-label="Where the energy went, from the whole to its parts">
        {layout.links.map((l) => {
          const mid = (l.from.x + layout.nodeWidth + l.to.x) / 2;
          const x0 = l.from.x + layout.nodeWidth;
          return (
            <path
              key={`${l.from.node.id}>${l.to.node.id}`}
              class={`link ${shade(l.to)}`}
              d={`M${x0},${l.y0} C${mid},${l.y0} ${mid},${l.y1} ${l.to.x},${l.y1}`}
              stroke-width={l.w}
            >
              <title>{`${l.from.node.label} to ${l.to.node.label}: ${figure(l.to)}`}</title>
            </path>
          );
        })}
        {layout.nodes.map((n) => {
          const link = href(n);
          const label = n.node.label;
          const lines = n.lines === 2;
          const text = (
            <text x={n.x + layout.nodeWidth + 6} y={n.labelY} dominant-baseline="middle">
              {lines ? (
                <>
                  <tspan x={n.x + layout.nodeWidth + 6} dy="-0.45em">
                    {label}
                  </tspan>
                  <tspan class="small" x={n.x + layout.nodeWidth + 6} dy="1.2em">
                    {figure(n)}
                  </tspan>
                </>
              ) : (
                <>
                  {label}
                  {!narrow && <tspan class="small"> {formatKwh(n.node.kwh)}</tspan>}
                </>
              )}
            </text>
          );
          return (
            <g key={n.node.id}>
              <rect class={`node ${shade(n)}`} x={n.x} y={n.y} width={layout.nodeWidth} height={n.h}>
                <title>{`${label}: ${figure(n)}`}</title>
              </rect>
              {link ? <a href={link}>{text}</a> : text}
            </g>
          );
        })}
      </svg>
    </div>
  );
}
