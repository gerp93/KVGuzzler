// Settings > Meters: every input the server reads, grouped by the device it is on. Inputs in use come first, as the panels are wired (a
// sub-panel's feed, then the circuits inside it); inputs nothing is clamped on are folded away; the fiddly parts (a backwards clamp, a
// breaker's two legs, which panel a circuit is in) sit behind one "Wiring" button on each row. Nothing here is sent to a device.
import { useState } from 'preact/hooks';
import { api } from './api';
import { KIND_NAMES } from './DevicesCard';
import { usePoll } from './hooks';
import { arrangeByPanel, type Meter, type Device } from './lib';

type Change = Parameters<typeof api.updateMeter>[1];

/** The name a Refoss input has on the monitor itself (A1 to C6), or null for anything else. */
function inputName(c: Meter): string | null {
  const n = c.index;
  if (!c.device_id.startsWith('refoss-') || n == null || n < 1 || n > 18) return null;
  return `${'ABC'[Math.floor((n - 1) / 6)]}${((n - 1) % 6) + 1}`;
}

const nameOf = (c: Meter) => inputName(c) ?? c.label;

function MeterRow({ meter, all, depth, onChanged }: { meter: Meter; all: Meter[]; depth: number; onChanged: () => void }) {
  const [label, setLabel] = useState(meter.label);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState('');
  const second = meter.pair_with ? all.find((c) => c.id === meter.pair_with) : undefined;
  const refoss = meter.device_id.startsWith('refoss-');
  const inPanel = meter.scope === 'circuit';

  const run = (id: string, body: Change) =>
    api.updateMeter(id, body).then(
      () => {
        setError('');
        onChanged();
      },
      (e: Error) => setError(e.message),
    );

  // Which clamps this row stands for ("A2 + B2"), shown only when the name does not already say it.
  const legs = [meter, ...(second ? [second] : [])].map(nameOf).join(' + ');

  // The second leg could be any other input of this monitor that is not part of a pair already.
  const taken = new Set(all.flatMap((c) => (c.pair_with ? [c.pair_with] : [])));
  const legOptions = all.filter((c) => c.device_id === meter.device_id && c.id !== meter.id && !c.pair_with && (!taken.has(c.id) || c.id === meter.pair_with));

  // The circuits this one could sit inside: any other circuit that is shown and is not itself inside this one.
  const inside = new Set<string>([meter.id]);
  for (let grew = true; grew; ) {
    grew = false;
    for (const c of all) {
      if (c.parent_id && inside.has(c.parent_id) && !inside.has(c.id)) {
        inside.add(c.id);
        grew = true;
      }
    }
  }
  const feeds = all.filter((c) => c.scope === 'circuit' && c.enabled && !inside.has(c.id));
  const feed = meter.parent_id ? all.find((c) => c.id === meter.parent_id) : undefined;

  // Anything but the mains can say which circuit it is on: a plug, the PC's wall plug, a sub-panel's circuit.
  const placeable = meter.scope !== 'mains';
  const hasWiring = refoss || placeable;
  const summary = [legs !== label ? legs : '', meter.scope === 'mains' ? 'the whole house' : '', feed && !feed.enabled ? `inside ${feed.label} (hidden)` : '']
    .filter(Boolean)
    .join(' · ');

  return (
    <li class="meter-row" style={depth > 0 ? { marginLeft: `${depth * 1.25}rem` } : undefined}>
      <div class="meter-main">
        <label class="grow">
          <span class="visually-hidden">Name for {legs}</span>
          <input
            value={label}
            maxLength={60}
            onInput={(e) => setLabel((e.target as HTMLInputElement).value)}
            onBlur={() => label !== meter.label && void run(meter.id, { label })}
            onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
          />
        </label>
        {summary && <span class="caption meter-tag">{summary}</span>}
        <div class="chips">
          {hasWiring && (
            <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}>
              Wiring
            </button>
          )}
          <button type="button" onClick={() => void run(meter.id, { enabled: false })}>
            Hide
          </button>
        </div>
      </div>

      {open && (
        <div class="meter-wiring">
          {refoss && (
            <>
              <fieldset class="clamps">
                <legend>Clamp direction</legend>
                {[meter, ...(second ? [second] : [])].map((c) => (
                  <label key={c.id} class="check">
                    <input type="checkbox" checked={c.invert === true} onChange={(e) => void run(c.id, { invert: (e.target as HTMLInputElement).checked })} />
                    Flip {nameOf(c)}
                  </label>
                ))}
                <p class="caption block">A load reads positive. Flip a clamp that reads backwards here, or turn the clamp round in the panel, not both.</p>
              </fieldset>
              <label class="field">
                <span>A 240 V breaker has two clamps. Second clamp of this breaker</span>
                <select value={meter.pair_with ?? ''} onChange={(e) => void run(meter.id, { pair_with: (e.target as HTMLSelectElement).value || null })}>
                  <option value="">None, one wire</option>
                  {legOptions.map((c) => (
                    <option key={c.id} value={c.id}>
                      plus {nameOf(c)}
                    </option>
                  ))}
                </select>
              </label>
              <label class="field">
                <span>What this measures</span>
                <select value={meter.scope} onChange={(e) => void run(meter.id, { scope: (e.target as HTMLSelectElement).value as 'circuit' | 'mains' })}>
                  <option value="circuit">One circuit</option>
                  <option value="mains">The whole house (a monitor on the main panel)</option>
                </select>
              </label>
            </>
          )}
          {placeable && (
            <label class="field">
              <span>{inPanel ? 'Which panel it is in' : 'Which circuit it is plugged into'}</span>
              <select value={meter.parent_id ?? ''} onChange={(e) => void run(meter.id, { parent_id: (e.target as HTMLSelectElement).value || null })}>
                <option value="">{inPanel ? 'The main panel' : 'Not said'}</option>
                {feeds.map((c) => (
                  <option key={c.id} value={c.id}>
                    {inPanel ? `The sub-panel fed by ${c.label}` : c.label}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
      )}
      {error && <p class="note error">{error}</p>}
    </li>
  );
}

function Unused({ meter, onChanged }: { meter: Meter; onChanged: () => void }) {
  const [error, setError] = useState('');
  return (
    <li class="meter-main">
      <span class="grow">{meter.label}</span>
      <button
        type="button"
        onClick={() => void api.updateMeter(meter.id, { enabled: true }).then(onChanged, (e: Error) => setError(e.message))}
      >
        Show
      </button>
      {error && <span class="note error">{error}</span>}
    </li>
  );
}

export function MetersCard({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  const { data: devices } = usePoll<Device[]>(api.devices, 30_000, [meters.length]);
  const byDevice = new Map<string, Meter[]>();
  for (const c of meters) byDevice.set(c.device_id, [...(byDevice.get(c.device_id) ?? []), c]);
  // Devices in the order the server lists them; any meter of a device it no longer lists goes last under its id.
  const order = [...(devices ?? []).map((d) => d.id).filter((id) => byDevice.has(id)), ...[...byDevice.keys()].filter((id) => !(devices ?? []).some((d) => d.id === id))];

  return (
    <section class="card">
      <h2>Meters</h2>
      <p class="caption">
        Name what each input measures, hide the ones with nothing on them, and open Wiring to fix a clamp that reads backwards, join the two clamps of a 240 V
        breaker, say which panel a circuit is in, or which circuit a plug is on. Whatever sits inside a circuit is shown beneath it, and the circuit's
        own reading already counts it, so nothing is added twice. This only changes how this dashboard shows things; nothing is sent to a device.
      </p>
      {order.map((id) => {
        const device = devices?.find((d) => d.id === id);
        const mine = byDevice.get(id) ?? [];
        // A breaker's second clamp is part of the first one's row, so it is not listed on its own.
        const secondLegs = new Set(mine.flatMap((c) => (c.pair_with ? [c.pair_with] : [])));
        const listed = mine.filter((c) => !secondLegs.has(c.id));
        const used = arrangeByPanel(listed.filter((c) => c.enabled));
        const unused = listed.filter((c) => !c.enabled);
        const kind = device ? [KIND_NAMES[device.kind] ?? device.kind, device.model && device.kind === 'refoss' ? device.model.toUpperCase() : ''].filter(Boolean).join(' ') : '';
        return (
          <div key={id} class="meter-device">
            <h3>
              {device ? device.name || device.host : id}
              {kind && <span class="caption"> {kind}</span>}
            </h3>
            {used.length === 0 ? (
              <p class="caption">Nothing on this device is shown right now.</p>
            ) : (
              <ul class="meters">
                {used.map(({ meter, depth }) => (
                  <MeterRow key={meter.id} meter={meter} all={meters} depth={depth} onChanged={onChanged} />
                ))}
              </ul>
            )}
            {unused.length > 0 && (
              <details>
                <summary>
                  {unused.length} hidden {unused.length === 1 ? 'input' : 'inputs'}
                </summary>
                <ul class="meters">
                  {unused.map((c) => (
                    <Unused key={c.id} meter={c} onChanged={onChanged} />
                  ))}
                </ul>
              </details>
            )}
          </div>
        );
      })}
    </section>
  );
}
