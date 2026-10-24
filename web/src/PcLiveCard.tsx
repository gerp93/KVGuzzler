// The PC right now, as its collector last saw it: what each part is drawing, how much of the whole PC that is, and which programs are
// causing it. A look is not history: the page asks for a new one every few seconds and says how old the one it shows is.
import {
  formatAge, formatWatts, idleText, isLiveStale, liveSplit, programsNow, sharePercent,
  type PcLive,
} from './lib';

function Row({ name, watts, base, hint }: { name: string; watts: number; base: number; hint?: string }) {
  const share = sharePercent(watts, base);
  return (
    <li>
      <span class="name">{name}</span>
      <span class="bar" aria-hidden="true">
        <i style={{ width: `${base > 0 ? Math.min(100, (watts / base) * 100) : 0}%` }} />
      </span>
      <span class="num">{formatWatts(watts)}</span>
      <span class="num cost">{hint ?? (share != null ? `${share}%` : '')}</span>
    </li>
  );
}

export function PcLiveCard({ live }: { live: PcLive }) {
  const split = liveSplit(live);
  const stale = isLiveStale(live);
  const programs = programsNow(live);
  const topProgram = Math.max(0, ...programs.map((p) => p.watts));

  return (
    <>
      <section class={`card${stale ? ' warn' : ''}`}>
        <div class="row">
          <h2>Right now</h2>
          <span class="caption">{stale ? `Not answering: last heard ${formatAge(live.age_seconds ?? 0)}` : `Updated ${formatAge(live.age_seconds ?? 0)}`}</span>
        </div>
        {stale && <p class="note">The PC may be asleep, off, or its collector stopped. These are its last readings.</p>}
        <p class="big small">{formatWatts(split.baseWatts)}</p>
        <p class="caption">
          {split.baseIsWall
            ? 'The whole PC at the wall, as the PC itself reads its plug.'
            : 'The sensors added together. The wall is not measured here, so the rest of the PC (fans, drives, power-supply loss) is not in this figure.'}
        </p>

        {split.sensors.length > 0 && (
          <ul class="bars wide">
            {split.sensors.map((r) => (
              <Row key={r.name} name={r.name} watts={r.watts} base={split.baseWatts} />
            ))}
            {split.otherWatts != null && split.otherWatts > 0 && (
              <Row name="Other (not metered)" watts={split.otherWatts} base={split.baseWatts} />
            )}
          </ul>
        )}
        {split.otherWatts != null && split.otherWatts > 0 && (
          <p class="caption">Other (not metered) is the wall minus the sensors: fans, drives, motherboard and power-supply loss.</p>
        )}

        {split.inside.length > 0 && (
          <>
            <h3>Inside those</h3>
            <ul class="bars wide">
              {split.inside.map((r) => (
                <Row key={r.name} name={r.name} watts={r.watts} base={split.baseWatts} hint="part of a sensor" />
              ))}
            </ul>
          </>
        )}

        {split.meters.length > 0 && (
          <>
            <h3>Other plugs it reads</h3>
            <ul class="bars wide">
              {split.meters.map((r) => (
                <Row key={r.name} name={r.name} watts={r.watts} base={Math.max(...split.meters.map((m) => m.watts))} hint="separate device" />
              ))}
            </ul>
          </>
        )}

        {(live.notes ?? []).length > 0 && (
          <ul class="notes">
            {(live.notes ?? []).map((n, i) => (
              <li key={i} class="caption">
                {n}
              </li>
            ))}
          </ul>
        )}
      </section>

      {programs.length > 0 && (
        <section class="card">
          <h2>Programs right now (an estimate)</h2>
          <ul class="bars wide">
            {programs.slice(0, 15).map((p) => (
              <li key={p.name}>
                <span class="name">{p.name}</span>
                <span class="bar" aria-hidden="true">
                  <i style={{ width: `${topProgram > 0 ? (p.watts / topProgram) * 100 : 0}%` }} />
                </span>
                <span class="num">{formatWatts(p.watts)}</span>
                <span class="num cost">
                  CPU {formatWatts(p.cpuWatts)} · GPU {formatWatts(p.gpuWatts)}
                </span>
              </li>
            ))}
          </ul>
          <p class="caption">
            Only the power above the PC's idle level is shared out, by how busy each program keeps the CPU and GPU. Idle level so far:{' '}
            {idleText(live)}. An estimate, not a measurement.
          </p>
        </section>
      )}
    </>
  );
}
