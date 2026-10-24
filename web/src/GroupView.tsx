import { api } from './api';
import { Chart, lineFrom, type Line } from './Chart';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import { formatKwh, formatMoney, periodRange, sumByTime, type Meter, type GroupDetail, type PeriodKey } from './lib';
import { routePath } from './route';

const COLORS = ['--color-accent-blue', '--color-accent-green', '--color-accent-red', '--color-accent-muted'];

interface Loaded {
  detail: GroupDetail;
  lines: Line[];
  domain: [number, number];
}

/** A group of plugs shown as one appliance (an entertainment center on two power strips): the total, and each plug. */
export function GroupView({ id, meters }: { id: string; meters: Meter[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const label = (meterId: string) => meters.find((c) => c.id === meterId)?.label ?? meterId;

  const { data, error } = usePoll<Loaded>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const detail = await api.groupDetail(id, from, to);
      const series = await Promise.all(detail.members.map((m) => api.series(m.meter_id, from, to)));
      // The plugs are all ordinary plugs, so their power can be added into one line; each plug is drawn on its own too.
      const lines: Line[] = [];
      if (series.length > 1) lines.push(lineFrom('Total', '--color-text', sumByTime(series)));
      series.forEach((s, i) => lines.push(lineFrom(label(s.meter_id), COLORS[i % COLORS.length]!, s.points)));
      return { detail, lines, domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] as [number, number] };
    },
    60_000,
    [id, period, meters],
  );

  const detail = data?.detail;
  const top = Math.max(0, ...(detail?.members.map((m) => m.kwh) ?? []));
  return (
    <>
      <a class="back" href={routePath('appliances')}>
        ← All appliances
      </a>
      {error && (
        <section class="card warn">
          <p>{error.message}</p>
        </section>
      )}
      {!data && !error && <p class="note">Loading…</p>}

      {detail && (
        <>
          <section class="card hero">
            <div class="row">
              <h2>{detail.group.name}</h2>
              <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
            </div>
            <p class="big">{formatKwh(detail.total_kwh)}</p>
            <p class="caption">
              {detail.members.length} plug{detail.members.length === 1 ? '' : 's'} added together
              {detail.total_cost != null ? `, ${formatMoney(detail.total_cost, detail.cost_estimated)}` : ''}.
            </p>
          </section>

          <section class="card">
            <h2>By plug</h2>
            {detail.members.length === 0 && <p class="note">No plugs in this group are reporting. Put plugs in it under Settings.</p>}
            <ul class="bars wide">
              {detail.members.map((m) => (
                <li key={m.meter_id}>
                  <span class="name">{label(m.meter_id)}</span>
                  <span class="bar" aria-hidden="true">
                    <i style={{ width: `${top > 0 ? (m.kwh / top) * 100 : 0}%` }} />
                  </span>
                  <span class="num">{formatKwh(m.kwh)}</span>
                  <span class="num cost">{m.cost != null ? formatMoney(m.cost, detail.cost_estimated) : ''}</span>
                </li>
              ))}
            </ul>
          </section>

          {data.lines.some((l) => l.x.length > 0) && (
            <section class="card">
              <h2>Power over time</h2>
              <Chart lines={data.lines} domain={data.domain} />
            </section>
          )}
        </>
      )}
    </>
  );
}
