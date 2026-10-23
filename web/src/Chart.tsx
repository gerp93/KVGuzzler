import { useEffect, useRef } from 'preact/hooks';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';

export interface Line {
  name: string;
  /** A theme custom property, e.g. `--color-accent-blue`. */
  colorVar: string;
  /** Seconds since the epoch, one per value. */
  x: number[];
  y: number[];
  /** Drawn dashed: a separate device next to the main lines. */
  dashed?: boolean;
}

/** A stretch of the period to shade (seconds since the epoch): a time nothing was being recorded, so no line is drawn across it. */
export interface Shade {
  from: number;
  to: number;
}

const css = (name: string) => getComputedStyle(document.body).getPropertyValue(name).trim() || '#888';

/** Merges lines onto one shared time axis, leaving a gap (null) where a line has no point. */
export function align(lines: Line[]): uPlot.AlignedData {
  const xs = [...new Set(lines.flatMap((l) => l.x))].sort((a, b) => a - b);
  const data: (number | null)[][] = lines.map((l) => {
    const at = new Map(l.x.map((x, i) => [x, l.y[i] ?? null]));
    return xs.map((x) => at.get(x) ?? null);
  });
  return [xs, ...data] as uPlot.AlignedData;
}

/** A chart line from the API's series points (average watts per bucket). */
export function lineFrom(name: string, colorVar: string, points: { t: string; avg_watts: number }[]): Line {
  return { name, colorVar, x: points.map((p) => Date.parse(p.t) / 1000), y: points.map((p) => p.avg_watts) };
}

/** The period a chart spans. A period that starts years before its first point (all time) starts at that point instead, so the
 * recorded part is not squeezed into a corner. */
export function effectiveDomain(domain: [number, number], lines: Pick<Line, 'x'>[]): [number, number] {
  const firsts = lines.filter((l) => l.x.length > 0).map((l) => Math.min(...l.x));
  if (firsts.length === 0) return domain;
  const first = Math.min(...firsts);
  const TWO_YEARS = 2 * 365 * 24 * 3600;
  return first - domain[0] > TWO_YEARS ? [first, domain[1]] : domain;
}

/** `domain` is the period shown, in seconds since the epoch, so a chart with few points still spans the period. */
export function Chart({ lines, domain, shades = [], height = 260 }: { lines: Line[]; domain: [number, number]; shades?: Shade[]; height?: number }) {
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = box.current;
    if (!el || lines.length === 0) return;
    const text = css('--color-text');
    const grid = css('--color-border');
    const shade = css('--color-accent-muted');
    const span = effectiveDomain(domain, lines);
    const plot = new uPlot(
      {
        width: el.clientWidth || 600,
        height,
        legend: { show: true },
        cursor: { drag: { x: false, y: false } },
        scales: {
          x: { time: true, range: () => span },
          y: { range: (_u, _min, max) => [0, max > 0 ? max * 1.05 : 1] },
        },
        axes: [
          { stroke: text, grid: { stroke: grid, width: 0.5 }, ticks: { stroke: grid } },
          {
            stroke: text,
            grid: { stroke: grid, width: 0.5 },
            ticks: { stroke: grid },
            values: (_u, v) => v.map((w) => (w >= 1000 ? `${w / 1000} kW` : `${w} W`)),
            size: 64,
          },
        ],
        series: [{}, ...lines.map((l) => ({ label: l.name, stroke: css(l.colorVar), width: 2, spanGaps: false, dash: l.dashed ? [6, 4] : undefined }))],
        hooks: {
          // Shade the times nothing was recorded, behind the lines.
          drawClear: [
            (u: uPlot) => {
              const { ctx, bbox } = u;
              ctx.save();
              ctx.globalAlpha = 0.25;
              ctx.fillStyle = shade;
              for (const g of shades) {
                const x0 = Math.max(bbox.left, u.valToPos(g.from, 'x', true));
                const x1 = Math.min(bbox.left + bbox.width, u.valToPos(g.to, 'x', true));
                if (x1 > x0) ctx.fillRect(x0, bbox.top, x1 - x0, bbox.height);
              }
              ctx.restore();
            },
          ],
        },
      },
      align(lines),
      el,
    );
    const resize = new ResizeObserver(() => plot.setSize({ width: el.clientWidth, height }));
    resize.observe(el);
    return () => {
      resize.disconnect();
      plot.destroy();
    };
  }, [lines, domain, shades, height]);

  return <div ref={box} class="chart" role="img" aria-label="Power over time" />;
}
