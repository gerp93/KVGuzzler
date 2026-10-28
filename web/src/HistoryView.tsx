import { api } from './api';
import { Chart, lineFrom, tallChartHeight, type Line } from './Chart';
import { EstimatorView } from './EstimatorView';
import { usePoll, useStored } from './hooks';
import { HouseHistoryCard } from './HouseHistoryCard';
import { PeriodPicker } from './Periods';
import { compareOrder, formatMoney, formatWatts, periodRange, sumByTime, type Meter, type PeriodKey, type Summary } from './lib';

/** The colours of the compared lines, in the order they were picked (`--series-N`, one per hue, defined in features.css): more than a few can be shown at once. */
const SERIES = 10;
const seriesColor = (i: number) => `--series-${(i % SERIES) + 1}`;

export function HistoryView({ meters }: { meters: Meter[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const [picked, setPicked] = useStored('chart-meters', '');
  const mains = meters.filter((c) => c.enabled && c.scope === 'mains');
  const others = meters.filter((c) => c.enabled && c.scope !== 'mains');
  const ids = picked.split(',').filter(Boolean);

  // What each meter did over the period: who has any reading at all, and what to say on its button. Read for the period, so the list follows the picker.
  const { data: sums } = usePoll<{ key: string; summary: Summary }>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      return { key: period, summary: await api.summary(from, to) };
    },
    60_000,
    [period],
  );
  const figures = sums?.key === period ? sums.summary : undefined;
  const order = compareOrder(others, figures);
  // A meter with nothing recorded in the period has no line to draw, picked or not.
  const chosen = order.filter((o) => o.usable && ids.includes(o.meter.id)).map((o) => o.meter);
  // The same meters in the order they were picked, so a line keeps its colour as others are added.
  const picks = ids.map((id) => chosen.find((c) => c.id === id)).filter((c): c is Meter => c != null);

  const { data, error } = usePoll<{ lines: Line[]; domain: [number, number] }>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const [house, rest] = await Promise.all([
        Promise.all(mains.map((m) => api.series(m.id, from, to))),
        Promise.all(picks.map((c) => api.series(c.id, from, to))),
      ]);
      const out: Line[] = [];
      // The house line is the mains legs added together; every other line is drawn on its own, never added to it.
      if (house.length > 0) out.push(lineFrom('House', '--color-text', sumByTime(house)));
      rest.forEach((s, i) => out.push(lineFrom(picks[i]?.label ?? s.meter_id, seriesColor(i), s.points)));
      return { lines: out, domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] };
    },
    60_000,
    [period, picks.map((c) => c.id).join(','), meters],
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
        <h2>Compare with the house</h2>
        <p class="caption">Any of them, biggest user in the period first. Each shows its average draw and cost over the period; one that drew nothing then (0 W) is greyed out.</p>
        <div class="chips">
          {order.map(({ meter: c, usable, avgWatts, cost, costEstimated, kwh }) => {
            const at = picks.findIndex((x) => x.id === c.id);
            return (
              <button
                key={c.id}
                type="button"
                class="compare"
                aria-pressed={at >= 0}
                disabled={!usable}
                title={usable ? `${kwh.toFixed(2)} kWh used` : 'This meter drew nothing in the period'}
                onClick={() => toggle(c.id)}
              >
                <span class="compare-name">
                  {at >= 0 && <i class="swatch" style={{ background: `var(${seriesColor(at)})` }} aria-hidden="true" />}
                  {c.label}
                </span>
                <span class="compare-figs">{avgWatts != null || usable ? `${avgWatts != null ? formatWatts(avgWatts) : '…'}${cost != null ? ` · ${formatMoney(cost, costEstimated)}` : ''}` : 'nothing recorded'}</span>
              </button>
            );
          })}
        </div>
      </section>

      <div class="history-pair">
        <HouseHistoryCard period={period as PeriodKey} />
        <EstimatorView meters={meters} />
      </div>
    </>
  );
}
