import { useState } from 'preact/hooks';
import { api } from './api';
import { usePoll } from './hooks';
import type { Device } from './lib';

export const KIND_NAMES: Record<string, string> = { shelly_plug: 'Shelly plug', refoss: 'Refoss monitor', pc: 'PC' };

function lastSeen(d: Device): string {
  if (!d.last_seen) return 'not answered yet';
  const seconds = Math.max(0, Math.round((Date.now() - Date.parse(d.last_seen)) / 1000));
  return seconds < 5 ? 'answering now' : seconds < 90 ? `last answered ${seconds} s ago` : `last answered ${Math.round(seconds / 60)} min ago`;
}

export function DevicesCard({ onChanged }: { onChanged: () => void }) {
  const [rev, setRev] = useState(0);
  const { data: devices, error } = usePoll<Device[]>(api.devices, 5000, [rev]);
  const [host, setHost] = useState('');
  const [name, setName] = useState('');
  const [isPc, setIsPc] = useState(false);
  const [refossHost, setRefossHost] = useState('');
  const [refossName, setRefossName] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState('');

  const changed = () => {
    setRev((n) => n + 1);
    onChanged();
  };

  const add = () => {
    setBusy(true);
    setMessage('');
    api
      .addDevice({ kind: 'shelly_plug', host: host.trim(), name: name.trim(), scope: isPc ? 'pc' : 'plug' })
      .then(
        () => {
          setHost('');
          setName('');
          setIsPc(false);
          changed();
        },
        (e: Error) => setMessage(e.message),
      )
      .finally(() => setBusy(false));
  };

  const addRefoss = () => {
    setBusy(true);
    setMessage('');
    api
      .addDevice({ kind: 'refoss', host: refossHost.trim(), name: refossName.trim() })
      .then(
        () => {
          setRefossHost('');
          setRefossName('');
          changed();
        },
        (e: Error) => setMessage(e.message),
      )
      .finally(() => setBusy(false));
  };

  const remove = (id: string) =>
    api.removeDevice(id).then(
      () => {
        setConfirming('');
        changed();
      },
      (e: Error) => setMessage(e.message),
    );

  return (
    <section class="card">
      <h2>Devices</h2>
      {error && <p class="note">{error.message}</p>}
      {devices && devices.length === 0 && <p class="note">No devices yet. Add a smart plug or a Refoss monitor below.</p>}
      {devices && devices.length > 0 && (
        <ul class="settings">
          {devices.map((d) => (
            <li key={d.id}>
              <span class="grow">
                <span class={d.online ? 'dot on' : 'dot'} role="img" aria-label={d.online ? 'answering' : 'not answering'} />
                {d.name || d.id}{' '}
                <span class="caption">
                  {KIND_NAMES[d.kind] ?? d.kind}
                  {d.host ? `, ${d.host}` : ''}, {lastSeen(d)}
                </span>
              </span>
              {confirming === d.id ? (
                <>
                  <button type="button" onClick={() => void remove(d.id)}>
                    Stop polling
                  </button>
                  <button type="button" onClick={() => setConfirming('')}>
                    Keep
                  </button>
                </>
              ) : (
                <button type="button" onClick={() => setConfirming(d.id)}>
                  Remove
                </button>
              )}
            </li>
          ))}
        </ul>
      )}

      <fieldset class="device-form">
        <legend>Add a Shelly plug</legend>
        <label class="field">
          <span>Address (like 192.168.1.50)</span>
          <input value={host} onInput={(e) => setHost((e.target as HTMLInputElement).value)} />
        </label>
        <label class="field">
          <span>Name (what is plugged into it)</span>
          <input value={name} maxLength={60} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
        </label>
        <label class="check">
          <input type="checkbox" checked={isPc} onChange={(e) => setIsPc((e.target as HTMLInputElement).checked)} />
          This is my PC's plug
        </label>
        {isPc && (
          <p class="caption">
            The PC is on one of your circuits already, so it is shown as its own slice and never added to the house or circuit figures.
          </p>
        )}
        <div class="chips">
          <button type="button" onClick={add} disabled={busy || host.trim() === ''}>
            {busy ? 'Checking the plug…' : 'Add plug'}
          </button>
        </div>
      </fieldset>
      <fieldset class="device-form">
        <legend>Add a Refoss energy monitor</legend>
        <label class="field">
          <span>Address (like 192.168.68.88)</span>
          <input value={refossHost} onInput={(e) => setRefossHost((e.target as HTMLInputElement).value)} />
        </label>
        <label class="field">
          <span>Name (where it is, like Garage panel)</span>
          <input value={refossName} maxLength={60} onInput={(e) => setRefossName((e.target as HTMLInputElement).value)} />
        </label>
        <p class="caption">
          Only the EM16P has been tried. Its local login must be off in the Refoss app. Every input is added; the ones with nothing clamped on them are hidden, and
          you rename, flip and pair the rest under Meters below.
        </p>
        <div class="chips">
          <button type="button" onClick={addRefoss} disabled={busy || refossHost.trim() === ''}>
            {busy ? 'Checking the monitor…' : 'Add monitor'}
          </button>
        </div>
      </fieldset>
      <p class="caption">The server reads a device once before it keeps it, and only ever reads: it cannot switch a plug or change a monitor.</p>
      {message && (
        <p class="note error" role="alert">
          {message}
        </p>
      )}
    </section>
  );
}
