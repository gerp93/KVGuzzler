import { api } from './api';
import { Chart, type Line } from './Chart';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import { periodRange, sumByTime, type Channel, type PeriodKey } from './lib';

const COLORS = ['--color-accent-blue', '--color-accent-green', '--color-accent-red'];

const toLine = (name: string, colorVar: string, points: { t: string; avg_watts: number }[]): Line => ({
  name,
  colorVar,
  x: points.map((p) => Date.parse(p.t) / 1000),
  y: points.map((p) => p.avg_watts),
});

export function HistoryView({ channels }: { channels: Channel[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const [picked, setPicked] = useStored('chart-channels', '');
  const mains = channels.filter((c) => c.enabled && c.scope === 'mains');
  const others = channels.filter((c) => c.enabled && c.scope !== 'mains');
  const ids = picked.split(',').filter(Boolean);
  const chosen = others.filter((c) => ids.includes(c.id)).slice(0, 3);

  const { data, error } = usePoll<{ lines: Line[]; domain: [number, number] }>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const [house, rest] = await Promise.all([
        Promise.all(mains.map((m) => api.series(m.id, from, to))),
        Promise.all(chosen.map((c) => api.series(c.id, from, to))),
      ]);
      const out: Line[] = [];
      // The house line is the mains legs added together; every other line is drawn on its own, never added to it.
      if (house.length > 0) out.push(toLine('House', '--color-text', sumByTime(house)));
      rest.forEach((s, i) => out.push(toLine(chosen[i]?.label ?? s.channel_id, COLORS[i % COLORS.length]!, s.points)));
      return { lines: out, domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] };
    },
    60_000,
    [period, picked, channels],
  );

  const toggle = (id: string) => {
    const set = new Set(ids);
    if (!set.delete(id)) set.add(id);
    setPicked([...set].join(','));
  };

  return (
    <>
      <section class="card">
        <div class="row">
          <h2>Power over time</h2>
          <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
        </div>
        {error && <p class="note">{error.message}</p>}
        {data && data.lines.some((l) => l.x.length > 0) ? (
          <Chart lines={data.lines} domain={data.domain} />
        ) : (
          <p class="note">{data ? 'Nothing recorded in this period yet.' : 'Loading…'}</p>
        )}
        <p class="caption">Average watts per point. The house line is the mains clamps; the others are drawn separately.</p>
      </section>

      <section class="card">
        <h2>Compare with the house (up to 3)</h2>
        <div class="chips">
          {others.map((c) => (
            <button key={c.id} type="button" aria-pressed={chosen.some((x) => x.id === c.id)} onClick={() => toggle(c.id)}>
              {c.label}
            </button>
          ))}
        </div>
      </section>
    </>
  );
}
