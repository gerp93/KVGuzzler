import { api } from './api';
import { usePoll } from './hooks';
import { circuitsByWatts, formatWatts, pcSlices, unmetered, type Channel, type Live, type Row } from './lib';

function Bars({ rows, of }: { rows: Row[]; of: number }) {
  return (
    <ul class="bars">
      {rows.map((r) => (
        <li key={r.channel.id}>
          <span class="name">{r.channel.label}</span>
          <span class="bar" aria-hidden="true">
            <i style={{ width: `${of > 0 ? Math.min(100, (r.watts / of) * 100) : 0}%` }} />
          </span>
          <span class="num">{formatWatts(r.watts)}</span>
        </li>
      ))}
    </ul>
  );
}

export function LiveView({ channels }: { channels: Channel[] }) {
  const { data: live, error } = usePoll<Live>(api.live, 2000, []);
  if (!live) return <p class="note">{error ? `Waiting for the server: ${error.message}` : 'Loading…'}</p>;

  const circuits = circuitsByWatts(channels, live.readings);
  const house = live.house_watts ?? null;
  const rest = unmetered(house, circuits);
  // Bars are shares of the house when there is a house figure, else of the biggest load shown.
  const scale = house ?? Math.max(0, ...circuits.map((r) => r.watts));
  const pc = pcSlices(channels, live.readings);

  return (
    <>
      <section class="card hero">
        <h2>House right now</h2>
        <p class="big">{formatWatts(house)}</p>
        <p class="caption">The two mains clamps added together.</p>
      </section>

      {live.notes.length > 0 && (
        <section class="card warn" aria-live="polite">
          {live.notes.map((n) => (
            <p key={n}>{n}</p>
          ))}
        </section>
      )}

      <section class="card">
        <h2>Circuits</h2>
        {circuits.length === 0 ? <p class="note">No circuits are reporting.</p> : <Bars rows={circuits} of={scale} />}
        {rest !== null && (
          <p class="derived">
            <span>Everything else (no clamp on it)</span>
            <span class="num">{formatWatts(rest)}</span>
          </p>
        )}
      </section>

      {pc.length > 0 && (
        <section class="card">
          <h2>Also measured</h2>
          <Bars rows={pc} of={scale} />
          <p class="caption">
            The PC is plugged into one of the circuits above, so this is part of it, never added to the house or circuit
            figures.
          </p>
        </section>
      )}
    </>
  );
}
