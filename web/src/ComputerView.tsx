import { api } from './api';
import { Skeleton } from './Skeleton';
import { Chart, lineFrom, wideChartHeight, type Line, type Shade } from './Chart';
import { usePoll, useStored } from './hooks';
import { EnergyHistory } from './EnergyHistory';
import { Estimator } from './Estimator';
import { Kpi, KpiRow } from './Kpi';
import { PcInsides } from './PcInsides';
import { PcLiveCard } from './PcLiveCard';
import { RateNote } from './RateNote';
import { PeriodPicker } from './Periods';
import {
  COUNT_PC, carbonNote, countedTotals, usageBasis, countedWatts, formatKwh, formatMoney, formatWatts, gapShades, isLiveStale, liveSplit, parseCountOff, pcLineStyle, periodRange, periodWords, toggleCount,
  type Meter, type MeterSummary, type ComputerDetail, type PeriodKey,
} from './lib';
import { routePath } from './route';
import { Kwh } from './Watts';

const COLORS = ['--color-accent-blue', '--color-accent-green', '--color-accent-red', '--color-accent-muted'];

interface Loaded {
  detail: ComputerDetail;
  lines: Line[];
  /** What each part of the PC drew, from its collector; empty without one. */
  inside: Line[];
  shades: Shade[];
  domain: [number, number];
}

function ago(iso: string | null | undefined): string {
  if (!iso) return 'never';
  const seconds = Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 1000));
  return seconds < 120 ? 'just now' : seconds < 7200 ? `${Math.round(seconds / 60)} min ago` : `${Math.round(seconds / 3600)} h ago`;
}

/** One computer: the whole PC at its plug, the desk around it, and (when a collector reports for it) its insides. */
/** `embedded` leaves out the way back, for when the page is shown inside another (a favorite on the live page). */
export function ComputerView({ id, meters, embedded = false }: { id: string; meters: Meter[]; embedded?: boolean }) {
  const [period, setPeriod] = useStored('period', '24h');
  const label = (meterId: string) => meters.find((c) => c.id === meterId)?.label ?? meterId;

  const { data, error } = usePoll<Loaded>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const detail = await api.computerDetail(id, from, to);
      // Each plug is drawn on its own line; the lines are never added together.
      const plugs = [detail.wall, ...detail.peripherals].filter((p): p is MeterSummary => !!p);
      const [series, pcSeries] = await Promise.all([
        Promise.all(plugs.map((p) => api.series(p.meter_id, from, to))),
        detail.computer.source_id ? api.computerSeries(id, from, to) : Promise.resolve(undefined),
      ]);
      const lines = series.map((s, i) => lineFrom(label(s.meter_id), i === 0 && detail.wall ? '--color-text' : COLORS[i % COLORS.length]!, s.points));
      const perScope = new Map<string, number>();
      const inside = (pcSeries?.lines ?? []).map((l) => {
        const index = perScope.get(l.scope) ?? 0;
        perScope.set(l.scope, index + 1);
        const style = pcLineStyle(l, index);
        return { ...lineFrom(l.name, style.colorVar, l.points), dashed: style.dashed };
      });
      return { detail, lines, inside, shades: gapShades(pcSeries?.gaps ?? []), domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] as [number, number] };
    },
    60_000,
    [id, period, meters],
  );

  // The look is small and changes every few seconds, so it has its own quick poll; the period data above is slow.
  const { data: look } = usePoll(() => api.computerLive(id), 5_000, [id]);

  // What each plug draws right now, as the server reads it.
  const { data: plugsNow } = usePoll(() => api.live(), 5_000, []);
  const plugWatts = (meterId: string) => plugsNow?.readings.find((r) => r.meter_id === meterId)?.watts ?? null;

  // What the figures count: the PC and each plug next to it, until one is switched off (and a new plug counts until it is).
  const [offText, setOffText] = useStored(`count-off-${id}`, '');
  const off = parseCountOff(offText);
  const toggle = (key: string) => setOffText([...toggleCount(off, key)].join(','));

  const detail = data?.detail;
  const liveNow = look ?? detail?.pc?.live ?? undefined;
  const counted = detail ? countedTotals(detail, off, label) : undefined;
  const hasPlugs = (detail?.peripherals.length ?? 0) > 0;
  // The PC itself right now: its wall plug as the server reads it, else the PC's own look (wall as it reads it, else the sensors).
  const pcWatts = detail?.computer.wall_meter_id != null ? plugWatts(detail.computer.wall_meter_id) : null;
  const pcLook = liveNow && !isLiveStale(liveNow) ? liveSplit(liveNow).baseWatts : null;
  const plugsList = (detail?.peripherals ?? []).map((p) => ({ key: p.meter_id, watts: plugWatts(p.meter_id) }));
  const nowWatts = countedWatts(pcWatts ?? pcLook, plugsList, off);
  const pcWhat = pcWatts != null ? 'the whole PC at the wall' : liveNow && liveSplit(liveNow).baseIsWall ? 'the whole PC at the wall, as it reads it' : 'the sensors added together';
  const nowWhat = [...(off.has(COUNT_PC) ? [] : [pcWhat]), ...(detail?.peripherals ?? []).filter((p) => !off.has(p.meter_id)).map((p) => label(p.meter_id))].join(', ') || 'nothing counted';
  const pcPeak = detail?.wall?.peak_watts ?? detail?.pc?.total_peak_watts ?? null;
  // The sensors' peak stands in for the wall's only when there is no wall plug and nothing else counts.
  const peak = hasPlugs && counted && counted.parts.length > 0 ? counted.peak : pcPeak;
  const peakIsBound = hasPlugs ? (counted?.peakIsBound ?? false) : false;
  return (
    <>
      {!embedded && (
        <a class="back" href={routePath('hierarchy')}>
          ← Hierarchy
        </a>
      )}

      {error && (
        <section class="card warn">
          <p>{error.message}</p>
        </section>
      )}
      {!data && !error && <Skeleton />}

      {detail && (
        <>
          <header class="page-head">
            <div>
              <h2 class="page-title">{detail.computer.name}</h2>
            </div>
            <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
          </header>
          <KpiRow>
            <Kpi tone="now" label="Right now" value={nowWatts != null ? formatWatts(nowWatts) : '—'} />
            <Kpi tone="energy" label="Energy" value={(hasPlugs ? counted?.kwh : detail.wall?.kwh) != null ? formatKwh(hasPlugs ? counted!.kwh : detail.wall!.kwh) : '—'} hint={periodWords(period as PeriodKey)} />
            <Kpi tone="cost" label="Cost" value={(hasPlugs ? counted?.cost : detail.wall?.cost) != null ? formatMoney(hasPlugs ? counted!.cost : detail.wall!.cost, detail.cost_estimated) : '—'} hint={periodWords(period as PeriodKey)} />
            <Kpi tone="peak" label="Highest draw" value={peak != null ? `${peakIsBound ? '≤ ' : ''}${formatWatts(peak)}` : '—'} hint={peakIsBound ? 'an upper bound' : 'the most in one reading'} />
          </KpiRow>
          <div class="kpi-notes">
            {hasPlugs && (
              <div class="count" role="group" aria-label="What to count">
                <span class="caption">Count:</span>
                {detail.wall && (
                  <label>
                    <input type="checkbox" checked={!off.has(COUNT_PC)} onChange={() => toggle(COUNT_PC)} /> PC
                  </label>
                )}
                {detail.peripherals.map((p) => (
                  <label key={p.meter_id}>
                    <input type="checkbox" checked={!off.has(p.meter_id)} onChange={() => toggle(p.meter_id)} /> {label(p.meter_id)}
                  </label>
                ))}
              </div>
            )}
            <p class="caption">
              {hasPlugs
                ? counted && counted.parts.length > 0
                  ? `Counting ${counted.parts.join(', ')}. Each is measured on its own plug; the PC's figure does not include the others.${peakIsBound ? ' The highest draw is an upper bound: the parts need not peak together.' : ''}`
                  : 'Nothing is counted: tick the PC or a plug above.'
                : detail.wall
                  ? 'The whole PC at the wall.'
                  : 'No plug is tied to this computer yet. Tie its wall plug to it under Computers in the Hierarchy tab.'}
              {!hasPlugs && peak != null && !detail.wall?.peak_watts ? ' Max draw is the highest the sensors reached together; the wall is not measured.' : ''}
            </p>
            {counted && carbonNote(counted.carbonG, counted.kwh, counted.carbonEstimatedKwh) && (
              <p class="caption">{carbonNote(counted.carbonG, counted.kwh, counted.carbonEstimatedKwh)}</p>
            )}
            <RateNote kwh={hasPlugs ? counted?.kwh : detail.wall?.kwh} cost={hasPlugs ? counted?.cost : detail.wall?.cost} />
          </div>

          {liveNow && <PcLiveCard live={liveNow} />}

          {detail.peripherals.length > 0 && detail.setup_kwh != null && (
            <section class="card">
              <h2>The whole desk</h2>
              <p class="big small"><Kwh k={detail.setup_kwh} /></p>
              <p class="caption">
                The PC plus all of its peripherals{detail.setup_cost != null ? `, ${formatMoney(detail.setup_cost, detail.cost_estimated)}` : ''}, whatever is
                ticked above.
              </p>
              <ul class="bars wide">
                {detail.peripherals.map((p) => (
                  <li key={p.meter_id}>
                    <span class="name">{label(p.meter_id)}</span>
                    <span class="bar" aria-hidden="true">
                      <i style={{ width: `${detail.setup_kwh! > 0 ? (p.kwh / detail.setup_kwh!) * 100 : 0}%` }} />
                    </span>
                    <span class="num"><Kwh k={p.kwh} /></span>
                    <span class="num cost">{p.cost != null ? formatMoney(p.cost, detail.cost_estimated) : ''}</span>
                  </li>
                ))}
              </ul>
            </section>
          )}

          {data.lines.some((l) => l.x.length > 0) && (
            <section class="card wide">
              <h2>Power over time</h2>
              <Chart lines={data.lines} domain={data.domain} height={wideChartHeight()} />
              <p class="caption">Each plug on its own line. They are not added together.</p>
            </section>
          )}

          <Estimator
            load={(window) =>
              api.computerEstimate(id, window, detail.peripherals.map((p) => p.meter_id).filter((cid) => !off.has(cid)), !off.has(COUNT_PC))
            }
            deps={[id, detail.peripherals.map((p) => p.meter_id).filter((cid) => !off.has(cid)).join(','), off.has(COUNT_PC)]}
            basisOf={usageBasis}
            who="the PC"
            nowWatts={nowWatts}
            nowWhat={nowWhat}
          />

          <EnergyHistory id={id} period={period as PeriodKey} />

          {data.inside.some((l) => l.x.length > 0) && (
            <section class="card wide">
              <h2>Inside the PC over time</h2>
              <Chart lines={data.inside} domain={data.domain} shades={data.shades} height={wideChartHeight()} />
              <p class="caption">
                What the PC's own collector recorded: the wall as it read it, each sensor, and the plugs it reads (dashed). The lines are not added
                together. Shaded stretches are times nothing was being recorded.
              </p>
            </section>
          )}

          {detail.pc ? (
            <>
              <p class="caption">
                Inside the PC, reported by its collector (last heard from {ago(detail.pc.last_seen)}).
              </p>
              <PcInsides pc={detail.pc} wholeKwh={detail.wall?.kwh ?? null} />
            </>
          ) : (
            <section class="card">
              <p class="note">
                {detail.computer.source_id
                  ? 'The collector for this computer has not reported in this period.'
                  : 'No collector reports for this computer yet, so there are no sensors or programs to show. What its plugs measure is above.'}
              </p>
            </section>
          )}
        </>
      )}
    </>
  );
}
