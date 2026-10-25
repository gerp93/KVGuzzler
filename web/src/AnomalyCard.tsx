// "Unusual days": the days the house (or one meter) used far more than it usually does, with how much more, what that cost and which meters made it so.
// A day's cause is already part of its figure, never added to it. On the Live page it says so when there is nothing unusual; on a meter's page it is
// left out when there is nothing to say.
import { useState } from 'preact/hooks';
import { api } from './api';
import { causesLine, dayLabel, dayLine, firstDays } from './anomalies';
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
        <p class="note">Nothing unusual in the last {DAYS} days: every day was close to what that kind of day usually is.</p>
      </section>
    );
  }
  const { list } = firstDays(data.days, all ? data.days.length : SHOWN);
  return (
    <section class="card">
      <h2>Unusual days</h2>
      <ul class="anomalies">
        {list.map((d) => (
          <li key={`${d.date}:${d.meter_id ?? 'house'}`}>
            <span class="when">{dayLabel(d.date)}</span>
            <span class="what">
              <strong>{d.label}</strong>: {dayLine(d)}
              {d.causes.length > 0 && <span class="caption block">{causesLine(d.causes)}</span>}
            </span>
          </li>
        ))}
      </ul>
      {data.days.length > SHOWN && (
        <button type="button" class="link" onClick={() => setAll(!all)}>
          {all ? 'Show fewer' : `Show all ${data.days.length}`}
        </button>
      )}
      <p class="caption">
        A day is listed when it used at least half as much again as the same kind of day (weekday or weekend) over the four weeks before it, and well
        beyond how much that meter's days normally vary. A meter inside something that was unusual the same day is named as its cause, not listed again.
      </p>
    </section>
  );
}
