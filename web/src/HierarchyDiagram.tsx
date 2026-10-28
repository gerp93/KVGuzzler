// The Hierarchy tab's diagram: the tree beside it drawn as boxes and lines. It is made from the same plan (treePlan.ts), so every change to the tree
// (a move, a hide, a rename, a new plug) is in it the next time the tab draws. A box shows the name, what it draws now and what it used over the period;
// a meter's box opens its page. A box is that meter's own reading: a parent's already contains what is inside it, nothing here is added.
import { meterPath } from './Bars';
import { formatKwh, formatWatts, type MeterSummary } from './lib';
import { uiScale } from './uiscale';
import { layoutDiagram } from './treeDiagram';
import type { Entry } from './treePlan';

const MAX_CHARS = 18;

interface Box {
  id: string;
  label: string;
  figure: string;
  title: string;
  href?: string;
  cls: string;
}

function boxOf(e: Entry, watts: Map<string, number>, figures: Map<string, MeterSummary>): Box {
  const join = (w: number | null | undefined, k: number | null | undefined) => [w != null ? formatWatts(w) : '', k != null ? formatKwh(k) : ''].filter(Boolean).join(' · ');
  if (e.kind === 'lump') return { id: e.id, label: e.label, figure: join(e.watts, e.kwh), title: e.title, cls: 'lump' };
  if (e.kind === 'part') return { id: e.id, label: e.part.label, figure: join(e.part.watts, e.part.kwh), title: e.part.remainder ? 'Wall plug less the parts above' : 'As its collector reports it', href: e.href, cls: 'part' };
  const m = e.meter;
  const cls = m.scope === 'mains' ? 'house' : m.scope === 'circuit' ? 'circuit' : 'device';
  return { id: e.id, label: m.label, figure: join(watts.get(m.id), figures.get(m.id)?.kwh), title: m.label, href: meterPath(m), cls };
}

export function HierarchyDiagram({ entries, watts, figures, hover, onHover }: { entries: Entry[]; watts: Map<string, number>; figures: Map<string, MeterSummary>; hover: string | null; onHover: (id: string | null) => void }) {
  if (entries.length === 0) return <p class="note">No meters shown.</p>;
  const s = uiScale();
  const sizes = { boxW: 132 * s, boxH: 38 * s, gapX: 18 * s, slot: 44 * s, pad: 6 * s };
  const layout = layoutDiagram(entries.map((e) => ({ id: e.id, depth: e.depth })), sizes);
  const boxes = new Map(entries.map((e) => [e.id, boxOf(e, watts, figures)]));
  const short = (t: string) => (t.length > MAX_CHARS ? `${t.slice(0, MAX_CHARS - 1)}…` : t);

  return (
    <div class="hier-diagram">
      <svg viewBox={`0 0 ${layout.width} ${layout.height}`} style={{ maxWidth: `${layout.width}px` }} role="group" aria-label="The hierarchy as a diagram">
        {layout.links.map((l) => {
          const mid = (l.x0 + l.x1) / 2;
          return <path key={`${l.from}>${l.to}`} class="link" d={`M${l.x0},${l.y0} C${mid},${l.y0} ${mid},${l.y1} ${l.x1},${l.y1}`} />;
        })}
        {layout.nodes.map((n) => {
          const b = boxes.get(n.id)!;
          const body = (
            <>
              <rect class={`box ${b.cls}${hover === n.id ? ' hot' : ''}`} x={n.x} y={n.y} width={sizes.boxW} height={sizes.boxH} rx={5 * s}>
                <title>{b.figure ? `${b.title}: ${b.figure}` : b.title}</title>
              </rect>
              <text class="name" x={n.x + 8 * s} y={n.y + 15 * s}>
                {short(b.label)}
              </text>
              <text class="small" x={n.x + 8 * s} y={n.y + 30 * s}>
                {b.figure}
              </text>
            </>
          );
          return (
            <g key={n.id} onMouseEnter={() => onHover(n.id)} onMouseLeave={() => onHover(null)}>
              {b.href ? <a href={b.href}>{body}</a> : body}
            </g>
          );
        })}
      </svg>
    </div>
  );
}
