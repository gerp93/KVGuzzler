// Where the server keeps its history, how big it has grown, and how fast it grows (read-only: the file lives on the box and is backed up or moved there).
// The growth is the server's estimate from the sizes it has noted: a month's worth, and the size a year from now at that rate.
import { useState } from 'preact/hooks';
import { api } from './api';
import { dailySizes } from './dbsize';
import { Facts } from './Facts';
import { usePoll } from './hooks';
import { formatBytes } from './lib';

/** How many days of sizes are listed before "Show all". */
const SHOWN = 7;

export function DatabaseCard() {
  const { data: status } = usePoll(api.status, 60_000, []);
  const { data: growth } = usePoll(api.databaseGrowth, 10 * 60_000, []);
  const [all, setAll] = useState(false);
  const days = growth ? dailySizes(growth.points) : [];
  const growing = growth?.per_month_bytes != null && growth.in_a_year_bytes != null;
  return (
    <section class="card page-narrow">
      <h2>Database</h2>
      {status?.database_path ? (
        <Facts
          items={[
            { label: 'Location', value: status.database_path },
            ...(status.database_bytes != null ? [{ label: 'Size', value: formatBytes(status.database_bytes) }] : []),
            ...(growing
              ? [
                  { label: 'Growth', value: `≈ ${formatBytes(growth.per_month_bytes!)} a month` },
                  { label: 'In a year', value: `≈ ${formatBytes(growth.in_a_year_bytes!)}` },
                ]
              : []),
          ]}
        />
      ) : (
        <p class="note">{status ? 'Location unknown.' : 'Reading…'}</p>
      )}
      {growth && !growing && <p class="note">Growth shows after 3 days of sizes{growth.readings > 0 ? ` (${growth.readings} so far)` : ''}.</p>}
      {days.length > 0 && (
        <div class="ev-table-wrap">
          <table class="ev-table">
            <caption class="visually-hidden">The database's size at the end of each day, newest first</caption>
            <thead>
              <tr>
                <th scope="col">Day</th>
                <th scope="col">Size</th>
                <th scope="col">Change</th>
              </tr>
            </thead>
            <tbody>
              {(all ? days : days.slice(0, SHOWN)).map((d) => (
                <tr key={d.day}>
                  <td>{new Date(`${d.day}T12:00:00`).toLocaleDateString()}</td>
                  <td>{formatBytes(d.bytes)}</td>
                  <td>{d.change == null ? '—' : `${d.change > 0 ? '+' : d.change < 0 ? '−' : ''}${formatBytes(Math.abs(d.change))}`}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {days.length > SHOWN && (
        <button type="button" class="ev-more" onClick={() => setAll(!all)}>
          {all ? 'Show fewer' : `Show all ${days.length}`}
        </button>
      )}
    </section>
  );
}
