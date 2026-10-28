// "Unusual days": the days the house (or one meter) used far more than it usually does, with how much more, what that cost and which meters made it so.
// A day's cause is already part of its figure, never added to it. On the Live page it says so when there is nothing unusual; on a meter's page it is
// left out when there is nothing to say.
import { useState } from 'preact/hooks';
import { api } from './api';
import { causesLine, dayFigures, dayLabel, firstDays } from './anomalies';
import { usePoll } from './hooks';

const SHOWN = 5;
const DAYS = 30;

export function AnomalyCard({ meterId, quiet }: { meterId?: string; quiet?: boolean }) {
  const [all, setAll] = useState(false);
  const { data } = usePoll(() => api.anomalies(DAYS, meterId), 10 * 60_000, [meterId ?? '']);
  if (!data) return null;
  if (data.days.length === 0) {
    return quiet ? null : (
      <section class="card">
        <h2>Unusual days</h2>
        <p class="note">None in the last {DAYS} days.</p>
      </section>
    );
  }
  const { list } = firstDays(data.days, all ? data.days.length : SHOWN);
  return (
    <section class="card">
      <h2>Unusual days</h2>
      <ul class="anomalies">
        {list.map((d) => {
          const f = dayFigures(d);
          return (
            <li key={`${d.date}:${d.meter_id ?? 'house'}`}>
              <span class="when">{dayLabel(d.date)}</span>
              <strong>{d.label}</strong>
              <span class="figs">
                <span>{f.kwh}</span>
                <span class="caption">usual {f.usual}</span>
                <span class="extra">{f.extra}</span>
                {f.cost && <span class="cost">{f.cost}</span>}
              </span>
              {d.causes.length > 0 && <span class="caption cause">{causesLine(d.causes)}</span>}
            </li>
          );
        })}
      </ul>
      {data.days.length > SHOWN && (
        <button type="button" class="link" onClick={() => setAll(!all)}>
          {all ? 'Show fewer' : `Show all ${data.days.length}`}
        </button>
      )}
      <p class="caption">Against the same kind of day (weekday or weekend) over the previous 4 weeks</p>
    </section>
  );
}
