// Groups, in the Hierarchy tab: each group is one solid line with the total of its members over the period, in the same columns as the tree. It is not a place in
// the wiring: the members stay where they are in the tree. A member that another member already contains (a plug on a circuit that is in the same group) is not
// added a second time. Groups are made, named and given their meters in the Connectors tab; the arrow opens the group's page.
import { usePoll } from './hooks';
import { api } from './api';
import { Kwh, Watts } from './Watts';
import { formatMoney, type ApplianceGroup, type GroupDetail, type Meter } from './lib';
import { routePath } from './route';

interface Loaded {
  groups: ApplianceGroup[];
  details: Map<string, GroupDetail>;
}

export function GroupsCard({
  meters, watts, from, to,
}: {
  meters: Meter[];
  /** What each meter draws right now, by meter id. */
  watts: Map<string, number>;
  /** The period the Energy and Cost columns follow (the one picked at the top of the tab). */
  from?: string;
  to?: string;
}) {
  const { data, error } = usePoll<Loaded>(
    async () => {
      const groups = await api.groups();
      const details = new Map<string, GroupDetail>();
      if (from && to) await Promise.all(groups.map(async (g) => details.set(g.id, await api.groupDetail(g.id, from, to))));
      return { groups, details };
    },
    30_000,
    [meters, from, to],
  );

  return (
    <section class="card wide">
      <h2>Groups</h2>
      {error && <p class="note">{error.message}</p>}
      {data && data.groups.length === 0 && (
        <p class="note">
          No groups yet. Make one in the <a href={routePath('connectors')}>Connectors tab</a>.
        </p>
      )}
      {data && data.groups.length > 0 && (
        <>
          <div class="hier-head" aria-hidden="true">
            <span class="hier-tag" />
            <span class="num">Power</span>
            <span class="num">Amps</span>
            <span class="num">Volts</span>
            <span class="num">Energy</span>
            <span class="num">Cost</span>
          </div>
          <ul class="hier">
            {data.groups.map((g) => (
              <GroupRow key={g.id} group={g} detail={data.details.get(g.id)} meters={meters} watts={watts} />
            ))}
          </ul>
        </>
      )}
    </section>
  );
}

function GroupRow({ group, detail, meters, watts }: { group: ApplianceGroup; detail?: GroupDetail; meters: Meter[]; watts: Map<string, number> }) {
  const members = group.meter_ids.map((id) => meters.find((m) => m.id === id)).filter((m): m is Meter => !!m);
  const notAdded = new Set(detail?.not_added ?? []);
  const counted = members.filter((m) => m.enabled && !notAdded.has(m.id));
  const nowParts = counted.map((m) => watts.get(m.id)).filter((w): w is number => w !== undefined);
  const now = nowParts.length > 0 ? nowParts.reduce((t, w) => t + w, 0) : null;

  return (
    <li class="hier-row">
      <div class="hier-main">
        <span />
        <span />
        <span />
        <a class="grow" href={routePath('hierarchy', 'group', group.id)}>
          {group.name}
        </a>
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
        </div>
      </div>
    </li>
  );
}
