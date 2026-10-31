// What is working this moment, a line each: the appliances (never a feed or a panel) above their own resting level, biggest first. Kept short; the rest is
// counted. With nothing working it says so, and with no levels learned yet (too little history) it says nothing at all.
import { meterPath } from './Bars';
import { levelsById, runningNow, type Levels } from './levels';
import type { Live, Meter } from './lib';
import { Watts } from './Watts';

export function RunningNow({ meters, live, levels, max = 5, big }: { meters: Meter[]; live: Live; levels: Levels | undefined; max?: number; big?: boolean }) {
  if (levelsById(levels).size === 0) return null;
  const running = runningNow(meters, live.readings, levels);
  if (running.length === 0) return <p class="running-none">Nothing running</p>;
  const shown = running.slice(0, max);
  return (
    <ul class={`running${big ? ' big' : ''}`} aria-label="Running right now, biggest first">
      {shown.map((r) => (
        <li key={r.meter.id}>
          <i aria-hidden="true" />
          {big ? <span class="name">{r.meter.label}</span> : <a class="name" href={meterPath(r.meter)}>{r.meter.label}</a>}
          <span class="num">
            <Watts w={r.watts} />
          </span>
        </li>
      ))}
      {running.length > shown.length && <li class="more">+ {running.length - shown.length} more</li>}
    </ul>
  );
}
