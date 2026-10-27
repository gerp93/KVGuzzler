// The big figures at the top of a page, each in a tile of its own: a small label, the figure large with its unit quiet beside it, and (optionally) a
// line under it and a trend. One tile is the lead (`now`: what the house draws this moment); the rest are the period's totals.
import type { ComponentChildren } from 'preact';
import { FigureIcon } from './Icons';
import { splitFigure } from './figure';

export type KpiTone = 'now' | 'energy' | 'cost' | 'peak';

export function Kpi({ label, value, tone, hint, children }: { label: string; value: string; tone: KpiTone; hint?: ComponentChildren; children?: ComponentChildren }) {
  const [figure, unit] = splitFigure(value);
  // The lead tile is what is drawn this moment: in kilowatts it is a big draw, and the figure takes the hot colour.
  const hot = tone === 'now' && unit === 'kW';
  return (
    <div class={`kpi kpi-${tone}${hot ? ' hot' : ''}`}>
      <span class="kpi-label">
        <FigureIcon name={tone} />
        {label}
        {tone === 'now' && <i class="kpi-live" aria-hidden="true" />}
      </span>
      <p class="kpi-value">
        <span class="figure">{figure}</span>
        {unit && <span class="unit">{unit}</span>}
      </p>
      {hint && <p class="kpi-hint">{hint}</p>}
      {children}
    </div>
  );
}

export function KpiRow({ children }: { children: ComponentChildren }) {
  return <div class="kpis">{children}</div>;
}
