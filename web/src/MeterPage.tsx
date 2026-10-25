// One meter's page: how much it used over a period, what that cost, its highest draw and carbon, what is inside it (a sub-panel's circuits, a
// circuit's plugs, nested as wired, with what is left over as "Other (not metered)"), and a chart of it with the meters directly inside it. The
// parent's own reading already contains the ones inside, so none of this is added up.
import { api } from './api';
import { Bars } from './Bars';
import { Chart, lineFrom, type Line } from './Chart';
import { FlowChart } from './FlowChart';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import { BaseLoadCard } from './BaseLoadCard';
import { CurrentCard } from './Breaker';
import { RateNote } from './RateNote';
import { RunsCard } from './RunsCard';
import { showRuns, type MeterRuns } from './runs';
import { carbonNote, formatKwh, formatMoney, formatWatts, houseShare, nestCircuits, periodRange, withDescendants, type Live, type Meter, type PeriodKey, type Row, type Summary } from './lib';

interface Loaded {
  summary: Summary;
  runs: MeterRuns | undefined;
  lines: Line[];
  domain: [number, number];
}

// The chart's lines: the meter in the text colour, the meters inside it each in a colour of their own.
const COLORS = ['--color-accent-blue', '--color-accent-green', '--color-accent-red', '--color-accent-muted'];

export function MeterPage({ id, meters, back }: { id: string; meters: Meter[]; back?: { href: string; label: string } }) {
  const [period, setPeriod] = useStored('period', '24h');
  const meter = meters.find((m) => m.id === id);
  const { data: live } = usePoll<Live>(api.live, 2000, []);
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
  const carbon = carbonNote(mine?.carbon_g, kwh, mine?.carbon_estimated_kwh ?? 0);
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
        <section class="card hero">
          <div class="row">
            <h2>{meter.label}</h2>
            <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
          </div>
          <div class="stats">
            <div class="stat">
              <span class="caption">Energy</span>
              <p class="big small">{kwh != null ? formatKwh(kwh) : '—'}</p>
            </div>
            <div class="stat">
              <span class="caption">Cost</span>
              <p class="big small">{mine?.cost != null ? formatMoney(mine.cost, data?.summary.cost_estimated) : '—'}</p>
            </div>
            <div class="stat">
              <span class="caption">Right now</span>
              <p class="big small">{formatWatts(watts)}</p>
            </div>
            <div class="stat">
              <span class="caption">Max draw</span>
              <p class="big small">{mine?.peak_watts != null ? formatWatts(mine.peak_watts) : '—'}</p>
            </div>
          </div>
          <p class="caption">{share != null ? `${share}% of the house over this period. ` : ''}Its own reading already counts everything inside it.</p>
          {carbon && <p class="caption">{carbon}</p>}
          <RateNote kwh={kwh} cost={mine?.cost ?? null} />
        </section>
      )}

      {meter && list.length > 0 && (
        <section class="card">
          <h2>Inside it</h2>
          <Bars rows={list} of={scale} summary={data?.summary} baseKwh={kwh} />
          <p class="caption">Shares are of this meter. "Other (not metered)" is its own reading less the meters listed, so nothing is added twice.</p>
        </section>
      )}

      {meter && data && (meters.some((m) => m.parent_id === id && m.enabled)) && (
        <section class="card">
          <h2>Where it goes</h2>
          <FlowChart meters={meters} summary={data.summary} root={id} />
        </section>
      )}

      {meter && <CurrentCard meter={meter} now={live?.readings.find((r) => r.meter_id === id) ?? { volts: null, amps: null }} peakAmps={mine?.peak_amps} />}

      {meter && <BaseLoadCard meterId={id} period={period as PeriodKey} />}

      {meter && showRuns(data?.runs) && <RunsCard runs={data!.runs!} />}

      {data && data.lines.length > 0 && (
        <section class="card">
          <h2>Power over time</h2>
          <Chart lines={data.lines} domain={data.domain} />
          {data.lines.length > 1 && <p class="caption">Each line is read on its own: the meters inside it are part of the first line, not added to it.</p>}
        </section>
      )}
      {data && data.lines.length === 0 && meter && <p class="note">Nothing recorded for this meter in this period.</p>}
    </>
  );
}
