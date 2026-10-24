// Settings > Channels: every input the server reads, grouped by the device it is on. Inputs in use come first, as the panels are wired (a
// sub-panel's feed, then the circuits inside it); inputs nothing is clamped on are folded away; the fiddly parts (a backwards clamp, a
// breaker's two legs, which panel a circuit is in) sit behind one "Wiring" button on each row. Nothing here is sent to a device.
import { useState } from 'preact/hooks';
import { api } from './api';
import { KIND_NAMES } from './DevicesCard';
import { usePoll } from './hooks';
import { arrangeByPanel, type Channel, type Device } from './lib';

type Change = Parameters<typeof api.updateChannel>[1];

/** The name a Refoss input has on the monitor itself (A1 to C6), or null for anything else. */
function inputName(c: Channel): string | null {
  const n = c.index;
  if (!c.device_id.startsWith('refoss-') || n == null || n < 1 || n > 18) return null;
  return `${'ABC'[Math.floor((n - 1) / 6)]}${((n - 1) % 6) + 1}`;
}

const nameOf = (c: Channel) => inputName(c) ?? c.label;

function ChannelRow({ channel, all, depth, onChanged }: { channel: Channel; all: Channel[]; depth: number; onChanged: () => void }) {
  const [label, setLabel] = useState(channel.label);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState('');
  const second = channel.pair_with ? all.find((c) => c.id === channel.pair_with) : undefined;
  const refoss = channel.device_id.startsWith('refoss-');
  const inPanel = channel.scope === 'circuit';

  const run = (id: string, body: Change) =>
    api.updateChannel(id, body).then(
      () => {
        setError('');
        onChanged();
      },
      (e: Error) => setError(e.message),
    );

  // Which clamps this row stands for ("A2 + B2"), shown only when the name does not already say it.
  const legs = [channel, ...(second ? [second] : [])].map(nameOf).join(' + ');

  // The second leg could be any other input of this monitor that is not part of a pair already.
  const taken = new Set(all.flatMap((c) => (c.pair_with ? [c.pair_with] : [])));
  const legOptions = all.filter((c) => c.device_id === channel.device_id && c.id !== channel.id && !c.pair_with && (!taken.has(c.id) || c.id === channel.pair_with));

  // The circuits this one could sit inside: any other circuit that is shown and is not itself inside this one.
  const inside = new Set<string>([channel.id]);
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
  const feed = channel.parent_id ? all.find((c) => c.id === channel.parent_id) : undefined;

  // A plug on a computer or in a group is shown there; any other plug can say which circuit it is on.
  const placeable = inPanel || (channel.scope === 'plug' && !channel.computer_id && !channel.group_id);
  const hasWiring = refoss || placeable;
  const summary = [legs !== label ? legs : '', channel.scope === 'mains' ? 'the whole house' : '', feed && !feed.enabled ? `inside ${feed.label} (hidden)` : '']
    .filter(Boolean)
    .join(' · ');

  return (
    <li class="chan" style={depth > 0 ? { marginLeft: `${depth * 1.25}rem` } : undefined}>
      <div class="chan-main">
        <label class="grow">
          <span class="visually-hidden">Name for {legs}</span>
          <input
            value={label}
            maxLength={60}
            onInput={(e) => setLabel((e.target as HTMLInputElement).value)}
            onBlur={() => label !== channel.label && void run(channel.id, { label })}
            onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
          />
        </label>
        {summary && <span class="caption chan-tag">{summary}</span>}
        <div class="chips">
          {hasWiring && (
            <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}>
              Wiring
            </button>
          )}
          <button type="button" onClick={() => void run(channel.id, { enabled: false })}>
            Hide
          </button>
        </div>
      </div>

      {open && (
        <div class="chan-wiring">
          {refoss && (
            <>
              <fieldset class="clamps">
                <legend>Clamp direction</legend>
                {[channel, ...(second ? [second] : [])].map((c) => (
                  <label key={c.id} class="check">
                    <input type="checkbox" checked={c.invert === true} onChange={(e) => void run(c.id, { invert: (e.target as HTMLInputElement).checked })} />
                    Flip {nameOf(c)}
                  </label>
                ))}
                <p class="caption block">A load reads positive. Flip a clamp that reads backwards here, or turn the clamp round in the panel, not both.</p>
              </fieldset>
              <label class="field">
                <span>A 240 V breaker has two clamps. Second clamp of this breaker</span>
                <select value={channel.pair_with ?? ''} onChange={(e) => void run(channel.id, { pair_with: (e.target as HTMLSelectElement).value || null })}>
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
                <select value={channel.scope} onChange={(e) => void run(channel.id, { scope: (e.target as HTMLSelectElement).value as 'circuit' | 'mains' })}>
                  <option value="circuit">One circuit</option>
                  <option value="mains">The whole house (a monitor on the main panel)</option>
                </select>
              </label>
            </>
          )}
          {placeable && (
            <label class="field">
              <span>{inPanel ? 'Which panel it is in' : 'Which circuit it is plugged into'}</span>
              <select value={channel.parent_id ?? ''} onChange={(e) => void run(channel.id, { parent_id: (e.target as HTMLSelectElement).value || null })}>
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

function Unused({ channel, onChanged }: { channel: Channel; onChanged: () => void }) {
  const [error, setError] = useState('');
  return (
    <li class="chan-main">
      <span class="grow">{channel.label}</span>
      <button
        type="button"
        onClick={() => void api.updateChannel(channel.id, { enabled: true }).then(onChanged, (e: Error) => setError(e.message))}
      >
        Show
      </button>
      {error && <span class="note error">{error}</span>}
    </li>
  );
}

export function ChannelsCard({ channels, onChanged }: { channels: Channel[]; onChanged: () => void }) {
  const { data: devices } = usePoll<Device[]>(api.devices, 30_000, [channels.length]);
  const byDevice = new Map<string, Channel[]>();
  for (const c of channels) byDevice.set(c.device_id, [...(byDevice.get(c.device_id) ?? []), c]);
  // Devices in the order the server lists them; any channel of a device it no longer lists goes last under its id.
  const order = [...(devices ?? []).map((d) => d.id).filter((id) => byDevice.has(id)), ...[...byDevice.keys()].filter((id) => !(devices ?? []).some((d) => d.id === id))];

  return (
    <section class="card">
      <h2>Channels</h2>
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
          <div key={id} class="chan-device">
            <h3>
              {device ? device.name || device.host : id}
              {kind && <span class="caption"> {kind}</span>}
            </h3>
            {used.length === 0 ? (
              <p class="caption">Nothing on this device is shown right now.</p>
            ) : (
              <ul class="channels">
                {used.map(({ channel, depth }) => (
                  <ChannelRow key={channel.id} channel={channel} all={channels} depth={depth} onChanged={onChanged} />
                ))}
              </ul>
            )}
            {unused.length > 0 && (
              <details>
                <summary>
                  {unused.length} hidden {unused.length === 1 ? 'input' : 'inputs'}
                </summary>
                <ul class="channels">
                  {unused.map((c) => (
                    <Unused key={c.id} channel={c} onChanged={onChanged} />
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
