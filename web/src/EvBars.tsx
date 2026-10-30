// Figures set side by side as horizontal bars, each as long as its figure against the longest: what a mile costs in charging and in each gas car, say. For
// comparing amounts that have no history to draw over time. The figures are the server's.
import type { TabBar } from './evTabs';

export function EvBars({ bars, height, label }: { bars: TabBar[]; height: number; label: string }) {
  const longest = Math.max(0, ...bars.map((b) => b.value));
  return (
    <ul class="ev-bars" style={{ minHeight: `${height}px` }} aria-label={label}>
      {bars.map((b) => (
        <li class="ev-bar" key={b.label}>
          <span class="ev-bar-label">{b.label}</span>
          <span class="ev-bar-track">
            <span class="ev-bar-fill" style={{ width: `${longest > 0 ? (b.value / longest) * 100 : 0}%`, background: `var(${b.colorVar})`, opacity: b.alpha ?? 1 }} />
          </span>
          <span class="ev-bar-value">{b.text}</span>
        </li>
      ))}
    </ul>
  );
}
