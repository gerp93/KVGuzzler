// What the PC's own collector reported about its insides: its sensors, the programs it blames, and the times it was not
// recording. Shown inside a computer's detail; none of it is added to the house or circuit figures.
import {
  formatDuration, formatKwh, formatWatts, gapLabel, groupComponents,
  type PcComponent, type PcSource,
} from './lib';

function ComponentBars({ rows }: { rows: PcComponent[] }) {
  const top = Math.max(0, ...rows.map((r) => r.kwh));
  return (
    <ul class="bars wide">
      {rows.map((c) => (
        <li key={`${c.scope}-${c.name}`}>
          <span class="name">{c.name}</span>
          <span class="bar" aria-hidden="true">
            <i style={{ width: `${top > 0 ? (c.kwh / top) * 100 : 0}%` }} />
          </span>
          <span class="num">{formatKwh(c.kwh)}</span>
          <span class="num cost">{c.avg_watts != null ? `avg ${formatWatts(c.avg_watts)}` : ''}</span>
        </li>
      ))}
    </ul>
  );
}

export function PcInsides({ pc }: { pc: PcSource }) {
  const g = groupComponents(pc.components);
  const topProgram = Math.max(0, ...pc.programs.map((p) => p.kwh));

  return (
    <>
      {g.sensors.length > 0 && (
        <section class="card">
          <h2>Sensors</h2>
          <ComponentBars rows={g.sensors} />
          {g.inside.length > 0 && (
            <>
              <h3>Inside those</h3>
              <ComponentBars rows={g.inside} />
              <p class="caption">Parts of a sensor above, shown for information and never added to it.</p>
            </>
          )}
          {pc.total_peak_watts != null && <p class="caption">Highest the sensors reached together: {formatWatts(pc.total_peak_watts)}.</p>}
        </section>
      )}

      {g.wall.length > 0 && (
        <section class="card">
          <h2>As the PC itself measured the wall</h2>
          <ComponentBars rows={g.wall} />
          <p class="caption">The PC's own reading of its plug, kept so the two can be compared.</p>
        </section>
      )}

      {g.meters.length > 0 && (
        <section class="card">
          <h2>Plugs the PC itself reads</h2>
          <ComponentBars rows={g.meters} />
          <p class="caption">Separate devices, not part of the PC's figures.</p>
        </section>
      )}

      {pc.programs.length > 0 && (
        <section class="card">
          <h2>Programs (an estimate)</h2>
          <ul class="bars wide">
            {pc.programs.slice(0, 15).map((p) => (
              <li key={p.name}>
                <span class="name">{p.name}</span>
                <span class="bar" aria-hidden="true">
                  <i style={{ width: `${topProgram > 0 ? (p.kwh / topProgram) * 100 : 0}%` }} />
                </span>
                <span class="num">{formatKwh(p.kwh)}</span>
                <span class="num cost">{formatDuration(p.active_seconds ?? 0)}</span>
              </li>
            ))}
          </ul>
          <p class="caption">
            Only the power above the PC's idle level is shared out, by how busy each program kept the CPU and GPU. It is an
            estimate, not a measurement, and it splits energy already counted above.
          </p>
        </section>
      )}

      {pc.gaps.length > 0 && (
        <section class="card">
          <h2>Times it was not recording</h2>
          <ul class="settings">
            {pc.gaps.map((gap) => (
              <li key={gap.from}>
                <span class="grow">
                  {gapLabel(gap.reason)}, {new Date(gap.from).toLocaleString()} for{' '}
                  {formatDuration((Date.parse(gap.to) - Date.parse(gap.from)) / 1000)}
                </span>
                {gap.plug_kwh != null && <span class="caption">plug's own counter: {formatKwh(gap.plug_kwh)}</span>}
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  );
}
