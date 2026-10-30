// The Ford connection, in the Connectors tab. Ford is linked once but reads the odometer of one charging meter, so with more than one the card says which. Connecting
// is the dialog of `FordSetup`, opened from the buttons of `FordConnection`; the wait Ford asks for between looks is counted here for both.
import { useRef, useState } from 'preact/hooks';
import { api } from './api';
import { FordConnection } from './FordConnection';
import { pickFordMeter } from './fordMeter';
import { FordSetup, type FordDraft, type SetupStep } from './FordSetup';
import { useFordWait } from './fordWait';
import { usePoll } from './hooks';
import type { FordLink, Meter } from './lib';
import { routePath } from './route';

export function FordCard({ meters }: { meters: Meter[] }) {
  const [version, setVersion] = useState(0);
  const { data: ford } = usePoll<FordLink>(() => api.ford(), 60_000, [version]);
  const { waitLeft, wait } = useFordWait(ford);
  // The step of the connecting dialog to open at, or nothing while it is closed.
  const [setup, setSetup] = useState<SetupStep | null>(null);
  const draft = useRef<FordDraft>({});
  const [chosen, setChosen] = useState('');
  const reload = () => setVersion((v) => v + 1);

  const cars = meters.filter((m) => m.ev);
  const meter = pickFordMeter(meters, ford?.meter_id, chosen);

  return (
    <section class="card page-narrow">
      <h2>Ford</h2>
      <p class="caption">Keeps the car's odometer and battery up to date.</p>
      {cars.length > 1 && (
        <label class="field">
          <span>Odometer of</span>
          <select value={meter?.id ?? ''} onChange={(e) => setChosen((e.target as HTMLSelectElement).value)}>
            {cars.map((m) => (
              <option key={m.id} value={m.id}>
                {m.label}
              </option>
            ))}
          </select>
        </label>
      )}
      {meter ? (
        <FordConnection cfg={ford} reload={reload} waitLeft={waitLeft} wait={wait} onSetup={setSetup} />
      ) : (
        <p class="note">
          No meter charges a vehicle yet. Open the charger's meter in the <a href={routePath('hierarchy')}>Hierarchy tab</a> and press Track miles.
        </p>
      )}
      {setup && meter && <FordSetup meter={meter} cfg={ford} startAt={setup} draft={draft} waitLeft={waitLeft} wait={wait} reload={reload} onClose={() => setSetup(null)} />}
    </section>
  );
}
