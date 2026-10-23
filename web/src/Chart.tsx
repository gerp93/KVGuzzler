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

/** `domain` is the period shown, in seconds since the epoch, so a chart with few points still spans the period. */
export function Chart({ lines, domain, height = 260 }: { lines: Line[]; domain: [number, number]; height?: number }) {
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = box.current;
    if (!el || lines.length === 0) return;
    const text = css('--color-text');
    const grid = css('--color-border');
    const plot = new uPlot(
      {
        width: el.clientWidth || 600,
        height,
        legend: { show: true },
        cursor: { drag: { x: false, y: false } },
        scales: {
          x: { time: true, range: () => domain },
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
        series: [{}, ...lines.map((l) => ({ label: l.name, stroke: css(l.colorVar), width: 2, spanGaps: false }))],
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
  }, [lines, domain, height]);

  return <div ref={box} class="chart" role="img" aria-label="Power over time" />;
}
