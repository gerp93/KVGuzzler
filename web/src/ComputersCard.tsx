// Computers: each is a row with its setup total (the wall plug plus its peripherals, which are separate plugs, so adding them is sound) in the same
// columns as the tree. The arrow opens what it is made of: its name, its wall plug, its peripherals. The wall plug and a peripheral stay where they
// are plugged in in the tree; this is only the grouping, like a group.
import { useState } from 'preact/hooks';
import { api } from './api';
import { usePoll } from './hooks';
import { formatKwh, formatMoney, formatWatts, type Computer, type ComputerDetail, type Meter } from './lib';
import { routePath } from './route';

interface Loaded {
  computers: Computer[];
  details: Map<string, ComputerDetail>;
}

export function ComputersCard({
  meters, onChanged, watts, from, to,
}: {
  meters: Meter[];
  onChanged: () => void;
  /** What each meter draws right now, by meter id. */
  watts: Map<string, number>;
  /** The period the Energy and Cost columns follow (the one picked at the top of the tab). */
  from?: string;
  to?: string;
}) {
  const [rev, setRev] = useState(0);
  const { data, error } = usePoll<Loaded>(
    async () => {
      const computers = await api.computers();
      const details = new Map<string, ComputerDetail>();
      if (from && to) await Promise.all(computers.map(async (c) => details.set(c.id, await api.computerDetail(c.id, from, to))));
      return { computers, details };
    },
    30_000,
    [rev, meters, from, to],
  );
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
    <section class="card wide">
      <h2>Computers</h2>
      <p class="caption">
        A computer is its wall plug (which measures the whole PC) plus its peripherals (each a separate plug: the monitor, the speakers). The row shows that setup's
        total; each plug stays where it is plugged in in the tree. Open a row to choose its plugs. A PC that has the KVGuzzler collector adds itself the first time it
        reports; add one by hand with + Add.
      </p>
      {error && <p class="note">{error.message}</p>}
      {data && data.computers.length === 0 && <p class="note">No computers yet.</p>}
      {data && data.computers.length > 0 && (
        <>
          <div class="hier-head" aria-hidden="true">
            <span class="hier-tag" />
            <span class="num">Now</span>
            <span class="num">Energy</span>
            <span class="num">Cost</span>
          </div>
          <ul class="hier">
            {data.computers.map((c) => (
              <ComputerRow key={c.id} computer={c} detail={data.details.get(c.id)} meters={meters} watts={watts} run={run} />
            ))}
          </ul>
        </>
      )}
      {message && (
        <p class="note error" role="alert">
          {message}
        </p>
      )}
    </section>
  );
}

function ComputerRow({
  computer, detail, meters, watts, run,
}: {
  computer: Computer;
  detail?: ComputerDetail;
  meters: Meter[];
  watts: Map<string, number>;
  run: (work: Promise<unknown>) => void;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(computer.name);
  const [confirming, setConfirming] = useState(false);

  // A plug can be chosen if it is free or already this computer's.
  const mine = (c: Meter) => !c.computer_id || c.computer_id === computer.id;
  const wallChoices = meters.filter((c) => c.scope === 'pc' && mine(c));
  const peripheralChoices = meters.filter((c) => c.scope === 'plug' && mine(c));

  const plugs = [computer.wall_meter_id, ...computer.peripheral_meter_ids].filter((id): id is string => !!id);
  const nowParts = plugs.map((id) => watts.get(id)).filter((w): w is number => w !== undefined);
  const now = nowParts.length > 0 ? nowParts.reduce((t, w) => t + w, 0) : null;
  const count = plugs.length;

  const setWall = (meterId: string) => {
    // One wall plug per computer: let the old one go first, then tie the new one.
    const work = async () => {
      if (computer.wall_meter_id) await api.updateMeter(computer.wall_meter_id, { computer_id: null });
      if (meterId) await api.updateMeter(meterId, { computer_id: computer.id });
    };
    run(work());
  };

  return (
    <li class="hier-row">
      <div class="hier-main">
        <button type="button" class="twisty" aria-expanded={open} aria-label={`Plugs of ${computer.name}`} onClick={() => setOpen(!open)}>
          <span aria-hidden="true">▸</span>
        </button>
        <span />
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
        <span class="caption hier-tag" title="The wall plug plus the peripherals">
          setup · {count} {count === 1 ? 'plug' : 'plugs'}
        </span>
        <span class="num hier-watts">{now !== null ? formatWatts(now) : ''}</span>
        <span class="num hier-energy">{detail?.setup_kwh != null ? formatKwh(detail.setup_kwh) : ''}</span>
        <span class="num hier-cost">{detail?.setup_cost != null ? formatMoney(detail.setup_cost, detail.cost_estimated) : detail ? '—' : ''}</span>
        <div class="hier-actions">
          <a class="button icon" href={routePath('hierarchy', 'computer', computer.id)} title="Open the computer's page" aria-label={`Open ${computer.name}`}>
            ›
          </a>
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
            <button type="button" onClick={() => setConfirming(true)} title="Remove the computer (its plugs stay)" aria-label={`Remove ${computer.name}`}>
              ✕
            </button>
          )}
        </div>
      </div>

      {open && (
        <div class="hier-panel">
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
          {wallChoices.length === 0 && <p class="caption block">Add a plug marked "my PC's plug" with + Add to choose it here.</p>}

          <p class="caption block">Peripherals (each is a separate plug)</p>
          {peripheralChoices.length === 0 && <p class="caption block">No other plugs to tie. Add a plug with + Add first.</p>}
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
