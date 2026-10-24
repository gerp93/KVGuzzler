// A trend line of the last few minutes of one live figure. The dashboard reads the live figure every couple of seconds anyway;
// keeping the last stretch of those readings in the page costs nothing and shows whether it is rising, falling or steady.
import { useEffect, useRef, useState } from 'preact/hooks';

const KEEP = 150; // about five minutes at one reading every two seconds

/** Remembers the last readings of `value`, one per change of `stamp` (a new reading, even an equal one). Nulls are skipped. */
export function useTrail(value: number | null | undefined, stamp: string | undefined): number[] {
  const trail = useRef<number[]>([]);
  const [, bump] = useState(0);
  useEffect(() => {
    if (value == null || !stamp) return;
    trail.current = [...trail.current, value].slice(-KEEP);
    bump((n) => n + 1);
  }, [stamp]);
  return trail.current;
}

export function Spark({ values, label }: { values: number[]; label: string }) {
  if (values.length < 3) return null;
  const lo = Math.min(...values);
  const hi = Math.max(...values);
  const span = hi - lo || 1;
  const W = 100;
  const H = 32;
  const pts = values.map((v, i) => [(i / (values.length - 1)) * W, H - 3 - ((v - lo) / span) * (H - 6)] as const);
  const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(2)},${y.toFixed(2)}`).join(' ');
  const last = pts[pts.length - 1]!;
  return (
    <div class="spark" role="img" aria-label={label}>
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none">
        <path class="area" d={`${line} L${W},${H} L0,${H} Z`} />
        <path class="line" d={line} />
      </svg>
      <span class="dotmark" style={{ left: `${last[0]}%`, top: `${(last[1] / H) * 100}%` }} />
    </div>
  );
}
