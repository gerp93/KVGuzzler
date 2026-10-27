// The top guzzlers of the period picked on the Live page: the appliances that used the most energy, or cost the most. Only meters that hold nothing are
// ranked (see guzzlers.ts), so no energy is listed twice and nothing is added to anything.
import { useState } from 'preact/hooks';
import { meterPath } from './Bars';
import { topGuzzlers, type GuzzlerBy } from './guzzlers';
import { useStored } from './hooks';
import { formatKwh, formatMoney, periodWords, type Meter, type PeriodKey, type Summary } from './lib';

const SHOWN = 5;

export function GuzzlersCard({ meters, summary, houseKwh, period }: { meters: Meter[]; summary: Summary | undefined; houseKwh: number | null | undefined; period: PeriodKey }) {
  const [saved, setBy] = useStored('guzzlers-by', 'kwh');
  const [all, setAll] = useState(false);
  const priced = (summary?.meters ?? []).some((f) => f.cost != null && f.kwh > 0);
  const by: GuzzlerBy = saved === 'cost' && priced ? 'cost' : 'kwh';
  const list = topGuzzlers(meters, summary, by, houseKwh);
  if (list.length === 0) return null;
  const shown = all ? list : list.slice(0, SHOWN);
  // The bars are against the biggest, so the order reads at a glance: by energy, or by cost when that is the order.
  const value = (g: { kwh: number; cost: string | null }): number => (by === 'cost' ? Number(g.cost ?? 0) : g.kwh);
  const top = Math.max(...shown.map(value), 0);
  return (
    <section class="card guzzlers">
      <div class="row">
        <h2>Top guzzlers</h2>
        {priced && (
          <div class="seg" role="group" aria-label="Rank by">
            <button type="button" aria-pressed={by === 'kwh'} onClick={() => setBy('kwh')}>
              Energy
            </button>
            <button type="button" aria-pressed={by === 'cost'} onClick={() => setBy('cost')}>
              Cost
            </button>
          </div>
        )}
      </div>
      <p class="caption">{`What used the most over ${periodWords(period)}, appliance by appliance.`}</p>
      <ol class="guzzler-list" aria-label={`The biggest users by ${by === 'cost' ? 'cost' : 'energy'}, biggest first`}>
        {shown.map((g, i) => (
          <li key={g.meter.id}>
            <span class="rank" aria-hidden="true">
              {i + 1}
            </span>
            <a class="name" href={meterPath(g.meter)}>
              {g.meter.label}
            </a>
            <span class="bar" aria-hidden="true">
              <i style={{ width: `${top > 0 ? Math.max(1, (value(g) / top) * 100) : 0}%` }} />
            </span>
            <span class="num">{formatKwh(g.kwh)}</span>
            <span class="num cost">
              {g.cost != null ? formatMoney(g.cost, summary?.cost_estimated) : '—'}
              {g.share != null ? ` · ${g.share}%` : ''}
            </span>
          </li>
        ))}
      </ol>
      {list.length > SHOWN && (
        <button type="button" class="link" onClick={() => setAll(!all)}>
          {all ? 'Show fewer' : `Show all ${list.length}`}
        </button>
      )}
      <p class="caption">Only appliances are ranked, not the feeds and panels that hold them, so nothing is counted twice. The percent is of the house's energy.</p>
    </section>
  );
}
