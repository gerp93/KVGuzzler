// The house's draw right now, always in the top bar whatever page is open: watts (kilowatts when it is a lot, in the hot colour), and the whole-house meter's
// amps and volts. It is its own component with its own 2 second reading, so only this corner redraws, never the page under it. Clicking it opens the Live page.
import { api } from './api';
import { formatAmps, formatVolts } from './amps';
import { usePoll } from './hooks';
import { isKilowatts, type Live, type Meter } from './lib';
import { measuredTotals } from './measured';
import { routePath } from './route';
import { Watts } from './Watts';

export function HouseNow({ meters }: { meters: Meter[] }) {
  const { data: live, error } = usePoll<Live>(api.live, 2000, []);
  if (!live) return null;
  const mains = meters.filter((m) => m.enabled && m.scope === 'mains');
  const whole = mains.length > 0;
  // With no whole-house clamp nothing says what the whole house draws: the top-level meters added up stand in for it, and the label says so.
  const watts = whole ? live.house_watts : measuredTotals(meters, undefined, live.readings).watts;
  if (watts == null) return null;
  // Amps and volts are one meter's: only said when exactly one meter is the whole house (several mains are different feeds, and currents are never added).
  const feed = whole && mains.length === 1 ? live.readings.find((r) => r.meter_id === mains[0]!.id) : undefined;
  return (
    <a
      class={`house-now${error ? ' stale' : ''}`}
      href={routePath('live')}
      title={error ? 'The server is not answering: this is the last reading' : whole ? 'The whole house right now' : 'Everything measured, right now: no whole-house clamp is reporting'}
    >
      <span class="label">{whole ? 'House' : 'Measured'}</span>
      <span class={`watts${isKilowatts(watts) ? ' hot' : ''}`}>
        <Watts w={watts} />
      </span>
      {feed?.amps != null && <span class="extra">{formatAmps(feed.amps)}</span>}
      {feed?.volts != null && <span class="extra">{formatVolts(feed.volts)}</span>}
    </a>
  );
}
