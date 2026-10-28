import { api } from './api';
import { Chart, lineFrom, tallChartHeight, type Line } from './Chart';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import { periodRange, sumByTime, type Meter, type PeriodKey } from './lib';

const COLORS = ['--color-accent-blue', '--color-accent-green', '--color-accent-red'];

export function HistoryView({ meters }: { meters: Meter[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const [picked, setPicked] = useStored('chart-meters', '');
  const mains = meters.filter((c) => c.enabled && c.scope === 'mains');
  const others = meters.filter((c) => c.enabled && c.scope !== 'mains');
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
      if (house.length > 0) out.push(lineFrom('House', '--color-text', sumByTime(house)));
      rest.forEach((s, i) => out.push(lineFrom(chosen[i]?.label ?? s.meter_id, COLORS[i % COLORS.length]!, s.points)));
      return { lines: out, domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] };
    },
    60_000,
    [period, picked, meters],
  );

  const toggle = (id: string) => {
    const set = new Set(ids);
    if (!set.delete(id)) set.add(id);
    setPicked([...set].join(','));
  };

  return (
    <>
      <section class="card wide chart-card">
        <div class="row">
          <h2>Power over time</h2>
          <PeriodPicker value={period as PeriodKey} onChange={setPeriod} inline />
        </div>
        {error && <p class="note">{error.message}</p>}
        {data && data.lines.some((l) => l.x.length > 0) ? (
          <Chart lines={data.lines} domain={data.domain} height={tallChartHeight()} />
        ) : (
          <p class="note">{data ? 'Nothing recorded in this period yet.' : 'Loading…'}</p>
        )}
        <p class="caption">Average watts per point</p>
      </section>

      <section class="card wide aside-card">
        <h2>Compare with the house (up to 3)</h2>
        <div class="chips">
          {others.map((c) => {
            const at = chosen.findIndex((x) => x.id === c.id);
            return (
              <button key={c.id} type="button" aria-pressed={at >= 0} disabled={at < 0 && chosen.length >= 3} onClick={() => toggle(c.id)}>
                {at >= 0 && <i class="swatch" style={{ background: `var(${COLORS[at % COLORS.length]})` }} aria-hidden="true" />}
                {c.label}
              </button>
            );
          })}
        </div>
      </section>
    </>
  );
}
