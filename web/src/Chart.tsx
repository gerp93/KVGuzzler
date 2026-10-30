import { useEffect, useRef } from 'preact/hooks';
import uPlot from 'uplot';
import { uiScale } from './uiscale';
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

/** The same colour, see-through, for the soft fill under a line; a colour that is not plain rgb() gets no fill. */
export function withAlpha(color: string, alpha: number): string | undefined {
  const m = /^rgba?\(\s*(\d+)[,\s]+(\d+)[,\s]+(\d+)/.exec(color);
  return m ? `rgba(${m[1]}, ${m[2]}, ${m[3]}, ${alpha})` : undefined;
}

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
/** A chart's height: taller on a big monitor, where the page around it is much wider and more spacious. */
function defaultHeight(): number {
  if (typeof window === 'undefined' || !window.matchMedia) return 260;
  if (window.matchMedia('(min-width: 3000px)').matches) return Math.round(Math.min(760, Math.max(400, window.innerHeight * 0.34)));
  return window.matchMedia('(min-width: 1800px)').matches ? 340 : 260;
}

/** The chart that is the whole point of a page (History): on a big monitor it takes the height that is left under the header and above the key. */
export function tallChartHeight(): number {
  if (typeof window === 'undefined' || !window.matchMedia || !window.matchMedia('(min-width: 1800px)').matches) return defaultHeight();
  return Math.max(defaultHeight(), Math.min(900, window.innerHeight - 380));
}

/** The height of the chart on the live page: the usual one on a small screen, taller on a wide one so it keeps up with the list beside it. */
export function wideChartHeight(): number {
  if (typeof window === 'undefined' || !window.matchMedia || !window.matchMedia('(min-width: 1800px)').matches) return defaultHeight();
  return Math.max(defaultHeight(), Math.min(520, Math.round(window.innerHeight * 0.36)));
}

// The default for `shades` must be one array, not a new `[]` on each render: the chart is rebuilt when any of its inputs is a different object, and a page that
// re-renders every couple of seconds (the live page) would tear the chart down and build it again each time, shrinking the page for a moment under the reader.
const NO_SHADES: Shade[] = [];

/** The vertical axis of a power chart: "800 W", "1.5 kW". */
export function wattsAxis(w: number): string {
  return w >= 1000 ? `${w / 1000} kW` : `${w} W`;
}

// `format` writes a value on the vertical axis (a chart of something other than power says its own unit) and must be one function, not a new one on each render, for the same reason as `shades`.
export function Chart({ lines, domain, shades = NO_SHADES, height = defaultHeight(), format = wattsAxis, label = 'Power over time' }: { lines: Line[]; domain: [number, number]; shades?: Shade[]; height?: number; format?: (v: number) => string; label?: string }) {
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = box.current;
    if (!el || lines.length === 0) return;
    const text = css('--color-text');
    const grid = css('--color-border');
    const shade = css('--color-accent-muted');
    const span = effectiveDomain(domain, lines);
    const axisFont = `${Math.round(12 * uiScale())}px system-ui, sans-serif`;
    const plot = new uPlot(
      {
        width: el.clientWidth || 600,
        height,
        legend: { show: true },
        cursor: { drag: { x: false, y: false } },
        scales: {
          x: { time: true, range: () => span },
          y: { range: (_u, min, max) => [min < 0 ? min * 1.05 : 0, max > 0 ? max * 1.05 : 1] },
        },
        axes: [
          { stroke: text, font: axisFont, grid: { stroke: grid, width: 0.5 }, ticks: { stroke: grid } },
          {
            stroke: text,
            font: axisFont,
            grid: { stroke: grid, width: 0.5 },
            ticks: { stroke: grid },
            values: (_u, v) => v.map(format),
            size: Math.round((el.clientWidth < 420 ? 52 : 64) * uiScale()),
          },
        ],
        series: [
          {},
          ...lines.map((l, i) => {
            const stroke = css(l.colorVar);
            // The first solid line gets a soft fill, so the shape of the day reads at a glance; the rest stay plain lines.
            const fill = i === 0 && !l.dashed ? withAlpha(stroke, 0.14) : undefined;
            return { label: l.name, stroke, fill, width: 2, spanGaps: false, dash: l.dashed ? [6, 4] : undefined, points: { size: 5 } };
          }),
        ],
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
  }, [lines, domain, shades, height, format]);

  return <div ref={box} class="chart" role="img" aria-label={label} />;
}
