// One meter's page: how much it used over a period, what that cost, its highest draw and carbon, what is inside it (a sub-panel's circuits, a
// circuit's plugs, nested as wired, with what is left over as "Other (not metered)"), and a chart of it with the meters directly inside it. The
// parent's own reading already contains the ones inside, so none of this is added up.
import { api } from './api';
import { Bars } from './Bars';
import { Chart, lineFrom, wideChartHeight, type Line } from './Chart';
import { FlowChart } from './FlowChart';
import { Kpi, KpiRow } from './Kpi';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import { AnomalyCard } from './AnomalyCard';
import { BaseLoadCard } from './BaseLoadCard';
import { CurrentCard } from './Breaker';
import { EvCard, EvPrompt } from './EvCard';
import { Facts } from './Facts';
import { RateNote } from './RateNote';
import { RunsCard } from './RunsCard';
import { showRuns, type MeterRuns } from './runs';
import { Lowest } from './Watts';
import { carbonFacts, formatKwh, formatMoney, formatWatts, houseShare, nestCircuits, periodRange, periodWords, withDescendants, type Live, type Meter, type PeriodKey, type Row, type Summary } from './lib';

interface Loaded {
  summary: Summary;
  runs: MeterRuns | undefined;
  lines: Line[];
  domain: [number, number];
}

// The chart's lines: the meter in the text colour, the meters inside it each in a colour of their own.
const COLORS = ['--color-accent-blue', '--color-accent-green', '--color-accent-red', '--color-accent-muted'];

export function MeterPage({ id, meters, back, onChanged }: { id: string; meters: Meter[]; back?: { href: string; label: string }; onChanged?: () => void }) {
  const [period, setPeriod] = useStored('period', '24h');
  const meter = meters.find((m) => m.id === id);
  const { data: live } = usePoll<Live>(api.live, 2000, []);
  const { data: levels } = usePoll(api.levels, 5 * 60_000, []);
  const direct = meters.filter((m) => m.parent_id === id && m.enabled).slice(0, 4);

  const { data, error } = usePoll<Loaded>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      // The runs are a nicety: if they cannot be read the rest of the page still shows.
      const [summary, own, runs, ...inside] = await Promise.all([
        api.summary(from, to),
        api.series(id, from, to),
        api.meterRuns(id, from, to).catch(() => undefined),
        ...direct.map((d) => api.series(d.id, from, to)),
      ]);
      const lines: Line[] = [];
      if (own.points.length > 0) lines.push(lineFrom(meter?.label ?? id, '--color-text', own.points));
      inside.forEach((s, i) => {
        if (s.points.length > 0) lines.push(lineFrom(direct[i]!.label, COLORS[i % COLORS.length]!, s.points));
      });
      return { summary, runs, lines, domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] as [number, number] };
    },
    60_000,
    [id, period, direct.map((d) => d.id).join(',')],
  );

  const mine = data?.summary.meters.find((c) => c.meter_id === id);
  const kwh = mine?.kwh ?? null;
  const houseKwh = data?.summary.house_kwh ?? null;
  const share = mine ? houseShare(mine.kwh, houseKwh) : null;
  const carbon = carbonFacts(mine?.carbon_g, kwh);
  const watts = live?.readings.find((r) => r.meter_id === id)?.watts ?? null;

  // What is inside it right now, nested as wired, the meter itself dropped from the list.
  const inside = withDescendants(id, meters);
  const reading = new Map((live?.readings ?? []).map((r) => [r.meter_id, r.watts]));
  const rows: Row[] = meters
    .filter((m) => inside.has(m.id) && m.enabled && reading.has(m.id))
    .map((m) => ({ meter: m, watts: reading.get(m.id)! }));
  const shift = rows.some((r) => r.meter.id === id) ? 1 : 0; // the meter's own row is dropped, and what is inside it moves up a level
  const list = meter ? nestCircuits(rows).filter((r) => r.meter.id !== id || r.remainder).map((r) => ({ ...r, depth: Math.max(0, (r.depth ?? 0) - shift) })) : [];
  const scale = Math.max(0, watts ?? 0, ...rows.map((r) => r.watts));

  return (
    <>
      {back && (
        <a class="back" href={back.href}>
          ← {back.label}
        </a>
      )}
      {error && (
        <section class="card warn">
          <p>{error.message}</p>
        </section>
      )}
      {!meter && <p class="note">There is no such meter.</p>}
      {meter && (
        <>
          <header class="page-head">
            <div>
              <h2 class="page-title">{meter.label}</h2>
              {share != null && <p class="page-sub">{share}% of the house over {periodWords(period as PeriodKey)}</p>}
            </div>
            <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
          </header>
          <KpiRow>
            <Kpi tone="now" label="Right now" value={formatWatts(watts)} />
            <Kpi tone="energy" label="Energy" value={kwh != null ? formatKwh(kwh) : '—'} hint={periodWords(period as PeriodKey)} />
            <Kpi tone="cost" label="Cost" value={mine?.cost != null ? formatMoney(mine.cost, data?.summary.cost_estimated) : '—'} hint={periodWords(period as PeriodKey)} />
            <Kpi tone="peak" label="Highest draw" value={mine?.peak_watts != null ? formatWatts(mine.peak_watts) : '—'} hint={<><Lowest w={mine?.min_watts} />peak reading</>} />
          </KpiRow>
          {meter.ev && <EvCard meter={meter} />}
        </>
      )}

      {/* The chart first (what happened), then what it draws in amps and its runs, what is inside it, and the slower facts. */}
      {data && data.lines.length > 0 && (
        <section class="card wide">
          <h2>Power over time</h2>
          <Chart lines={data.lines} domain={data.domain} height={wideChartHeight()} />
        </section>
      )}
      {data && data.lines.length === 0 && meter && <p class="note">Nothing recorded for this meter in this period.</p>}

      {meter && <CurrentCard meter={meter} now={live?.readings.find((r) => r.meter_id === id) ?? { volts: null, amps: null }} peakAmps={mine?.peak_amps} runs={showRuns(data?.runs) ? data!.runs!.runs : null} />}

      {meter && onChanged && <EvPrompt meter={meter} onChanged={onChanged} />}

      {meter && showRuns(data?.runs) && <RunsCard runs={data!.runs!} breaker={meter.breaker_amps ?? null} />}

      {meter && list.length > 0 && (
        <section class="card">
          <h2>Inside it</h2>
          <Bars rows={list} of={scale} summary={data?.summary} baseKwh={kwh} period={period as PeriodKey} levels={levels} />
        </section>
      )}

      {meter && data && (meters.some((m) => m.parent_id === id && m.enabled)) && (
        <section class="card">
          <h2>Energy distribution</h2>
          <FlowChart meters={meters} summary={data.summary} root={id} />
        </section>
      )}

      {meter && <BaseLoadCard meterId={id} period={period as PeriodKey} />}

      {meter && <AnomalyCard meterId={id} quiet />}

      {meter && (
        <div class="kpi-notes">
          <Facts items={carbon} table title="Carbon footprint" />
          <RateNote kwh={kwh} cost={mine?.cost ?? null} table title="Electricity price" />
        </div>
      )}
    </>
  );
}
