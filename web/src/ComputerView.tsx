import { api } from './api';
import { Chart, lineFrom, type Line, type Shade } from './Chart';
import { usePoll, useStored } from './hooks';
import { EnergyHistory } from './EnergyHistory';
import { PcInsides } from './PcInsides';
import { PcLiveCard } from './PcLiveCard';
import { PeriodPicker } from './Periods';
import { formatKwh, formatMoney, formatWatts, gapShades, isLiveStale, liveSplit, pcLineStyle, periodRange, type Channel, type ChannelSummary, type ComputerDetail, type PeriodKey } from './lib';
import { routePath } from './route';

const COLORS = ['--color-accent-blue', '--color-accent-green', '--color-accent-red', '--color-accent-muted'];

interface Loaded {
  detail: ComputerDetail;
  lines: Line[];
  /** What each part of the PC drew, from its collector; empty without one. */
  inside: Line[];
  shades: Shade[];
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
      const [series, pcSeries] = await Promise.all([
        Promise.all(plugs.map((p) => api.series(p.channel_id, from, to))),
        detail.computer.source_id ? api.computerSeries(id, from, to) : Promise.resolve(undefined),
      ]);
      const lines = series.map((s, i) => lineFrom(label(s.channel_id), i === 0 && detail.wall ? '--color-text' : COLORS[i % COLORS.length]!, s.points));
      const perScope = new Map<string, number>();
      const inside = (pcSeries?.lines ?? []).map((l) => {
        const index = perScope.get(l.scope) ?? 0;
        perScope.set(l.scope, index + 1);
        const style = pcLineStyle(l, index);
        return { ...lineFrom(l.name, style.colorVar, l.points), dashed: style.dashed };
      });
      return { detail, lines, inside, shades: gapShades(pcSeries?.gaps ?? []), domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] as [number, number] };
    },
    60_000,
    [id, period, channels],
  );

  // The look is small and changes every few seconds, so it has its own quick poll; the period data above is slow.
  const { data: look } = usePoll(() => api.computerLive(id), 5_000, [id]);

  const detail = data?.detail;
  const liveNow = look ?? detail?.pc?.live ?? undefined;
  const nowWatts = liveNow ? (isLiveStale(liveNow) ? null : liveSplit(liveNow).baseWatts) : null;
  const peak = detail?.wall?.peak_watts ?? detail?.pc?.total_peak_watts ?? null;
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
            <div class="stats">
              <div class="stat">
                <span class="caption">Energy</span>
                <p class="big small">{detail.wall ? formatKwh(detail.wall.kwh) : '—'}</p>
              </div>
              <div class="stat">
                <span class="caption">Cost</span>
                <p class="big small">{detail.wall?.cost != null ? formatMoney(detail.wall.cost, detail.cost_estimated) : '—'}</p>
              </div>
              <div class="stat">
                <span class="caption">Right now</span>
                <p class="big small">{nowWatts != null ? formatWatts(nowWatts) : '—'}</p>
              </div>
              <div class="stat">
                <span class="caption">Max draw</span>
                <p class="big small">{peak != null ? formatWatts(peak) : '—'}</p>
              </div>
            </div>
            <p class="caption">
              {detail.wall
                ? 'The whole PC at the wall.'
                : 'No plug is tied to this computer yet. Tie its wall plug in Settings.'}
              {peak != null && !detail.wall?.peak_watts ? " Max draw is the highest the sensors reached together; the wall is not measured." : ''}
            </p>
          </section>

          {liveNow && <PcLiveCard live={liveNow} />}

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

          <EnergyHistory id={id} period={period as PeriodKey} />

          {data.inside.some((l) => l.x.length > 0) && (
            <section class="card">
              <h2>Inside the PC over time</h2>
              <Chart lines={data.inside} domain={data.domain} shades={data.shades} />
              <p class="caption">
                What the PC's own collector recorded: the wall as it read it, each sensor, and the plugs it reads (dashed). The lines are not added
                together. Shaded stretches are times nothing was being recorded.
              </p>
            </section>
          )}

          {detail.pc ? (
            <>
              <p class="caption">
                Inside the PC, reported by its collector (last heard from {ago(detail.pc.last_seen)}).
              </p>
              <PcInsides pc={detail.pc} wholeKwh={detail.wall?.kwh ?? null} />
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
