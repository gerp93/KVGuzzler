import { api } from './api';
import { ApplianceView } from './ApplianceView';
import { ComputerView } from './ComputerView';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import {
  applianceList, formatKwh, formatMoney, periodRange,
  type Channel, type Computer, type PeriodKey, type Summary,
} from './lib';
import { routePath } from './route';

interface Loaded {
  summary: Summary;
  computers: Computer[];
}

/** The Appliances tab: a list, and one thing open at a time (an ordinary appliance, or a computer with its full breakdown). */
export function AppliancesView({ channels, open }: { channels: Channel[]; open: string[] }) {
  const [kind, id] = open;
  if (kind === 'computer' && id) return <ComputerView id={id} channels={channels} />;
  if (kind === 'channel' && id) return <ApplianceView id={id} channels={channels} />;
  return <AppliancesList channels={channels} />;
}

function AppliancesList({ channels }: { channels: Channel[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const { data, error } = usePoll<Loaded>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const [summary, computers] = await Promise.all([api.summary(from, to), api.computers()]);
      return { summary, computers };
    },
    60_000,
    [period, channels],
  );

  const list = data ? applianceList(data.summary, channels, data.computers) : undefined;
  const estimated = data?.summary.cost_estimated;
  const top = Math.max(0, ...(list?.appliances.map((a) => a.channel.kwh) ?? []), data?.summary.unmetered_kwh ?? 0);
  const topComputer = Math.max(0, ...(list?.computers.map((c) => c.wall?.kwh ?? 0) ?? []));

  return (
    <>
      <section class="card hero">
        <div class="row">
          <h2>House energy</h2>
          <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
        </div>
        <p class="big">{formatKwh(data?.summary.house_kwh)}</p>
        {data?.summary.house_cost && (
          <p class="caption">
            {formatMoney(data.summary.house_cost, estimated)}
            {estimated && ' (some days are priced at the latest bill, until the next one is entered)'}
          </p>
        )}
      </section>

      {error && (
        <section class="card warn">
          <p>{error.message}</p>
        </section>
      )}

      {list && (
        <section class="card">
          <h2>Where it went</h2>
          {list.appliances.length === 0 && <p class="note">No appliances on a clamp or plug yet.</p>}
          <ul class="bars wide">
            {list.appliances.map((a) => (
              <li key={a.channel.channel_id}>
                <a class="rowlink" href={routePath('appliances', 'channel', a.channel.channel_id)}>
                  <span class="name">{a.label}</span>
                  <span class="bar" aria-hidden="true">
                    <i style={{ width: `${top > 0 ? (a.channel.kwh / top) * 100 : 0}%` }} />
                  </span>
                  <span class="num">{formatKwh(a.channel.kwh)}</span>
                  <span class="num cost">{a.channel.cost != null ? formatMoney(a.channel.cost, estimated) : ''}</span>
                </a>
              </li>
            ))}
          </ul>
          {data?.summary.unmetered_kwh != null && (
            <p class="derived">
              <span>Everything else (no clamp on it)</span>
              <span class="num">{formatKwh(data.summary.unmetered_kwh)}</span>
            </p>
          )}
        </section>
      )}

      {list && list.computers.length > 0 && (
        <section class="card">
          <h2>Computers</h2>
          <ul class="bars wide">
            {list.computers.map(({ computer, wall }) => (
              <li key={computer.id}>
                <a class="rowlink" href={routePath('appliances', 'computer', computer.id)}>
                  <span class="name">{computer.name}</span>
                  <span class="bar" aria-hidden="true">
                    <i style={{ width: `${topComputer > 0 ? ((wall?.kwh ?? 0) / topComputer) * 100 : 0}%` }} />
                  </span>
                  <span class="num">{wall ? formatKwh(wall.kwh) : 'no plug'}</span>
                  <span class="num cost">{wall?.cost != null ? formatMoney(wall.cost, estimated) : ''}</span>
                </a>
              </li>
            ))}
          </ul>
          <p class="caption">
            A computer is on one of the circuits, so these are shown on their own and never added to the house or circuit figures.
            Open one to see its sensors, programs and peripherals.
          </p>
        </section>
      )}
    </>
  );
}
