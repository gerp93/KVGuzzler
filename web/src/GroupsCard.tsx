// Groups: a logical grouping of any meters, shown as one line with the total of its members over the period. It is not a place in the wiring: the
// members stay where they are in the tree. A member that another member already contains (a plug on a circuit that is in the same group) is
// listed but not added a second time. A row opens (the arrow) to choose the members.
import { useState } from 'preact/hooks';
import { Hint } from './Hint';
import { api } from './api';
import { usePoll } from './hooks';
import { Kwh, Watts } from './Watts';
import { formatMoney, type ApplianceGroup, type GroupDetail, type Meter } from './lib';
import { routePath } from './route';

interface Loaded {
  groups: ApplianceGroup[];
  details: Map<string, GroupDetail>;
}

export function GroupsCard({
  meters, onChanged, watts, deviceName, from, to,
}: {
  meters: Meter[];
  onChanged: () => void;
  /** What each meter draws right now, by meter id. */
  watts: Map<string, number>;
  deviceName: (id: string) => string;
  /** The period the Energy and Cost columns follow (the one picked at the top of the tab). */
  from?: string;
  to?: string;
}) {
  const [rev, setRev] = useState(0);
  const { data, error } = usePoll<Loaded>(
    async () => {
      const groups = await api.groups();
      const details = new Map<string, GroupDetail>();
      if (from && to) await Promise.all(groups.map(async (g) => details.set(g.id, await api.groupDetail(g.id, from, to))));
      return { groups, details };
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
      <h2>Groups</h2>
      <Hint>
        A group is a logical grouping, not a place in the wiring: put any meters in one (the plugs of an entertainment center, a desk and its computer) and it shows
        their total as one line. Every meter stays where it is in the tree. If a meter is inside another in the same group (a plug on a circuit), it is listed but not
        added again, because the one that holds it already counts it. Add a group with + Add.
      </Hint>
      {error && <p class="note">{error.message}</p>}
      {data && data.groups.length === 0 && <p class="note">No groups yet.</p>}
      {data && data.groups.length > 0 && (
        <>
          <div class="hier-head" aria-hidden="true">
            <span class="hier-tag" />
            <span class="num">Now</span>
            <span class="num">Amps</span>
            <span class="num">Volts</span>
            <span class="num">Energy</span>
            <span class="num">Cost</span>
          </div>
          <ul class="hier">
            {data.groups.map((g) => (
              <GroupRow key={g.id} group={g} detail={data.details.get(g.id)} meters={meters} watts={watts} deviceName={deviceName} run={run} />
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

function GroupRow({
  group, detail, meters, watts, deviceName, run,
}: {
  group: ApplianceGroup;
  detail?: GroupDetail;
  meters: Meter[];
  watts: Map<string, number>;
  deviceName: (id: string) => string;
  run: (work: Promise<unknown>) => void;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(group.name);
  const [confirming, setConfirming] = useState(false);

  const members = group.meter_ids.map((id) => meters.find((m) => m.id === id)).filter((m): m is Meter => !!m);
  const notAdded = new Set(detail?.not_added ?? []);
  const counted = members.filter((m) => m.enabled && !notAdded.has(m.id));
  const nowParts = counted.map((m) => watts.get(m.id)).filter((w): w is number => w !== undefined);
  const now = nowParts.length > 0 ? nowParts.reduce((t, w) => t + w, 0) : null;

  // Any shown meter that is not in a group yet (the mains is the house itself; a breaker's second clamp is part of the first one's row).
  const secondLegs = new Set(meters.flatMap((m) => (m.pair_with ? [m.pair_with] : [])));
  const available = meters
    .filter((m) => m.enabled && m.scope !== 'mains' && !m.group_id && !secondLegs.has(m.id))
    .sort((a, b) => a.label.localeCompare(b.label));

  return (
    <li class="hier-row">
      <div class="hier-main">
        <button type="button" class="twisty" aria-expanded={open} aria-label={`Members of ${group.name}`} onClick={() => setOpen(!open)}>
          <span aria-hidden="true">▸</span>
        </button>
        <span />
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
        <span class="caption hier-tag">
          {members.length} {members.length === 1 ? 'meter' : 'meters'}
        </span>
        <span class="num hier-watts">{now !== null ? <Watts w={now} /> : ''}</span>
        <span class="hier-amps" />
        <span class="hier-volts" />
        <span class="num hier-energy">{detail ? <Kwh k={detail.total_kwh} /> : ''}</span>
        <span class="num hier-cost">{detail?.total_cost != null ? formatMoney(detail.total_cost, detail.cost_estimated) : detail ? '—' : ''}</span>
        <div class="hier-actions">
          <a class="button icon" href={routePath('hierarchy', 'group', group.id)} title="Open the group's page" aria-label={`Open ${group.name}`}>
            ›
          </a>
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
            <button type="button" onClick={() => setConfirming(true)} title="Remove the group (its meters stay)" aria-label={`Remove ${group.name}`}>
              ✕
            </button>
          )}
        </div>
      </div>

      {open && (
        <div class="hier-panel">
          <p class="caption block">Meters in this group</p>
          {members.length === 0 && <p class="caption block">None yet. Choose one below.</p>}
          <div class="chips">
            {members.map((m) => (
              <button
                key={m.id}
                type="button"
                title={notAdded.has(m.id) ? 'Inside another meter of this group, so not added again. Click to take it out.' : 'Click to take it out of the group.'}
                onClick={() => run(api.updateMeter(m.id, { group_id: null }))}
              >
                {m.label}
                {notAdded.has(m.id) ? ' (inside another)' : ''} ✕
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
                  {deviceName(m.device_id) ? ` · ${deviceName(m.device_id)}` : ''}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
    </li>
  );
}
