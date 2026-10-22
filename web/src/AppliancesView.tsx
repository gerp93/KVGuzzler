import { api } from './api';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import {
  formatKwh, formatMoney, periodRange, splitSummary,
  type Channel, type ChannelSummary, type PeriodKey, type Summary,
} from './lib';

export function AppliancesView({ channels }: { channels: Channel[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const { data, error } = usePoll<Summary>(
    () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      return api.summary(from, to);
    },
    60_000,
    [period],
  );
  const label = (id: string) => channels.find((c) => c.id === id)?.label ?? id;

  const split = data ? splitSummary(data) : undefined;
  const top = Math.max(0, ...(split?.circuits.map((c) => c.kwh) ?? []), split?.unmeteredKwh ?? 0);

  const Item = ({ c }: { c: ChannelSummary }) => (
    <li>
      <span class="name">{label(c.channel_id)}</span>
      <span class="bar" aria-hidden="true">
        <i style={{ width: `${top > 0 ? (c.kwh / top) * 100 : 0}%` }} />
      </span>
      <span class="num">{formatKwh(c.kwh)}</span>
      {c.cost !== null && c.cost !== undefined && (
        <span class="num cost">{formatMoney(c.cost, data?.cost_estimated)}</span>
      )}
    </li>
  );

  return (
    <>
      <section class="card hero">
        <div class="row">
          <h2>House energy</h2>
          <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
        </div>
        <p class="big">{formatKwh(split?.houseKwh)}</p>
        {data?.house_cost && (
          <p class="caption">
            {formatMoney(data.house_cost, data.cost_estimated)}
            {data.cost_estimated && ' (some days are priced at the latest bill, until the next one is entered)'}
          </p>
        )}
      </section>

      {error && (
        <section class="card warn">
          <p>{error.message}</p>
        </section>
      )}

      {split && (
        <section class="card">
          <h2>Where it went</h2>
          <ul class="bars wide">
            {split.circuits.map((c) => (
              <Item key={c.channel_id} c={c} />
            ))}
          </ul>
          {split.unmeteredKwh !== null && (
            <p class="derived">
              <span>Everything else (no clamp on it)</span>
              <span class="num">{formatKwh(split.unmeteredKwh)}</span>
            </p>
          )}
        </section>
      )}

      {split && split.pc.length > 0 && (
        <section class="card">
          <h2>Also measured</h2>
          <ul class="bars wide">
            {split.pc.map((c) => (
              <Item key={c.channel_id} c={c} />
            ))}
          </ul>
          <p class="caption">The PC is part of one of the circuits above, so it is shown here and never added to them.</p>
        </section>
      )}
    </>
  );
}
