// The PC right now, as its collector last saw it: what each part is drawing, how much of the whole PC that is, and which programs are
// causing it. A look is not history: the page asks for a new one every few seconds and says how old the one it shows is.
import { formatCurrent } from './amps';
import { Watts } from './Watts';
import {
  formatAge, idleText, isLiveStale, liveSplit, plugNamed, programsNow, sharePercent,
  type PcLive, type Reading,
} from './lib';

/** A plug the house server reads, as it reads it now: the PC's amps and volts come from there, the collector's own look has only watts. */
export interface PlugNow {
  label: string;
  reading: Reading | undefined;
}

function Row({ name, watts, base, hint, current }: { name: string; watts: number; base: number; hint?: string; current?: string | null }) {
  const share = sharePercent(watts, base);
  return (
    <li>
      <span class="name">{name}</span>
      <span class="bar" aria-hidden="true">
        <i style={{ width: `${base > 0 ? Math.min(100, (watts / base) * 100) : 0}%` }} />
      </span>
      <span class="num">
        <Watts w={watts} />
        {current && <span class="live-current">{current}</span>}
      </span>
      <span class="num cost">{hint ?? (share != null ? `${share}%` : '')}</span>
    </li>
  );
}

/** `wall` is the reading of the PC's own wall plug and `plugs` the plugs beside it, from the house server; each shows its volts and amps under its watts. */
export function PcLiveCard({ live, wall, plugs = [] }: { live: PcLive; wall?: Reading | undefined; plugs?: PlugNow[] }) {
  const split = liveSplit(live);
  const stale = isLiveStale(live);
  const programs = programsNow(live);
  const topProgram = Math.max(0, ...programs.map((p) => p.watts));

  return (
    <>
      <section class={`card tile${stale ? ' warn' : ''}`}>
        <div class="row">
          <h2>Right now</h2>
          <span class="caption">{stale ? `Not answering: last heard ${formatAge(live.age_seconds ?? 0)}` : `Updated ${formatAge(live.age_seconds ?? 0)}`}</span>
        </div>
        <p class="big small"><Watts w={split.baseWatts} /></p>
        {split.baseIsWall && formatCurrent(wall) && <p class="live-current now">{formatCurrent(wall)}</p>}
        <p class="caption">{split.baseIsWall ? 'Whole PC at the wall' : 'Sensors only'}</p>

        {split.sensors.length > 0 && (
          <ul class="bars wide">
            {split.sensors.map((r) => (
              <Row key={r.name} name={r.name} watts={r.watts} base={split.baseWatts} />
            ))}
            {split.otherWatts != null && split.otherWatts > 0 && (
              <Row name="Everything else" watts={split.otherWatts} base={split.baseWatts} />
            )}
          </ul>
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
                <Row
                  key={r.name}
                  name={r.name}
                  watts={r.watts}
                  base={Math.max(...split.meters.map((m) => m.watts))}
                  hint="separate device"
                  current={formatCurrent(plugNamed(r.name, plugs)?.reading)}
                />
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
                <span class="num"><Watts w={p.watts} /></span>
                <span class="num cost">
                  CPU <Watts w={p.cpuWatts} /> · GPU <Watts w={p.gpuWatts} />
                </span>
              </li>
            ))}
          </ul>
          <p class="caption">Idle baseline: {idleText(live)}</p>
        </section>
      )}
    </>
  );
}
