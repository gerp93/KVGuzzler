// The cost estimator on a tab of its own: what running the house would cost at the saved price, at a load typed in, at what it draws right now, or at what it really used per day.
import { api } from './api';
import { Estimator } from './Estimator';
import { usePoll } from './hooks';
import { houseUsageBasis, type Live, type Meter } from './lib';
import { measuredTotals } from './measured';

export function EstimatorView({ meters }: { meters: Meter[] }) {
  const whole = meters.some((m) => m.enabled && m.scope === 'mains');
  const { data: live } = usePoll<Live>(api.live, 5000, []);
  if (!whole) {
    return (
      <section class="card">
        <h2>Cost estimator</h2>
        <p class="note">The estimator works from the whole house, so it needs a whole-house clamp: mark the main panel's meter as the house in the Hierarchy tab.</p>
      </section>
    );
  }
  const nowWatts = live?.house_watts ?? measuredTotals(meters, undefined, live?.readings ?? []).watts;
  return <Estimator load={api.houseEstimate} deps={[]} basisOf={houseUsageBasis} nowWatts={nowWatts} nowWhat="the mains clamps" />;
}
