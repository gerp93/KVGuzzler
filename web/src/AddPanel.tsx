// The "+ Add" panel of the Hierarchy tab: a new Shelly plug, a Refoss monitor, a computer or an appliance group. What is added shows up in the
// tree, and is edited there (the arrow on its row), so nothing about setting it up lives in Settings. The server reads a device once before it
// keeps it, and only ever reads: it cannot switch a plug or change a monitor.
import { useState } from 'preact/hooks';
import { api } from './api';

type Kind = 'plug' | 'refoss' | 'computer' | 'group';

const KINDS: { id: Kind; label: string }[] = [
  { id: 'plug', label: 'Shelly plug' },
  { id: 'refoss', label: 'Refoss monitor' },
  { id: 'computer', label: 'Computer' },
  { id: 'group', label: 'Appliance group' },
];

export function AddPanel({ onAdded, onCancel }: { onAdded: () => void; onCancel: () => void }) {
  const [kind, setKind] = useState<Kind>('plug');
  const [host, setHost] = useState('');
  const [name, setName] = useState('');
  const [isPc, setIsPc] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  const needsHost = kind === 'plug' || kind === 'refoss';
  const ready = needsHost ? host.trim() !== '' : name.trim() !== '';

  const add = () => {
    setBusy(true);
    setMessage('');
    const work =
      kind === 'plug'
        ? api.addDevice({ kind: 'shelly_plug', host: host.trim(), name: name.trim(), scope: isPc ? 'pc' : 'plug' })
        : kind === 'refoss'
          ? api.addDevice({ kind: 'refoss', host: host.trim(), name: name.trim() })
          : kind === 'computer'
            ? api.addComputer(name.trim())
            : api.addGroup(name.trim());
    work
      .then(
        () => {
          setHost('');
          setName('');
          setIsPc(false);
          onAdded();
        },
        (e: Error) => setMessage(e.message),
      )
      .finally(() => setBusy(false));
  };

  return (
    <fieldset class="device-form add-panel">
      <legend>Add to the hierarchy</legend>
      <div class="chips" role="group" aria-label="What to add">
        {KINDS.map((k) => (
          <button key={k.id} type="button" aria-pressed={kind === k.id} onClick={() => setKind(k.id)}>
            {k.label}
          </button>
        ))}
      </div>

      {needsHost && (
        <label class="field">
          <span>Address (like {kind === 'refoss' ? '192.168.68.88' : '192.168.1.50'})</span>
          <input value={host} onInput={(e) => setHost((e.target as HTMLInputElement).value)} />
        </label>
      )}
      <label class="field">
        <span>
          {kind === 'plug' ? 'Name (what is plugged into it)' : kind === 'refoss' ? 'Name (where it is, like Garage panel)' : kind === 'computer' ? 'Name of the computer' : 'Name of the group'}
        </span>
        <input value={name} maxLength={60} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
      </label>

      {kind === 'plug' && (
        <>
          <label class="check">
            <input type="checkbox" checked={isPc} onChange={(e) => setIsPc((e.target as HTMLInputElement).checked)} />
            PC plug
          </label>
          {isPc && <p class="caption">Measures the whole PC. Set its circuit in the tree afterwards.</p>}
        </>
      )}
      {kind === 'refoss' && (
        <p class="caption">Tested on the EM16P. Turn off local login in the Refoss app. Inputs with nothing clamped on them are hidden.</p>
      )}
      {kind === 'computer' && <p class="caption">Ties a wall plug and its peripherals together. A PC running the collector adds itself.</p>}
      {kind === 'group' && <p class="caption">Several plugs shown as one total.</p>}

      <div class="chips">
        <button type="button" onClick={add} disabled={busy || !ready}>
          {busy ? (kind === 'plug' ? 'Checking the plug…' : kind === 'refoss' ? 'Checking the monitor…' : 'Adding…') : `Add ${KINDS.find((k) => k.id === kind)!.label.toLowerCase()}`}
        </button>
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </div>
      {message && (
        <p class="note error" role="alert">
          {message}
        </p>
      )}
    </fieldset>
  );
}
