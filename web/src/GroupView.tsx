import { api } from './api';
import { Skeleton } from './Skeleton';
import { Chart, lineFrom, type Line } from './Chart';
import { usePoll, useStored } from './hooks';
import { Kpi, KpiRow } from './Kpi';
import { PeriodPicker } from './Periods';
import { formatKwh, formatMoney, periodRange, periodWords, sumByTime, type Meter, type GroupDetail, type PeriodKey } from './lib';
import { routePath } from './route';

const COLORS = ['--color-accent-blue', '--color-accent-green', '--color-accent-red', '--color-accent-muted'];

interface Loaded {
  detail: GroupDetail;
  lines: Line[];
  domain: [number, number];
}

/** A logical group of meters (an entertainment center on two power strips, a desk and its computer): the total, and each meter. A member that another
 * member already contains is listed but not added again. */
export function GroupView({ id, meters }: { id: string; meters: Meter[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const label = (meterId: string) => meters.find((c) => c.id === meterId)?.label ?? meterId;

  const { data, error } = usePoll<Loaded>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const detail = await api.groupDetail(id, from, to);
      const series = await Promise.all(detail.members.map((m) => api.series(m.meter_id, from, to)));
      // The total line adds only the members that no other member contains; each member is drawn on its own too.
      const lines: Line[] = [];
      const counted = series.filter((s) => !detail.not_added.includes(s.meter_id));
      if (counted.length > 1) lines.push(lineFrom('Total', '--color-text', sumByTime(counted)));
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
      <a class="back" href={routePath('hierarchy')}>
        ← Hierarchy
      </a>
      {error && (
        <section class="card warn">
          <p>{error.message}</p>
        </section>
      )}
      {!data && !error && <Skeleton />}

      {detail && (
        <>
          <header class="page-head">
            <div>
              <h2 class="page-title">{detail.group.name}</h2>
              <p class="page-sub">
                {detail.members.length - detail.not_added.length} meter{detail.members.length - detail.not_added.length === 1 ? '' : 's'} added together
              </p>
            </div>
            <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
          </header>
          <KpiRow>
            <Kpi tone="energy" label="Energy" value={formatKwh(detail.total_kwh)} hint={periodWords(period as PeriodKey)} />
            <Kpi tone="cost" label="Cost" value={detail.total_cost != null ? formatMoney(detail.total_cost, detail.cost_estimated) : '—'} hint={periodWords(period as PeriodKey)} />
          </KpiRow>
          {detail.not_added.length > 0 && (
            <div class="kpi-notes">
              <p class="caption">
                {detail.not_added.length} more {detail.not_added.length === 1 ? 'is' : 'are'} inside another member, so not added again.
              </p>
            </div>
          )}

          <section class="card">
            <h2>By meter</h2>
            {detail.members.length === 0 && <p class="note">No meters in this group are reporting. Put meters in it in the Hierarchy tab.</p>}
            <ul class="bars wide">
              {detail.members.map((m) => (
                <li key={m.meter_id}>
                  <span class="name" title={detail.not_added.includes(m.meter_id) ? 'Inside another member, so not added to the total' : undefined}>
                    {label(m.meter_id)}
                    {detail.not_added.includes(m.meter_id) ? ' (inside another)' : ''}
                  </span>
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
            <section class="card wide">
              <h2>Power over time</h2>
              <Chart lines={data.lines} domain={data.domain} />
            </section>
          )}
        </>
      )}
    </>
  );
}
