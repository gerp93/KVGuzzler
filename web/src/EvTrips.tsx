// The stretches between odometer readings, newest first: the miles driven over the energy the meter used in the same days, and what the energy cost. The
// newest row is what has been used since the latest reading, which has no miles yet. The sums are the server's.
import { useState } from 'preact/hooks';
import { dayLabel, formatDistance, formatEfficiency, lowCoverage, stretchLabel, type DistanceUnit, type MeterEv } from './ev';
import { formatMoney, formatPerMile } from './lib';
import { Kwh } from './Watts';

/** How many stretches are listed before "Show all": one a day adds up. */
const SHOWN = 5;

export function EvTrips({ ev, unit }: { ev: MeterEv; unit: DistanceUnit }) {
  const [showAll, setShowAll] = useState(false);
  const trips = [...ev.intervals].reverse();
  const since = ev.since_last;
  if (trips.length === 0 && !since) return null;
  const distanceWord = unit === 'km' ? 'kilometer' : 'mile';

  return (
    <section class="ev-section">
      <div class="ev-section-head">
        <h3 class="ev-section-title">Between odometer readings</h3>
        {trips.length > 0 && <span class="ev-section-sub">{trips.length === 1 ? '1 stretch' : `${trips.length} stretches`}</span>}
      </div>
      <div class="ev-table-wrap">
        <table class="ev-table">
          <caption class="visually-hidden">Between odometer readings, newest first</caption>
          <thead>
            <tr>
              <th scope="col">Between</th>
              <th scope="col">Distance</th>
              <th scope="col">Energy</th>
              <th scope="col">Cost</th>
              <th scope="col">Per {distanceWord}</th>
              <th scope="col">Efficiency</th>
            </tr>
          </thead>
          <tbody>
            {since && (
              <tr class="so-far">
                <td>Since {dayLabel(since.from_day)}</td>
                <td>so far</td>
                <td>
                  <Kwh k={since.kwh} />
                </td>
                <td>{since.cost != null ? formatMoney(since.cost, ev.cost_estimated) : '—'}</td>
                <td>—</td>
                <td>—</td>
              </tr>
            )}
            {(showAll ? trips : trips.slice(0, SHOWN)).map((s) => (
              <tr key={s.from_day}>
                <td>
                  {stretchLabel(s)}
                  {lowCoverage(s.coverage) && (
                    <span class="badge" title={`Measured ${Math.round(s.coverage * 100)}% of these days: energy and cost are understated`}>
                      {Math.round(s.coverage * 100)}% measured
                    </span>
                  )}
                </td>
                <td>{formatDistance(s.miles, unit)}</td>
                <td>
                  <Kwh k={s.kwh} />
                </td>
                <td>{s.cost != null ? formatMoney(s.cost, s.cost_estimated) : '—'}</td>
                <td>
                  <strong>{formatPerMile(s.cost_per_mile, s.cost_estimated, unit)}</strong>
                </td>
                <td>{formatEfficiency(s.miles_per_kwh, unit)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {trips.length > SHOWN && (
        <button type="button" class="ev-more" onClick={() => setShowAll(!showAll)}>
          {showAll ? 'Show fewer' : `Show all ${trips.length}`}
        </button>
      )}
    </section>
  );
}
