import { useState } from 'preact/hooks';
import { api } from './api';
import { usePoll } from './hooks';
import type { Device, Meter } from './lib';

export const KIND_NAMES: Record<string, string> = { shelly_plug: 'Shelly plug', refoss: 'Refoss monitor', pc: 'PC' };

function lastSeen(d: Device): string {
  if (!d.last_seen) return 'not answered yet';
  const seconds = Math.max(0, Math.round((Date.now() - Date.parse(d.last_seen)) / 1000));
  return seconds < 5 ? 'answering now' : seconds < 90 ? `last answered ${seconds} s ago` : `last answered ${Math.round(seconds / 60)} min ago`;
}

export function DevicesCard({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  const { data: devices, error } = usePoll<Device[]>(api.devices, 5000, [meters.length]);
  const [message, setMessage] = useState('');
  const [confirming, setConfirming] = useState('');

  const remove = (id: string) =>
    api.removeDevice(id).then(
      () => {
        setConfirming('');
        onChanged();
      },
      (e: Error) => setMessage(e.message),
    );

  return (
    <section class="card wide">
      <h2>Devices</h2>
      <p class="caption">What the server reads. Add one with the + Add button at the top. The server only ever reads a device: it cannot switch a plug or change a monitor.</p>
      {error && <p class="note">{error.message}</p>}
      {devices && devices.length === 0 && <p class="note">No devices yet.</p>}
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
      {message && (
        <p class="note error" role="alert">
          {message}
        </p>
      )}
    </section>
  );
}
