// What the PC's own collector reported about its insides: its sensors, the programs it blames, and the times it was not
// recording. Shown inside a computer's detail; none of it is added to the house or circuit figures.
import {
  formatDuration, formatKwh, formatMoney, gapLabel, groupComponents, moneyText, programRows, programSplit, runDuration, runKwh,
  type PcComponent, type PcSource,
} from './lib';
import { Facts } from './Facts';
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
            </>
          )}
          {pc.total_peak_watts != null && <Facts items={[{ label: 'Peak, sensors together', value: `${Math.round(pc.total_peak_watts)} W` }]} />}
        </section>
      )}

      {g.wall.length > 0 && (
        <section class="card">
          <h2>As the PC itself measured the wall</h2>
          <ComponentBars rows={g.wall} />
        </section>
      )}

      {g.meters.length > 0 && (
        <section class="card">
          <h2>Plugs the PC itself reads</h2>
          <ComponentBars rows={g.meters} />
        </section>
      )}

      {pc.programs.length > 0 && (
        <section class="card">
          <h2>Who used the power (an estimate)</h2>
          <Facts
            items={[
              { label: 'CPU + GPU', value: formatKwh(programsKwh) },
              ...(pc.programs.every((p) => p.cost != null) ? [{ label: 'Cost', value: formatMoney(moneyText(pc.programs.reduce((t, p) => t + Number(p.cost), 0)), true) }] : []),
            ]}
          />
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
            <summary>Method</summary>
            <p class="caption">
              Power above idle is shared out by each program's share of CPU and GPU busy time (grouped by executable). Idle belongs to no
              program. Shares are of {wholeKwh != null && wholeKwh > 0 ? 'the whole PC at the wall' : 'CPU and GPU energy'}.
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
