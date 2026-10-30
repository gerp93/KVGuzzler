// Appliance groups, in the Connectors tab: where a group is made, named and given its meters. A group is a logical grouping of any meters (not a place in the wiring:
// the members stay where they are in the tree); the Hierarchy tab shows each as one solid line with the total of its members. A meter is in one group at a time.
// A member that another member already contains is listed on the group's page but not added a second time.
import { useState } from 'preact/hooks';
import { AddPanel } from './AddPanel';
import { api } from './api';
import { usePoll } from './hooks';
import type { ApplianceGroup, Meter } from './lib';
import { routePath } from './route';

export function GroupsSetupCard({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  const [rev, setRev] = useState(0);
  const [adding, setAdding] = useState(false);
  const { data: groups, error } = usePoll<ApplianceGroup[]>(api.groups, 30_000, [rev, meters]);
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
        <h2>Appliance groups</h2>
        <button type="button" aria-expanded={adding} onClick={() => setAdding(!adding)}>
          ＋ Add
        </button>
      </div>
      <p class="caption">Meters shown together as one total, like a desk and its computer.</p>
      {adding && (
        <AddPanel
          kinds={['group']}
          onAdded={() => {
            setAdding(false);
            setRev((n) => n + 1);
            onChanged();
          }}
          onCancel={() => setAdding(false)}
        />
      )}
      {error && <p class="note">{error.message}</p>}
      {groups && groups.length === 0 && <p class="note">No groups yet.</p>}
      {groups && groups.length > 0 && (
        <ul class="settings devices">
          {groups.map((g) => (
            <GroupRow key={g.id} group={g} meters={meters} run={run} />
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

function GroupRow({ group, meters, run }: { group: ApplianceGroup; meters: Meter[]; run: (work: Promise<unknown>) => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(group.name);
  const [confirming, setConfirming] = useState(false);

  const members = group.meter_ids.map((id) => meters.find((m) => m.id === id)).filter((m): m is Meter => !!m);

  // Any shown meter that is not in a group yet (the mains is the house itself; a breaker's second clamp is part of the first one's row).
  const secondLegs = new Set(meters.flatMap((m) => (m.pair_with ? [m.pair_with] : [])));
  const available = meters
    .filter((m) => m.enabled && m.scope !== 'mains' && !m.group_id && !secondLegs.has(m.id))
    .sort((a, b) => a.label.localeCompare(b.label));

  return (
    <li class="device-row">
      <div class="device-main">
        <span class="grow">
          <a href={routePath('hierarchy', 'group', group.id)}>{group.name}</a>{' '}
          <span class="caption">
            {members.length} {members.length === 1 ? 'meter' : 'meters'}
          </span>
        </span>
        {confirming ? (
          <>
            <button type="button" onClick={() => run(api.removeGroup(group.id))}>
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
            <button type="button" onClick={() => setConfirming(true)} title="Remove the group (its meters stay)">
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
            <button type="button" disabled={!name.trim() || name.trim() === group.name} onClick={() => run(api.renameGroup(group.id, name.trim()))}>
              Save
            </button>
          </div>

          <p class="caption block">Meters in this group (click one to take it out)</p>
          {members.length === 0 && <p class="caption block">None.</p>}
          <div class="chips">
            {members.map((m) => (
              <button key={m.id} type="button" onClick={() => run(api.updateMeter(m.id, { group_id: null }))}>
                {m.label} ✕
              </button>
            ))}
          </div>
          <label class="field">
            <span>Add a meter to this group</span>
            <select
              value=""
              onChange={(e) => {
                const id = (e.target as HTMLSelectElement).value;
                if (id) run(api.updateMeter(id, { group_id: group.id }));
              }}
            >
              <option value="">Choose a meter…</option>
              {available.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.label}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
    </li>
  );
}
