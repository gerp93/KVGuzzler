import { api } from './api';
import { Chart, lineFrom, type Line } from './Chart';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import { formatKwh, formatMoney, formatWatts, periodRange, type Channel, type ChannelSummary, type PeriodKey, type Summary } from './lib';
import { routePath } from './route';

interface Loaded {
  summary: Summary;
  mine: ChannelSummary | undefined;
  line: Line | undefined;
  domain: [number, number];
}

/** One ordinary appliance (a circuit or a plug): how much it used, what that cost, and when. */
export function ApplianceView({ id, channels }: { id: string; channels: Channel[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const channel = channels.find((c) => c.id === id);

  const { data, error } = usePoll<Loaded>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const [summary, series] = await Promise.all([api.summary(from, to), api.series(id, from, to)]);
      return {
        summary,
        mine: summary.channels.find((c) => c.channel_id === id),
        line: series.points.length > 0 ? lineFrom(channel?.label ?? id, '--color-text', series.points) : undefined,
        domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] as [number, number],
      };
    },
    60_000,
    [id, period],
  );

  const mine = data?.mine;
  const houseKwh = data?.summary.house_kwh ?? null;
  const share = mine && houseKwh && houseKwh > 0 ? (mine.kwh / houseKwh) * 100 : null;

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
      {!channel && <p class="note">There is no such appliance.</p>}
      {channel && (
        <section class="card hero">
          <div class="row">
            <h2>{channel.label}</h2>
            <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
          </div>
          <p class="big">{formatKwh(mine?.kwh)}</p>
          <p class="caption">
            {mine?.cost != null && `${formatMoney(mine.cost, data?.summary.cost_estimated)}. `}
            {share != null && `${share.toFixed(1)}% of the house. `}
            {mine?.peak_watts != null && `Highest draw ${formatWatts(mine.peak_watts)}.`}
          </p>
        </section>
      )}
      {data?.line && (
        <section class="card">
          <h2>Power over time</h2>
          <Chart lines={[data.line]} domain={data.domain} />
        </section>
      )}
      {data && !data.line && channel && <p class="note">Nothing recorded for this appliance in this period.</p>}
    </>
  );
}
