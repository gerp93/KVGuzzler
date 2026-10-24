// "Always on": the power the house (or one meter) hardly ever goes below, a year of it in energy and money, and its share of what is used.
// Not shown until an hour of minutes has been recorded. A view of one reading: never added to anything.
import { api } from './api';
import { baseLoadSentence, lookedAt, shareText, yearKwh } from './baseload';
import { usePoll } from './hooks';
import { formatMoney, formatWatts, periodRange, type PeriodKey } from './lib';
import { routePath } from './route';

export function BaseLoadCard({ meterId, period }: { meterId?: string; period: PeriodKey }) {
  const { data } = usePoll(
    async () => {
      const { from, to } = periodRange(period, new Date());
      return (await (meterId ? api.meterBaseLoad(meterId, from, to) : api.houseBaseLoad(from, to))) ?? null;
    },
    60_000,
    [meterId ?? '', period],
  );
  if (!data) return null;
  return (
    <section class="card">
      <h2>Always on</h2>
      <div class="stats three">
        <div class="stat">
          <span class="caption">Never below</span>
          <p class="big small">{formatWatts(data.watts)}</p>
        </div>
        <div class="stat">
          <span class="caption">A year of it</span>
          <p class="big small">{data.cost_per_year != null ? formatMoney(data.cost_per_year, data.rate_estimated) : yearKwh(data.kwh_per_year)}</p>
          {data.cost_per_year != null && <span class="caption">{yearKwh(data.kwh_per_year)}</span>}
        </div>
        <div class="stat">
          <span class="caption">Share of use</span>
          <p class="big small">{shareText(data.share)}</p>
        </div>
      </div>
      <p class="caption">
        {baseLoadSentence(data, 'this meter')} Worked out as the level it is above 95% of the time, from {lookedAt(data.minutes)} of readings in the period
        {data.cost_per_year != null ? ", priced at today's rate" : ''}.
      </p>
      {data.parts && data.parts.length > 0 && (
        <>
          <h3>What each meter rests at</h3>
          <ul class="bars wide parts">
            {data.parts.map((p) => (
              <li key={p.meter_id}>
                <span class="name" title={p.label}>
                  <a href={routePath('hierarchy', 'meter', p.meter_id)}>{p.label}</a>
                </span>
                <span class="bar" aria-hidden="true">
                  <i style={{ width: `${Math.min(100, (p.watts / data.parts![0]!.watts) * 100)}%` }} />
                </span>
                <span class="num">{formatWatts(p.watts)}</span>
                <span class="num cost">{p.cost_per_year != null ? `${formatMoney(p.cost_per_year)} a year` : yearKwh(p.kwh_per_year)}</span>
              </li>
            ))}
          </ul>
          <p class="caption">Each is that meter's own floor. They are not added to each other or to the house's: the quietest minutes need not be the same ones, and an appliance that cycles (a fridge) rests at its off level between cycles.</p>
        </>
      )}
    </section>
  );
}
