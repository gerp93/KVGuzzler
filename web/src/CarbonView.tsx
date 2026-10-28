// The carbon footprint of the house on a page of its own: what it came to over the period picked, how it moved hour by hour (or day by day) and the grid's
// carbon intensity it was worked out from. The footprint is the house's energy of each hour at that hour's grid intensity (the grid's average mix, never what
// one more kWh would cause), so it follows the same period choice as the other pages.
import { useMemo } from 'preact/hooks';
import { api } from './api';
import { Chart, type Line } from './Chart';
import { Facts } from './Facts';
import { usePoll, useStored } from './hooks';
import { Kpi, KpiRow } from './Kpi';
import { PeriodLoading } from './Loading';
import { PeriodPicker } from './Periods';
import { formatCarbon, formatKwh, periodRange, periodWords, type CarbonConfig, type CarbonSeries, type Meter, type PeriodKey, type Summary } from './lib';
import { routePath } from './route';

interface Figures {
  key: string;
  summary: Summary;
  series: CarbonSeries;
  domain: [number, number];
}

/** Grams on a chart's axis: "800 g", "1.5 kg". */
function gramsAxis(g: number): string {
  return g >= 1000 ? `${g / 1000} kg` : `${g} g`;
}

const plain = (v: number): string => String(v);

/** How long ago a saved hour was, in words ("11 hours ago"), for saying how fresh the grid's figure is. */
function ago(iso: string, now: Date): string {
  const hours = Math.max(0, Math.round((now.getTime() - Date.parse(iso)) / 3_600_000));
  return hours < 1 ? 'this hour' : hours === 1 ? '1 hour ago' : hours < 48 ? `${hours} hours ago` : `${Math.round(hours / 24)} days ago`;
}

export function CarbonView({ meters }: { meters: Meter[] }) {
  const [period, setPeriod] = useStored('period', '24h');
  const whole = meters.some((m) => m.enabled && m.scope === 'mains');
  const { data: grid } = usePoll<CarbonConfig>(api.carbon, 60_000, []);

  const { data: figures, error } = usePoll<Figures>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const [summary, series] = await Promise.all([api.summary(from, to), api.carbonSeries(from, to)]);
      return { key: period, summary, series, domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] };
    },
    60_000,
    [period],
  );
  const loading = figures?.key !== period;
  const span = periodWords(period as PeriodKey);

  // One array while the figures are the same ones, so the charts are not rebuilt by every re-render.
  const lines = useMemo(() => {
    const points = figures?.series.points ?? [];
    const emitted: Line = { name: 'Carbon', colorVar: '--color-text', x: points.map((p) => Date.parse(p.t) / 1000), y: points.map((p) => p.carbon_g) };
    // Only the stretches with a saved intensity: a stretch without one is not a reading, so no point is drawn for it.
    const known = points.filter((p) => p.grid_g_per_kwh != null);
    const intensity: Line = { name: 'Grid intensity', colorVar: '--color-accent-blue', x: known.map((p) => Date.parse(p.t) / 1000), y: known.map((p) => Math.round(p.grid_g_per_kwh!)) };
    return { emitted: [emitted], intensity: [intensity] };
  }, [figures]);

  const summary = figures?.summary;
  const grams = summary?.house_carbon_g;
  const kwh = summary?.house_kwh;
  const estimated = summary?.house_carbon_estimated_kwh ?? 0;
  const average = grams != null && kwh != null && kwh > 0 ? grams / kwh : null;
  const estimatedShare = kwh != null && kwh > 0 ? Math.round((estimated / kwh) * 100) : null;
  const hasPoints = (figures?.series.points.length ?? 0) > 0;
  const per = figures?.series.bucket === 'day' ? 'day' : 'hour';

  return (
    <>
      <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
      {loading && !error && <PeriodLoading span={span} />}
      {!whole && <p class="note">The carbon footprint is worked out from the whole house, so it needs a whole-house clamp: mark the main panel's meter as the house in the Hierarchy tab.</p>}
      {whole && grid && !grid.configured && (
        <p class="note">
          No grid carbon data is set up, so there is no footprint to show. <a href={routePath('settings')}>Set it up in Settings</a> with a free US EIA key and your grid's code.
        </p>
      )}
      {error && <p class="note">{error.message}</p>}

      {whole && (
        <>
          <KpiRow>
            <Kpi tone="carbon" label="Carbon footprint" value={grams != null ? formatCarbon(grams) : '—'} hint={grams != null ? `${span} · the house's energy at the grid's average mix` : span} />
            <Kpi tone="energy" label="Energy" value={kwh != null ? formatKwh(kwh) : '—'} hint={span} />
            <Kpi
              tone="carbon"
              label="Grid average"
              value={average != null ? `${Math.round(average)} g/kWh` : '—'}
              hint={estimatedShare != null && estimatedShare > 0 ? `${estimatedShare}% of the energy at an average` : 'weighted by when it was used'}
            />
            <Kpi
              tone="carbon"
              label="Grid, latest saved"
              value={grid?.last_grams != null ? `${Math.round(grid.last_grams)} g/kWh` : '—'}
              hint={grid?.last_hour ? `${ago(grid.last_hour, new Date())} · ${grid.place ?? ''}` : (grid?.place ?? '')}
            />
          </KpiRow>

          {hasPoints && (
            <>
              <section class="panel panel-chart">
                <div class="panel-head">
                  <h2>{`Carbon per ${per}`}</h2>
                </div>
                <Chart lines={lines.emitted} domain={figures!.domain} format={gramsAxis} label={`Carbon per ${per}`} />
                <p class="caption">{`Grams of CO₂e per ${per}: the house's energy at that ${per}'s grid intensity. ≈, the grid's average mix.`}</p>
              </section>
              <section class="panel panel-chart">
                <div class="panel-head">
                  <h2>Grid carbon intensity (g/kWh)</h2>
                </div>
                <Chart lines={lines.intensity} domain={figures!.domain} format={plain} label="Grid carbon intensity" />
                <p class="caption">How carbon-heavy the grid's power was, by hour: coal weighs most, wind, solar and nuclear least.</p>
              </section>
            </>
          )}
          {figures && !hasPoints && grid?.configured && <p class="note">No grid data saved for this period yet.</p>}

          <section class="panel">
            <div class="panel-head">
              <h2>How this is worked out</h2>
            </div>
            <Facts
              table
              items={[
                { label: 'Your energy', value: "the whole house's, from the mains clamps, each hour" },
                { label: 'Grid intensity', value: `the US EIA's hourly generation by fuel for ${grid?.place ?? 'your grid'}, each fuel at its life-cycle emissions` },
                { label: 'Footprint', value: "each hour's energy × that hour's intensity, added up" },
                {
                  label: 'Latest hour',
                  value: grid?.last_hour ? `${new Date(grid.last_hour).toLocaleString()} (${ago(grid.last_hour, new Date())}); EIA reports with a delay` : 'none saved yet',
                },
                { label: 'Hours with no figure', value: 'valued at the average of the hours that have one, and counted above as "at an average"' },
              ]}
            />
          </section>
        </>
      )}
    </>
  );
}
