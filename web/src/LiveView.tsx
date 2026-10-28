// The main dashboard: the whole house, with the same kinds of figure a computer's page has (energy, cost, right now, highest draw, carbon,
// an estimator, a history list, a CSV and a chart), then each circuit. The house is the mains clamps alone: circuits and plugs are parts of
// it and are never added to it.
import { useMemo } from 'preact/hooks';
import { api } from './api';
import { Bars, meterPath } from './Bars';
import { FlowChart } from './FlowChart';
import { AnomalyCard } from './AnomalyCard';
import { Chart, lineFrom, wideChartHeight, type Line } from './Chart';
import { formatAmps, formatVolts } from './amps';
import { GuzzlersCard } from './GuzzlersCard';
import { Kpi, KpiRow } from './Kpi';
import { Lowest, Watts } from './Watts';
import { Facts } from './Facts';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import { measuredTotals, topLevelMeters } from './measured';
import { notMeteredRows, unclamped, type BarRow } from './notMetered';
import { Skeleton } from './Skeleton';
import { Spark, useTrail } from './Spark';
import { routePath } from './route';
import { RateNote } from './RateNote';
import {
  carbonFacts, circuitsByWatts, formatKwh, formatMoney, formatWatts, nestCircuits, periodRange, periodWords, pcSlices, sumByTime,
  type Meter, type Live, type PeriodKey, type Summary,
} from './lib';

interface Period {
  /** The period these figures are for, so the page can tell when what it holds is for the one picked before. */
  key: string;
  summary: Summary;
  line: Line | undefined;
  domain: [number, number];
}

/** The live page: the whole house. A meter, a computer or a group opens on its own page from the links in the tables and the Hierarchy tab. */
export function LiveView({ meters, onAddDevice }: { meters: Meter[]; onAddDevice: () => void }) {
  return <House meters={meters} onAddDevice={onAddDevice} />;
}

function House({ meters, onAddDevice }: { meters: Meter[]; onAddDevice: () => void }) {
  const [period, setPeriod] = useStored('period', '24h');
  const { data: live, error } = usePoll<Live>(api.live, 2000, []);
  const mains = meters.filter((c) => c.enabled && c.scope === 'mains');
  // With whole-house clamps the page is the house. Without them nothing says what the whole house used, so it adds up what is measured at the top of the
  // tree ("Everything measured") and says so, instead of showing dashes.
  const whole = mains.length > 0;
  const subjects = whole ? mains : topLevelMeters(meters);
  const nowWatts = whole ? live?.house_watts : measuredTotals(meters, undefined, live?.readings ?? []).watts;
  const trail = useTrail(nowWatts, live?.time);
  const title = whole ? 'House' : 'Everything measured';

  // The period's figures change slowly: read them once a minute, and when the period or the meters change.
  const { data: figures } = usePoll<Period>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const [summary, series] = await Promise.all([api.summary(from, to), Promise.all(subjects.map((m) => api.series(m.id, from, to)))]);
      const points = series.length > 0 ? sumByTime(series) : [];
      return {
        key: period,
        summary,
        // The line is the mains legs added together (or, without them, the top-level meters added together): the sums that are sound. Every other
        // meter keeps to its own row.
        line: points.length > 0 ? lineFrom(title, '--color-text', points) : undefined,
        domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] as [number, number],
      };
    },
    60_000,
    [period, subjects.map((m) => m.id).join(',')],
  );

  // One array while the figures are the same ones: the chart is rebuilt when this is a different object, and this page re-renders every 2 seconds.
  const chartLines = useMemo(() => (figures?.line ? [figures.line] : []), [figures]);

  if (!live) return error ? <p class="note">{`Waiting for the server: ${error.message}`}</p> : <Skeleton />;

  if (meters.length === 0) {
    return (
      <section class="card">
        <h2>Nothing to show yet</h2>
        <p>Add a Shelly plug or a Refoss monitor to start.</p>
        <div class="chips">
          <button type="button" onClick={onAddDevice}>
            Add a device
          </button>
        </div>
      </section>
    );
  }

  // A sub-panel's feed is followed by the circuits inside it; the house is compared with the top level only.
  // The PC's wall plug is a slice of a circuit: once it says which circuit, it is shown beneath it (and counted in what that circuit carries).
  const base = circuitsByWatts(meters, live.readings);
  const slices = pcSlices(meters, live.readings);
  const top = base[0];
  const placed = slices.filter((r) => r.meter.parent_id && base.some((b) => b.meter.id === r.meter.parent_id));
  const circuits = nestCircuits([...base, ...placed]);
  const house = live.house_watts ?? null;
  // The mains' volts and amps, when one meter is the whole house (several mains are different feeds: their currents are never added).
  const feed = whole && mains.length === 1 ? live.readings.find((r) => r.meter_id === mains[0]!.id) : undefined;
  const feedNow = feed && (feed.volts != null || feed.amps != null) ? `${formatVolts(feed.volts)} · ${formatAmps(feed.amps)}` : undefined;
  // Bars are shares of the house when there is a house figure, else of the biggest load shown.
  const pc = slices.filter((r) => !placed.includes(r));
  // With a house figure, the plugs and computers on circuits nothing clamps are listed inside "Everything else", where they are; without one that line
  // does not exist, so they are rows of the table like the circuits.
  const plugs = unclamped(circuits, pc);
  const tableRows: BarRow[] =
    house != null && mains[0]
      ? [...circuits.filter((r) => !plugs.includes(r)), ...notMeteredRows({ house, clamped: circuits, plugs, summary: figures?.summary, host: mains[0] })]
      : [...circuits, ...pc];
  const scale = house ?? Math.max(0, ...circuits.map((r) => r.watts), ...pc.map((r) => r.watts));

  const summary = figures?.summary;
  const measured = measuredTotals(meters, summary, live.readings);
  const kwh = whole ? (summary?.house_kwh ?? null) : measured.kwh;
  const cost = whole ? (summary?.house_cost ?? null) : measured.cost;
  const peak = whole ? (summary?.house_peak_watts ?? null) : measured.peakWatts;
  // The most current the one whole-house meter carried in the period (a 240 V pair's is the larger leg: never the legs added).
  // One whole-house meter (a 240 V pair is one) has its own exact highest reading; with several, or none (the top-level meters added), the peaks need not coincide: an upper bound.
  const peakIsBound = !(whole && mains.length === 1);
  const peakAmps = whole && mains.length === 1 ? summary?.meters.find((m) => m.meter_id === mains[0]!.id)?.peak_amps : undefined;
  const carbon = whole ? carbonFacts(summary?.house_carbon_g, kwh) : [];
  const span = periodWords(period as PeriodKey);
  // The headings change the moment a period is picked but the figures come from the server a moment later (a long period reads more): until they arrive the
  // ones on the page are the last period's, so say so instead of letting them pass for the new ones.
  const loading = figures?.key !== period;

  return (
    <>
      <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
      {loading && (
        <div class="period-loading" role="status">
          <span class="spinner" aria-hidden="true" />
          <span>Loading {span}…</span>
        </div>
      )}
      {!whole && <p class="caption">Top-level meters only: there is no whole-house clamp, so this is not the whole house.</p>}

      {live.notes.length > 0 && (
        <section class="card warn" aria-live="polite">
          {live.notes.map((n) => (
            <p key={n}>{n}</p>
          ))}
        </section>
      )}

      <KpiRow>
        <Kpi
          tone="now"
          label="Right now"
          value={formatWatts(nowWatts ?? null)}
          hint={feedNow ? <span class="now-current">{feedNow}</span> : undefined}
          corner={
            top && top.watts > 0 ? (
              <>
                Largest: <a href={meterPath(top.meter)}>{top.meter.label}</a> <Watts w={top.watts} />
                {nowWatts != null && nowWatts > 0 ? ` · ${Math.round((top.watts / nowWatts) * 100)}%` : ''}
              </>
            ) : undefined
          }
        >
          <Spark values={trail} label="The last few minutes of the draw" />
        </Kpi>
        <Kpi tone="energy" label="Energy" value={kwh != null ? formatKwh(kwh) : '—'} hint={span} />
        <Kpi
          tone="cost"
          label="Cost"
          value={cost != null ? formatMoney(cost, summary?.cost_estimated) : '—'}
          hint={
            cost == null && kwh != null ? (
              <a class="hintlink" href={routePath('settings')}>
                Set a price
              </a>
            ) : (
              span
            )
          }
        />
        <Kpi
          tone="peak"
          label="Highest draw"
          value={peak != null ? `${peakIsBound ? '≤ ' : ''}${formatWatts(peak)}` : '—'}
          hint={
            <>
              {whole && <Lowest w={summary?.house_min_watts} />}
              {peakAmps != null && (
                <span class="now-current" title="The highest current in the period. Amps run higher than watts ÷ volts when the load has a power factor under 1: motors and electronics draw current that does no work.">
                  {formatAmps(peakAmps)} peak
                </span>
              )}
              {peakIsBound ? 'upper bound' : 'peak reading'}
            </>
          }
        />
      </KpiRow>

      <div class="band band-main">
        {figures?.line && (
          <section class="panel panel-chart">
            <div class="panel-head">
              <h2>Power over time</h2>
              <a class="panel-link" href={routePath('history')}>
                Compare meters in History
              </a>
            </div>
            <Chart lines={chartLines} domain={figures.domain} height={wideChartHeight()} />
          </section>
        )}

        <section class="panel panel-circuits">
          <div class="panel-head">
            <h2>Circuits</h2>
          </div>
          {tableRows.length === 0 ? <p class="note">No circuits are reporting.</p> : <Bars rows={tableRows} of={scale} summary={summary} baseKwh={kwh} period={period as PeriodKey} />}
        </section>
      </div>

      <div class="band band-flow">
        {summary && circuits.length > 0 && (
          <section class="panel panel-flow">
            <div class="panel-head">
              <h2>Energy distribution</h2>
            </div>
            <FlowChart meters={meters} summary={summary} />
          </section>
        )}

        <section class="panel panel-insights">
          <GuzzlersCard meters={meters} summary={summary} houseKwh={kwh} period={period as PeriodKey} />
          {whole && <AnomalyCard />}
        </section>
      </div>


      {/* What the figures above rest on: slower facts, below what happens now */}
      <div class="kpi-notes">
        <Facts items={carbon} table title="Carbon footprint" />
        <RateNote kwh={kwh} cost={cost} table title="Electricity price" />
      </div>
    </>
  );
}
