// The wall view (`#/wall`): the house at a glance on a screen that stays on, a tablet or a Pi on the wall. No navigation, nothing to click: what the house draws
// right now, today's energy and cost, and what is working this moment. It is a page of its own (no top bar), reads the same figures as the Live page and
// keeps the screen awake where the browser allows it.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { Kpi } from './Kpi';
import { usePoll } from './hooks';
import { formatKwh, formatMoney, formatWatts, type Live, type Meter } from './lib';
import { measuredTotals } from './measured';
import { RunningNow } from './RunningNow';
import { Spark, useTrail } from './Spark';

/** The start of the local day, as the summary wants it. */
function midnight(now: Date): string {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate()).toISOString();
}

/** Asks the browser to keep the screen on, again whenever the page comes back (the lock is dropped when it is hidden). Not every browser has it. */
function useAwake() {
  useEffect(() => {
    let lock: { release: () => Promise<void> } | undefined;
    const ask = () => {
      const wl = (navigator as unknown as { wakeLock?: { request: (t: 'screen') => Promise<{ release: () => Promise<void> }> } }).wakeLock;
      if (!wl || document.hidden) return;
      wl.request('screen').then(
        (l) => (lock = l),
        () => undefined,
      );
    };
    ask();
    document.addEventListener('visibilitychange', ask);
    return () => {
      document.removeEventListener('visibilitychange', ask);
      void lock?.release().catch(() => undefined);
    };
  }, []);
}

function useClock(): string {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 15_000);
    return () => clearInterval(id);
  }, []);
  return now.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}

export function WallView({ meters }: { meters: Meter[] }) {
  useAwake();
  const clock = useClock();
  const { data: live, error } = usePoll<Live>(api.live, 2000, []);
  const { data: levels } = usePoll(api.levels, 5 * 60_000, []);
  const { data: summary } = usePoll(() => api.summary(midnight(new Date()), new Date(Date.now() + 60_000).toISOString()), 60_000, []);

  const mains = meters.filter((m) => m.enabled && m.scope === 'mains');
  const whole = mains.length > 0;
  const watts = live ? (whole ? live.house_watts : measuredTotals(meters, undefined, live.readings).watts) : null;
  const trail = useTrail(watts, live?.time);
  const measured = measuredTotals(meters, summary, live?.readings ?? []);
  const kwh = whole ? (summary?.house_kwh ?? null) : measured.kwh;
  const cost = whole ? (summary?.house_cost ?? null) : measured.cost;

  return (
    <div class={`wall${error ? ' stale' : ''}`}>
      <header class="wall-top">
        <span class="wall-title">{whole ? 'House' : 'Everything measured'}</span>
        <span class="wall-clock">
          {error && <span class="wall-off">Server not answering</span>}
          {clock}
        </span>
      </header>
      <Kpi tone="now" label="Right now" value={formatWatts(watts)}>
        <Spark values={trail} label="The last few minutes of the draw" />
      </Kpi>
      <section class="wall-running">
        <h2>Running now</h2>
        {live ? <RunningNow meters={meters} live={live} levels={levels} max={8} big /> : null}
      </section>
      <Kpi tone="energy" label="Energy today" value={kwh != null ? formatKwh(kwh) : '—'} />
      <Kpi tone="cost" label="Cost today" value={cost != null ? formatMoney(cost, summary?.cost_estimated) : '—'} />
    </div>
  );
}
