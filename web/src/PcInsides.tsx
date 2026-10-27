// What the PC's own collector reported about its insides: its sensors, the programs it blames, and the times it was not
// recording. Shown inside a computer's detail; none of it is added to the house or circuit figures.
import {
  formatDuration, formatKwh, formatMoney, gapLabel, groupComponents, programRows, programSplit, runDuration, runKwh,
  type PcComponent, type PcSource,
} from './lib';
import { Kwh, Watts } from './Watts';

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
          <span class="num"><Kwh k={c.kwh} /></span>
          <span class="num cost">{c.avg_watts != null ? <>avg <Watts w={c.avg_watts} /></> : ''}</span>
        </li>
      ))}
    </ul>
  );
}

export function PcInsides({ pc, wholeKwh = null }: { pc: PcSource; wholeKwh?: number | null }) {
  const g = groupComponents(pc.components);
  const rows = programRows(pc.programs, wholeKwh);
  const split = programSplit(pc.programs, wholeKwh);
  const splitTotal = split.reduce((t, p) => t + p.kwh, 0);
  const programsKwh = pc.programs.reduce((t, p) => t + p.kwh, 0);

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
          {pc.total_peak_watts != null && <p class="caption">Highest the sensors reached together: <Watts w={pc.total_peak_watts} />.</p>}
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
          <h2>Who used the power (an estimate)</h2>
          <p class="note">
            CPU and GPU together: <Kwh k={programsKwh} />
            {pc.programs.every((p) => p.cost != null) ? `, ${formatMoney(pc.programs.reduce((t, p) => t + Number(p.cost), 0).toFixed(2), true)}` : ''}
          </p>
          {splitTotal > 0 && (
            <>
              <div class="split" role="img" aria-label="Where the power went">
                {split.map((p) => (
                  <i key={p.kind} class={`part ${p.kind}`} style={{ width: `${(p.kwh / splitTotal) * 100}%` }} title={`${p.label}: ${formatKwh(p.kwh)}`} />
                ))}
              </div>
              <ul class="legend">
                {split.map((p) => (
                  <li key={p.kind}>
                    <i class={`swatch ${p.kind}`} aria-hidden="true" /> {p.label} {Math.round((p.kwh / splitTotal) * 100)}%
                  </li>
                ))}
              </ul>
            </>
          )}
          <p class="caption">{wholeKwh != null && wholeKwh > 0 ? 'Percentages in the list are of the whole PC at the wall.' : 'Percentages in the list are of the energy the CPU and GPU used.'}</p>
          <ul class="bars wide">
            {rows.map((r) => (
              <li key={r.name}>
                <span class="name">
                  {r.name}
                  <span class="caption block">{r.note}</span>
                </span>
                <span class={`bar ${r.kind}`} aria-hidden="true">
                  <i style={{ width: `${r.share * 100}%` }} />
                </span>
                <span class="num"><Kwh k={r.kwh} /></span>
                <span class="num cost">{r.cost != null ? formatMoney(r.cost, true) : ''}</span>
              </li>
            ))}
          </ul>
          <details>
            <summary>How this is worked out</summary>
            <p class="note">
              The CPU and GPU only report their total power; Windows reports how much of the CPU and GPU each program used. The power above
              idle is shared out in proportion to that. The idle power itself belongs to no program. Every program is tracked automatically,
              grouped by executable name. It is an estimate, not a measurement, and it splits energy already counted above.
            </p>
          </details>
        </section>
      )}

      {(pc.runs ?? []).length > 0 && (
        <section class="card">
          <h2>Recent runs (an estimate)</h2>
          <ul class="settings">
            {(pc.runs ?? []).map((r) => (
              <li key={`${r.name}-${r.start}`}>
                <span class="grow">
                  {r.name}
                  <span class="caption">
                    {' '}
                    {r.still_running ? 'running now, since ' : ''}
                    {new Date(r.start).toLocaleString()}, {formatDuration(runDuration(r))}
                  </span>
                </span>
                <span class="num"><Kwh k={runKwh(r)} /></span>
                {r.cost != null && <span class="num cost">{formatMoney(r.cost, true)}</span>}
              </li>
            ))}
          </ul>
          <p class="caption">
            What a program is estimated to have caused from when it started until its last process closed. A program with many
            processes is one run.
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
                {gap.plug_kwh != null && <span class="caption">plug's own counter: <Kwh k={gap.plug_kwh} /></span>}
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  );
}
