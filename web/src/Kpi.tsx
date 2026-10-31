// The big figures at the top of a page, each in a tile of its own: a small label, the figure large with its unit quiet beside it, and (optionally) a
// line under it and a trend. One tile is the lead (`now`: what the house draws this moment); the rest are the period's totals.
import type { ComponentChildren } from 'preact';
import { FigureIcon } from './Icons';
import { splitFigure } from './figure';
import { formatCarbon } from './lib';

export type KpiTone = 'now' | 'energy' | 'cost' | 'peak' | 'carbon';

/** `corner`: a small line at the tile's upper right, level with the label (what is biggest in it, say). */
export function Kpi({ label, value, tone, hint, corner, children }: { label: string; value: string; tone: KpiTone; hint?: ComponentChildren; corner?: ComponentChildren; children?: ComponentChildren }) {
  const [figure, unit] = splitFigure(value);
  // The kilo unit (kW, kWh) is a big figure and takes the hot colour; watts and watt-hours take the cool one. Money and the rest stay as they are.
  const heat = unit === 'kW' || unit === 'kWh' ? ' hot' : unit === 'W' || unit === 'Wh' ? ' cool' : '';
  return (
    <div class={`kpi kpi-${tone}${heat}`}>
      <div class="kpi-head">
        <span class="kpi-label">
          <FigureIcon name={tone} />
          {label}
          {tone === 'now' && <i class="kpi-live" aria-hidden="true" />}
        </span>
        {corner && <span class="kpi-corner">{corner}</span>}
      </div>
      <p class="kpi-value">
        <span class="figure">{figure}</span>
        {unit && <span class="unit">{unit}</span>}
      </p>
      {hint && <p class="kpi-hint">{hint}</p>}
      {children}
    </div>
  );
}

/** `five`: the row has a fifth tile (carbon), which gets a column of its own on a wide screen. */
export function KpiRow({ children, five }: { children: ComponentChildren; five?: boolean }) {
  return <div class={five ? 'kpis kpis-five' : 'kpis'}>{children}</div>;
}

/** The carbon tile of a meter's or computer's page: the amount, and the energy-weighted grid average it came to. Nothing when there is no carbon to show. */
export function CarbonKpi({ carbonG, kwh }: { carbonG: number | null | undefined; kwh: number | null | undefined }) {
  if (carbonG == null || kwh == null) return null;
  return <Kpi tone="carbon" label="Carbon footprint" value={formatCarbon(carbonG)} hint={kwh > 0 ? `${Math.round(carbonG / kwh)} g/kWh grid average` : undefined} />;
}
