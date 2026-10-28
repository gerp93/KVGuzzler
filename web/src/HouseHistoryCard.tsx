// The house's energy over the rolling periods, one row each, and the daily CSV of the period being looked at.
import { api } from './api';
import { usePoll } from './hooks';
import { routePath } from './route';
import { formatCarbon, formatMoney, periodRange, type PeriodKey } from './lib';
import { Kwh, Watts } from './Watts';

export function HouseHistoryCard({ period }: { period: PeriodKey }) {
  const { data, error } = usePoll(() => api.houseHistory(), 60_000, []);
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
                <span class="caption block">
                  {p.peak_watts != null && (
                    <>
                      peak ≤ <Watts w={p.peak_watts} />
                    </>
                  )}
                  {p.peak_watts != null && p.carbon_g != null && ' · '}
                  {p.carbon_g != null && formatCarbon(p.carbon_g)}
                </span>
              </span>
              <span class="num"><Kwh k={p.kwh} /></span>
              {p.cost != null && <span class="num cost">{formatMoney(p.cost, p.cost_estimated)}</span>}
            </li>
          ))}
        </ul>
      )}
      {data && !anyPrice && <p class="caption">Set a price in <a href={routePath('settings')}>Settings</a> to see costs.</p>}
      <p>
        <a class="button" href={api.houseExportUrl(from, to)} download title="One row per day for the period picked: the house and each circuit, with cost">
          Export to CSV…
        </a>
      </p>
    </section>
  );
}
