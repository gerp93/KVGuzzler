import { useState } from 'preact/hooks';
import { api } from './api';
import { usePoll } from './hooks';
import type { Meter, Computer } from './lib';

/** Settings for computers: add one, name it, and tie its wall plug and its peripherals to it. */
export function ComputersCard({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  const [rev, setRev] = useState(0);
  const { data: computers, error } = usePoll<Computer[]>(api.computers, 10_000, [rev, meters]);
  const [name, setName] = useState('');
  const [message, setMessage] = useState('');
  const [confirming, setConfirming] = useState('');

  const changed = () => {
    setRev((n) => n + 1);
    onChanged();
  };
  const run = (work: Promise<unknown>) =>
    work.then(
      () => {
        setMessage('');
        changed();
      },
      (e: Error) => setMessage(e.message),
    );

  return (
    <section class="card">
      <h2>Computers</h2>
      <p class="caption">
        A computer is its own kind of appliance. Tie its wall plug (which measures the whole PC) and its peripherals to it, and
        it opens as one thing in Appliances. A PC that has the KVGuzzler collector adds itself here the first time it reports.
      </p>
      {error && <p class="note">{error.message}</p>}
      {computers && computers.length === 0 && <p class="note">No computers yet.</p>}

      {computers?.map((c) => (
        <ComputerBlock
          key={c.id}
          computer={c}
          meters={meters}
          confirming={confirming === c.id}
          onConfirm={(on) => setConfirming(on ? c.id : '')}
          run={run}
        />
      ))}

      <fieldset class="device-form">
        <legend>Add a computer</legend>
        <label class="field">
          <span>Name</span>
          <input value={name} maxLength={60} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
        </label>
        <div class="chips">
          <button
            type="button"
            disabled={name.trim() === ''}
            onClick={() => {
              void run(api.addComputer(name.trim()).then(() => setName('')));
            }}
          >
            Add computer
          </button>
        </div>
      </fieldset>
      {message && (
        <p class="note error" role="alert">
          {message}
        </p>
      )}
    </section>
  );
}

function ComputerBlock({
  computer, meters, confirming, onConfirm, run,
}: {
  computer: Computer;
  meters: Meter[];
  confirming: boolean;
  onConfirm: (on: boolean) => void;
  run: (work: Promise<unknown>) => void;
}) {
  const [name, setName] = useState(computer.name);
  // A plug can be chosen if it is free or already this computer's.
  const mine = (c: Meter) => !c.computer_id || c.computer_id === computer.id;
  const wallChoices = meters.filter((c) => c.scope === 'pc' && mine(c));
  const peripheralChoices = meters.filter((c) => c.scope === 'plug' && mine(c));

  const setWall = (meterId: string) => {
    // One wall plug per computer: let the old one go first, then tie the new one.
    const work = async () => {
      if (computer.wall_meter_id) await api.updateMeter(computer.wall_meter_id, { computer_id: null });
      if (meterId) await api.updateMeter(meterId, { computer_id: computer.id });
    };
    run(work());
  };

  return (
    <div class="computer-block">
      <div class="row">
        <label class="grow">
          <span class="visually-hidden">Name of the computer</span>
          <input
            value={name}
            maxLength={60}
            onInput={(e) => setName((e.target as HTMLInputElement).value)}
            onBlur={() => name.trim() && name !== computer.name && run(api.updateComputer(computer.id, { name }))}
            onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
          />
        </label>
        {confirming ? (
          <>
            <button type="button" onClick={() => run(api.removeComputer(computer.id))}>
              Remove it
            </button>
            <button type="button" onClick={() => onConfirm(false)}>
              Keep
            </button>
          </>
        ) : (
          <button type="button" onClick={() => onConfirm(true)}>
            Remove
          </button>
        )}
      </div>

      <label class="field">
        <span>Wall plug (measures the whole PC)</span>
        <select value={computer.wall_meter_id ?? ''} onChange={(e) => setWall((e.target as HTMLSelectElement).value)}>
          <option value="">None</option>
          {wallChoices.map((c) => (
            <option key={c.id} value={c.id}>
              {c.label}
            </option>
          ))}
        </select>
      </label>
      {wallChoices.length === 0 && <p class="caption">Add a plug marked "my PC's plug" under Devices to choose it here.</p>}

      <p class="caption">Peripherals (each is a separate plug)</p>
      {peripheralChoices.length === 0 && <p class="caption">No other plugs to tie. Add a plug under Devices first.</p>}
      <div class="chips">
        {peripheralChoices.map((c) => {
          const tied = c.computer_id === computer.id;
          return (
            <button
              key={c.id}
              type="button"
              aria-pressed={tied}
              onClick={() => run(api.updateMeter(c.id, { computer_id: tied ? null : computer.id }))}
            >
              {c.label}
            </button>
          );
        })}
      </div>
      {computer.source_id && (
        <p class="caption">
          Its collector reports as <code>{computer.source_id}</code>.
        </p>
      )}
    </div>
  );
}
