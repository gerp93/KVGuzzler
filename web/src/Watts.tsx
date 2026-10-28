// A figure in watts or kilowatts, or in watt-hours or kilowatt-hours, coloured by its unit so the size shows at a glance: the kilo unit (a big draw, a big
// amount of energy) takes the theme's hot colour and the small one its cool colour (the dryer and the EV charger stand out, a standby draw does not).
// Kpi.tsx does the same for the big tiles. Not for a number in a sentence that is about something else.
import { formatKwh, formatWatts, isKilowattHours, isKilowatts } from './lib';

export function Watts({ w }: { w: number | null | undefined }) {
  const text = formatWatts(w);
  if (w === null || w === undefined) return <>{text}</>;
  return <span class={isKilowatts(w) ? 'big-draw' : 'small-draw'}>{text}</span>;
}

/** The lowest draw of a period, for the line under the highest in the same tile: "lowest 355 W", the least a whole minute averaged. Nothing when there is none. */
export function Lowest({ w }: { w: number | null | undefined }) {
  if (w === null || w === undefined) return null;
  return (
    <span class="kpi-low" title="The lowest a whole minute averaged in the period">
      lowest <Watts w={w} />
    </span>
  );
}

export function Kwh({ k }: { k: number | null | undefined }) {
  const text = formatKwh(k);
  if (k === null || k === undefined) return <>{text}</>;
  return <span class={isKilowattHours(k) ? 'big-draw' : 'small-draw'}>{text}</span>;
}
