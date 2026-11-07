// What each appliance draws when it is doing nothing, the biggest first, with a year of it in money and how it has moved against the stretch before. Pick a day
// to see what unplugging something did. Each figure is that meter's own floor: never added to the others or to the house's.
import { useState } from 'preact/hooks';
import { api } from './api';
import { usePoll } from './hooks';
import { formatKwh, formatMoney } from './lib';
import { routePath } from './route';
import { changeText, changeYearText, floorText, houseLine, STANDBY_DAYS, stretchLabel } from './standby';

export function StandbyCard() {
  const [days, setDays] = useState<number>(30);
  const [change, setChange] = useState('');
  const { data, error } = usePoll(() => api.standby(days, change || undefined), 5 * 60_000, [days, change]);
  const house = data ? houseLine(data) : null;

  return (
    <section class="card standby">
      <div class="row">
        <h2>Always on</h2>
        <label class="field inline">
          <span class="visually-hidden">Compare</span>
          <select value={days} onChange={(e) => setDays(Number((e.target as HTMLSelectElement).value))}>
            {STANDBY_DAYS.map((d) => (
              <option key={d} value={d}>{`${d} days`}</option>
            ))}
          </select>
        </label>
      </div>
      {error && !data && <p class="note">{error.message}</p>}
      {!data && !error && <p class="note">Loading…</p>}
      {data && (
        <>
          {house && <p class="note">{house}</p>}
          {data.parts.length === 0 ? (
            <p class="note">Nothing rests above 1 W yet, or too little has been recorded.</p>
          ) : (
            <div class="ev-table-wrap">
              <table class="ev-table standby-table">
                <caption class="visually-hidden">Each appliance's always-on power, biggest first</caption>
                <thead>
                  <tr>
                    <th scope="col">Appliance</th>
                    <th scope="col">Never below</th>
                    <th scope="col">Before</th>
                    <th scope="col">Change</th>
                    <th scope="col">A year of it</th>
                  </tr>
                </thead>
                <tbody>
                  {data.parts.map((p) => (
                    <tr key={p.meter_id}>
                      <td>
                        <a href={routePath('hierarchy', 'meter', p.meter_id)}>{p.label}</a>
                      </td>
                      <td>{floorText(p.watts)}</td>
                      <td>{p.prev_watts != null ? floorText(p.prev_watts) : '—'}</td>
                      <td title={changeYearText(p)}>{changeText(p) || '—'}</td>
                      <td>{p.cost_per_year != null ? formatMoney(p.cost_per_year, data.rate_estimated) : formatKwh(p.kwh_per_year)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <p class="caption">{`${stretchLabel(data.from, data.to)} against ${stretchLabel(data.prev_from, data.prev_to)}.`}</p>
        </>
      )}
      <label class="field">
        <span>Compare around a day (optional)</span>
        <input type="date" value={change} max={new Date().toISOString().slice(0, 10)} onInput={(e) => setChange((e.target as HTMLInputElement).value)} />
      </label>
    </section>
  );
}
