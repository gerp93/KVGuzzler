import { useState } from 'preact/hooks';
import { Hint } from './Hint';
import { api } from './api';
import { usePoll } from './hooks';
import type { Device, Meter } from './lib';

export const KIND_NAMES: Record<string, string> = { shelly_plug: 'Shelly plug', refoss: 'Refoss monitor', pc: 'PC' };

function lastSeen(d: Device): string {
  if (!d.last_seen) return 'not answered yet';
  const seconds = Math.max(0, Math.round((Date.now() - Date.parse(d.last_seen)) / 1000));
  return seconds < 5 ? 'answering now' : seconds < 90 ? `last answered ${seconds} s ago` : `last answered ${Math.round(seconds / 60)} min ago`;
}

/** One device: its name, kind, address and state, and the way to change the name or the address or to stop polling it. */
function DeviceRow({ device, onChanged }: { device: Device; onChanged: () => void }) {
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(device.name ?? '');
  const [host, setHost] = useState(device.host);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [confirming, setConfirming] = useState(false);

  const open = () => {
    setName(device.name ?? '');
    setHost(device.host);
    setMessage('');
    setEditing(true);
  };
  const changedHost = host.trim() !== device.host;
  const changed = changedHost || name.trim() !== (device.name ?? '');

  const save = () => {
    setBusy(true);
    setMessage('');
    api
      .updateDevice(device.id, { ...(name.trim() !== (device.name ?? '') ? { name: name.trim() } : {}), ...(changedHost ? { host: host.trim() } : {}) })
      .then(
        () => {
          setEditing(false);
          onChanged();
        },
        (e: Error) => setMessage(e.message),
      )
      .finally(() => setBusy(false));
  };

  const remove = () =>
    api.removeDevice(device.id).then(
      () => {
        setConfirming(false);
        onChanged();
      },
      (e: Error) => setMessage(e.message),
    );

  return (
    <li class="device-row">
      <div class="device-main">
        <span class="grow">
          <span class={device.online ? 'dot on' : 'dot'} role="img" aria-label={device.online ? 'answering' : 'not answering'} />
          {device.name || device.id}{' '}
          <span class="caption">
            {KIND_NAMES[device.kind] ?? device.kind}
            {device.host ? `, ${device.host}` : ''}, {lastSeen(device)}
          </span>
        </span>
        {confirming ? (
          <>
            <button type="button" onClick={() => void remove()}>
              Stop polling
            </button>
            <button type="button" onClick={() => setConfirming(false)}>
              Keep
            </button>
          </>
        ) : (
          <>
            {device.kind !== 'pc' && device.host !== '' && (
              <button type="button" aria-expanded={editing} onClick={() => (editing ? setEditing(false) : open())}>
                Edit
              </button>
            )}
            <button type="button" onClick={() => setConfirming(true)}>
              Remove
            </button>
          </>
        )}
      </div>
      {editing && (
        <div class="device-edit">
          <label class="field">
            <span>Name (a plug's meter is named the same)</span>
            <input value={name} maxLength={60} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
          </label>
          <label class="field">
            <span>Address (a plain host name or IP address)</span>
            <input value={host} onInput={(e) => setHost((e.target as HTMLInputElement).value)} />
          </label>
          <p class="caption">
            If the device got a new address from the router, put it here. Its meters keep their names, settings and history. A new address is read once first: if nothing
            answers there like this device, nothing changes.
          </p>
          <div class="chips">
            <button type="button" onClick={save} disabled={busy || !changed || host.trim() === '' || name.trim() === ''}>
              {busy ? (changedHost ? 'Checking the new address…' : 'Saving…') : 'Save'}
            </button>
            <button type="button" onClick={() => setEditing(false)}>
              Cancel
            </button>
          </div>
        </div>
      )}
      {message && (
        <p class="note error" role="alert">
          {message}
        </p>
      )}
    </li>
  );
}

export function DevicesCard({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  const [rev, setRev] = useState(0);
  const { data: devices, error } = usePoll<Device[]>(api.devices, 5000, [meters.length, rev]);
  const refresh = () => {
    setRev((n) => n + 1);
    onChanged();
  };

  return (
    <section class="card wide">
      <h2>Devices</h2>
      <Hint>
        What the server reads. Add one with the + Add button at the top; change a device's name or address, or stop polling it, here. The server only ever reads a
        device: it cannot switch a plug or change a monitor.
      </Hint>
      {error && <p class="note">{error.message}</p>}
      {devices && devices.length === 0 && <p class="note">No devices yet.</p>}
      {devices && devices.length > 0 && (
        <ul class="settings devices">
          {devices.map((d) => (
            <DeviceRow key={d.id} device={d} onChanged={refresh} />
          ))}
        </ul>
      )}
    </section>
  );
}
