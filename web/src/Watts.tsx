// A figure in watts or kilowatts, or in watt-hours or kilowatt-hours, coloured by its unit so the size shows at a glance: the kilo unit (a big draw, a big
// amount of energy) takes the theme's hot colour and the small one its cool colour (the dryer and the EV charger stand out, a standby draw does not).
// Kpi.tsx does the same for the big tiles. Not for a number in a sentence that is about something else.
import { formatKwh, formatWatts, isKilowattHours, isKilowatts } from './lib';

export function Watts({ w }: { w: number | null | undefined }) {
  const text = formatWatts(w);
  if (w === null || w === undefined) return <>{text}</>;
  return <span class={isKilowatts(w) ? 'big-draw' : 'small-draw'}>{text}</span>;
}

export function Kwh({ k }: { k: number | null | undefined }) {
  const text = formatKwh(k);
  if (k === null || k === undefined) return <>{text}</>;
  return <span class={isKilowattHours(k) ? 'big-draw' : 'small-draw'}>{text}</span>;
}
