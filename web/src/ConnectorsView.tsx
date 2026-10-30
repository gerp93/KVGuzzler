// The Connectors tab: everything the dashboard reads from outside the house's own wiring, set up in one place. The devices it polls (plugs and monitors) and the
// services it asks (EIA, OpenEI, Ford) are the house's, not a meter's; what they feed (the price, the gas cars) stays where it is used. Which meter is which, and
// how they are wired, is the Hierarchy tab.
import { ComputersCard } from './ComputersCard';
import { DevicesCard } from './DevicesCard';
import { EiaCard } from './EiaCard';
import { FordCard } from './FordCard';
import { GroupsSetupCard } from './GroupsSetupCard';
import type { Meter } from './lib';
import { OpenEiCard } from './OpenEiCard';

export function ConnectorsView({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  return (
    <>
      <DevicesCard meters={meters} onChanged={onChanged} />
      <ComputersCard meters={meters} onChanged={onChanged} />
      <GroupsSetupCard meters={meters} onChanged={onChanged} />
      <EiaCard />
      <OpenEiCard />
      <FordCard meters={meters} />
    </>
  );
}
