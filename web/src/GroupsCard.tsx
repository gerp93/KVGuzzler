import { useState } from 'preact/hooks';
import { api } from './api';
import { usePoll } from './hooks';
import type { ApplianceGroup, Meter } from './lib';

/** Settings for appliance groups: several plugs shown as one appliance (an entertainment center on two power strips). */
export function GroupsCard({ meters, onChanged }: { meters: Meter[]; onChanged: () => void }) {
  const [rev, setRev] = useState(0);
  const { data: groups, error } = usePoll<ApplianceGroup[]>(api.groups, 10_000, [rev, meters]);
  const [name, setName] = useState('');
  const [message, setMessage] = useState('');
  const [confirming, setConfirming] = useState('');

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
    <section class="card">
      <h2>Appliance groups</h2>
      <p class="caption">
        For something that runs through more than one plug, like an entertainment center on two power strips: group the plugs and
        The Hierarchy tab lists them as one, with their energy and cost added. Each plug still records on its own.
      </p>
      {error && <p class="note">{error.message}</p>}
      {groups && groups.length === 0 && <p class="note">No groups yet.</p>}

      {groups?.map((g) => (
        <GroupBlock
          key={g.id}
          group={g}
          meters={meters}
          confirming={confirming === g.id}
          onConfirm={(on) => setConfirming(on ? g.id : '')}
          run={run}
        />
      ))}

      <fieldset class="device-form">
        <legend>Add a group</legend>
        <label class="field">
          <span>Name</span>
          <input value={name} maxLength={60} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
        </label>
        <div class="chips">
          <button
            type="button"
            disabled={name.trim() === ''}
            onClick={() => {
              void run(api.addGroup(name.trim()).then(() => setName('')));
            }}
          >
            Add group
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

function GroupBlock({
  group, meters, confirming, onConfirm, run,
}: {
  group: ApplianceGroup;
  meters: Meter[];
  confirming: boolean;
  onConfirm: (on: boolean) => void;
  run: (work: Promise<unknown>) => void;
}) {
  const [name, setName] = useState(group.name);
  // An ordinary plug can be chosen if it is free or already in this group; a computer's plug is shown with its computer.
  const choices = meters.filter((c) => c.scope === 'plug' && !c.computer_id && (!c.group_id || c.group_id === group.id));

  return (
    <div class="computer-block">
      <div class="row">
        <label class="grow">
          <span class="visually-hidden">Name of the group</span>
          <input
            value={name}
            maxLength={60}
            onInput={(e) => setName((e.target as HTMLInputElement).value)}
            onBlur={() => name.trim() && name !== group.name && run(api.renameGroup(group.id, name))}
            onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
          />
        </label>
        {confirming ? (
          <>
            <button type="button" onClick={() => run(api.removeGroup(group.id))}>
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
      <p class="caption">Plugs in this group</p>
      {choices.length === 0 && <p class="caption">No plugs to choose from. Add a plug under Devices first.</p>}
      <div class="chips">
        {choices.map((c) => {
          const inGroup = c.group_id === group.id;
          return (
            <button
              key={c.id}
              type="button"
              aria-pressed={inGroup}
              onClick={() => run(api.updateMeter(c.id, { group_id: inGroup ? null : group.id }))}
            >
              {c.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}
