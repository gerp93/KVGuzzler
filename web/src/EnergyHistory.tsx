// A computer's energy over the rolling periods, one row each, and the daily CSV of the period being looked at.
import { api } from './api';
import { usePoll } from './hooks';
import { routePath } from './route';
import { formatKwh, formatMoney, historyDetail, periodRange, type PeriodKey } from './lib';

export function EnergyHistory({ id, period }: { id: string; period: PeriodKey }) {
  const { data, error } = usePoll(() => api.computerHistory(id), 60_000, [id]);
  const { from, to } = periodRange(period, new Date());
  const anyPrice = data?.periods.some((p) => p.cost != null) ?? false;

  return (
    <section class="card">
      <h2>Energy history</h2>
      {error && <p class="note">{error.message}</p>}
      {!data && !error && <p class="note">Loading…</p>}
      {data && (
        <ul class="settings">
          {data.periods.map((p) => (
            <li key={p.key}>
              <span class="grow">
                {p.label}
                <span class="caption block">{historyDetail(p)}</span>
              </span>
              <span class="num">{formatKwh(p.kwh)}</span>
              {p.cost != null && <span class="num cost">{formatMoney(p.cost, p.cost_estimated)}</span>}
            </li>
          ))}
        </ul>
      )}
      {data?.periods.some((p) => p.basis === 'sensors') && (
        <p class="caption">No wall plug is tied to this computer, so these figures are what its sensors measured. They leave out fans, drives and power-supply loss.</p>
      )}
      {data && !anyPrice && <p class="caption">Save your electricity rate (or a bill) in <a href={routePath('settings')}>Settings</a> to see costs.</p>}
      <p>
        <a class="button" href={api.computerExportUrl(id, from, to)} download>
          Export to CSV…
        </a>
        <span class="caption"> One row per day for the period picked above. The cost uses each day's price.</span>
      </p>
    </section>
  );
}
