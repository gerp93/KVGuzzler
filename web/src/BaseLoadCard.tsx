// "Always on": the power the house (or one meter) hardly ever goes below, a year of it in energy and money, and its share of what is used.
// Not shown until an hour of minutes has been recorded. A view of one reading: never added to anything.
import { api } from './api';
import { lookedAt, shareText, yearKwh, type BaseLoad } from './baseload';
import { usePoll } from './hooks';
import { formatMoney, periodRange, type PeriodKey } from './lib';
import { Watts } from './Watts';

/** Under this a meter's floor is noise. */
const MIN_METER_WATTS = 1;

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
  // A meter that rests at nothing (a water heater, a charger with the car away) has no always-on power to speak of: no card, not a row of zeros.
  if (meterId && data.watts < MIN_METER_WATTS) return null;
  return (
    <section class="card">
      <h2>Always on</h2>
      <BaseLoadStats data={data} />
    </section>
  );
}

/** The figures of an always-on reading: shared by a meter's own card and the house's, which sits in the Top guzzlers card under "At rest". */
export function BaseLoadStats({ data }: { data: BaseLoad }) {
  return (
    <>
      <div class="stats three">
        <div class="stat">
          <span class="caption">Never below</span>
          <p class="big small"><Watts w={data.watts} /></p>
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
      <p class="caption">Based on {lookedAt(data.minutes)} of readings</p>
    </>
  );
}
