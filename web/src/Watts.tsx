// A live draw written out in watts. Once it is in kilowatts it takes the theme's hot colour, so a big draw (the dryer, the EV charger) shows at a glance.
// Only for what is drawn right now: peaks and totals stay plain.
import { formatWatts, isKilowatts } from './lib';

export function Watts({ w }: { w: number | null | undefined }) {
  const text = formatWatts(w);
  return isKilowatts(w) ? <span class="big-draw">{text}</span> : <>{text}</>;
}
