// Computers, in the Connectors tab: each PC with the collector that reports its CPU, GPU, memory and programs (whether it is being heard from), and what the
// computer is made of: its name, its wall plug (the whole PC at the wall) and its peripherals (each a separate plug). The wall plug and a peripheral stay where
// they are plugged in in the tree; this is only the grouping, like a group. A PC's figures are on its own page. The collector pushes rather than being polled,
// and where it sends to is set on the PC.
import { useState } from 'preact/hooks';
import { AddPanel } from './AddPanel';
import { api } from './api';
import { collectorState } from './collector';
import { usePoll } from './hooks';
import { formatAge, type Computer, type Meter } from './lib';
import { routePath } from './route';

export function ComputersCard({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  const [rev, setRev] = useState(0);
  const [adding, setAdding] = useState(false);
  const { data: computers, error } = usePoll<Computer[]>(api.computers, 5000, [rev, meters]);
  const [message, setMessage] = useState('');

  const run = (work: Promise<unknown>) =>
    work.then(
      () => {
        setMessage('');
        setRev((n) => n + 1);
        onChanged();
      },
      (e: Error) => setMessage(e.message),
    );

  return (
    <section class="card page-narrow">
      <div class="card-head">
        <h2>Computers</h2>
        <button type="button" aria-expanded={adding} onClick={() => setAdding(!adding)}>
          ＋ Add
        </button>
      </div>
      <p class="caption">PCs, their plugs, and the collector that reports what is inside each.</p>
      {adding && (
        <AddPanel
          kinds={['computer']}
          onAdded={() => {
            setAdding(false);
            setRev((n) => n + 1);
            onChanged();
          }}
          onCancel={() => setAdding(false)}
        />
      )}
      {error && <p class="note">{error.message}</p>}
      {computers && computers.length === 0 && <p class="note">No computers yet. A PC running the collector adds itself.</p>}
      {computers && computers.length > 0 && (
        <ul class="settings devices">
          {computers.map((c) => (
            <ComputerRow key={c.id} computer={c} meters={meters} run={run} />
          ))}
        </ul>
      )}
      {message && (
        <p class="note error" role="alert">
          {message}
        </p>
      )}
    </section>
  );
}

function ComputerRow({ computer, meters, run }: { computer: Computer; meters: Meter[]; run: (work: Promise<unknown>) => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(computer.name);
  const [confirming, setConfirming] = useState(false);

  // A plug can be chosen if it is free or already this computer's.
  const mine = (c: Meter) => !c.computer_id || c.computer_id === computer.id;
  const wallChoices = meters.filter((c) => c.scope === 'pc' && mine(c));
  const peripheralChoices = meters.filter((c) => c.scope === 'plug' && mine(c));

  const heard = computer.source_id ? collectorState(computer.last_seen, Date.now()) : undefined;
  const status = !heard
    ? 'no collector'
    : heard.state === 'never'
      ? 'collector has never reported'
      : heard.state === 'reporting'
        ? `collector reporting, ${formatAge(heard.ageSeconds)}`
        : `collector not answering, last heard ${formatAge(heard.ageSeconds)}`;

  const setWall = (meterId: string) => {
    // One wall plug per computer: let the old one go first, then tie the new one.
    const work = async () => {
      if (computer.wall_meter_id) await api.updateMeter(computer.wall_meter_id, { computer_id: null });
      if (meterId) await api.updateMeter(meterId, { computer_id: computer.id });
    };
    run(work());
  };

  return (
    <li class="device-row">
      <div class="device-main">
        <span class="grow">
          {heard && <span class={heard.state === 'reporting' ? 'dot on' : 'dot'} role="img" aria-label={heard.state === 'reporting' ? 'reporting' : 'not answering'} />}
          <a href={routePath('hierarchy', 'computer', computer.id)}>{computer.name}</a> <span class="caption">{status}</span>
        </span>
        {confirming ? (
          <>
            <button type="button" onClick={() => run(api.removeComputer(computer.id))}>
              Remove it
            </button>
            <button type="button" onClick={() => setConfirming(false)}>
              Keep
            </button>
          </>
        ) : (
          <>
            <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}>
              Edit
            </button>
            <button type="button" onClick={() => setConfirming(true)} title="Remove the computer (its plugs stay)">
              Remove
            </button>
          </>
        )}
      </div>

      {open && (
        <div class="device-edit">
          <label class="field">
            <span>Name</span>
            <input value={name} maxLength={60} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
          </label>
          <div class="chips">
            <button type="button" disabled={!name.trim() || name.trim() === computer.name} onClick={() => run(api.updateComputer(computer.id, { name: name.trim() }))}>
              Save
            </button>
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
          {wallChoices.length === 0 && <p class="caption block">No PC plugs available.</p>}

          <p class="caption block">Peripherals (each is a separate plug)</p>
          {peripheralChoices.length === 0 && <p class="caption block">No other plugs.</p>}
          <div class="chips">
            {peripheralChoices.map((c) => {
              const tied = c.computer_id === computer.id;
              return (
                <button key={c.id} type="button" aria-pressed={tied} onClick={() => run(api.updateMeter(c.id, { computer_id: tied ? null : computer.id }))}>
                  {c.label}
                </button>
              );
            })}
          </div>
          {computer.source_id && (
            <p class="caption block">
              Its collector reports as <code>{computer.source_id}</code>.
            </p>
          )}
        </div>
      )}
    </li>
  );
}
