import { api } from './api';
import { Chart, lineFrom, type Line } from './Chart';
import { usePoll, useStored } from './hooks';
import { PcInsides } from './PcInsides';
import { PeriodPicker } from './Periods';
import { formatKwh, formatMoney, formatWatts, periodRange, type Channel, type ChannelSummary, type ComputerDetail, type PeriodKey } from './lib';
import { routePath } from './route';

const COLORS = ['--color-accent-blue', '--color-accent-green', '--color-accent-red', '--color-accent-muted'];

interface Loaded {
  detail: ComputerDetail;
  lines: Line[];
  domain: [number, number];
}

function ago(iso: string | null | undefined): string {
  if (!iso) return 'never';
  const seconds = Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 1000));
  return seconds < 120 ? 'just now' : seconds < 7200 ? `${Math.round(seconds / 60)} min ago` : `${Math.round(seconds / 3600)} h ago`;
}

/** One computer: the whole PC at its plug, the desk around it, and (when a collector reports for it) its insides. */
export function ComputerView({ id, channels }: { id: string; channels: Channel[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const label = (channelId: string) => channels.find((c) => c.id === channelId)?.label ?? channelId;

  const { data, error } = usePoll<Loaded>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const detail = await api.computerDetail(id, from, to);
      // Each plug is drawn on its own line; the lines are never added together.
      const plugs = [detail.wall, ...detail.peripherals].filter((p): p is ChannelSummary => !!p);
      const series = await Promise.all(plugs.map((p) => api.series(p.channel_id, from, to)));
      const lines = series.map((s, i) => lineFrom(label(s.channel_id), i === 0 && detail.wall ? '--color-text' : COLORS[i % COLORS.length]!, s.points));
      return { detail, lines, domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] as [number, number] };
    },
    60_000,
    [id, period, channels],
  );

  const detail = data?.detail;
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
              <h2>{detail.computer.name}</h2>
              <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
            </div>
            <p class="big">{detail.wall ? formatKwh(detail.wall.kwh) : '—'}</p>
            <p class="caption">
              {detail.wall
                ? `The whole PC at the wall${detail.wall.cost != null ? `, ${formatMoney(detail.wall.cost, detail.cost_estimated)}` : ''}${detail.wall.peak_watts != null ? `, highest draw ${formatWatts(detail.wall.peak_watts)}` : ''}.`
                : 'No plug is tied to this computer yet. Tie its wall plug in Settings.'}
            </p>
          </section>

          {detail.peripherals.length > 0 && detail.setup_kwh != null && (
            <section class="card">
              <h2>The whole desk</h2>
              <p class="big small">{formatKwh(detail.setup_kwh)}</p>
              <p class="caption">
                The PC plus its peripherals{detail.setup_cost != null ? `, ${formatMoney(detail.setup_cost, detail.cost_estimated)}` : ''}. A separate
                figure: the PC's own number above does not include them.
              </p>
              <ul class="bars wide">
                {detail.peripherals.map((p) => (
                  <li key={p.channel_id}>
                    <span class="name">{label(p.channel_id)}</span>
                    <span class="bar" aria-hidden="true">
                      <i style={{ width: `${detail.setup_kwh! > 0 ? (p.kwh / detail.setup_kwh!) * 100 : 0}%` }} />
                    </span>
                    <span class="num">{formatKwh(p.kwh)}</span>
                    <span class="num cost">{p.cost != null ? formatMoney(p.cost, detail.cost_estimated) : ''}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}

          {data.lines.some((l) => l.x.length > 0) && (
            <section class="card">
              <h2>Power over time</h2>
              <Chart lines={data.lines} domain={data.domain} />
              <p class="caption">Each plug on its own line. They are not added together.</p>
            </section>
          )}

          {detail.pc ? (
            <>
              <p class="caption">
                Inside the PC, reported by its collector (last heard from {ago(detail.pc.last_seen)}).
              </p>
              <PcInsides pc={detail.pc} />
            </>
          ) : (
            <section class="card">
              <p class="note">
                {detail.computer.source_id
                  ? 'The collector for this computer has not reported in this period.'
                  : 'No collector reports for this computer yet, so there are no sensors or programs to show. What its plugs measure is above.'}
              </p>
            </section>
          )}
        </>
      )}
    </>
  );
}
