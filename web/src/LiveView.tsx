// The main dashboard: the whole house, with the same kinds of figure a computer's page has (energy, cost, right now, highest draw, carbon,
// an estimator, a history list, a CSV and a chart), then each circuit. The house is the mains clamps alone: circuits and plugs are parts of
// it and are never added to it.
import { api } from './api';
import { Bars } from './Bars';
import { ComputerView } from './ComputerView';
import { FlowChart } from './FlowChart';
import { Chart, lineFrom, type Line } from './Chart';
import { Estimator } from './Estimator';
import { HouseHistoryCard } from './HouseHistoryCard';
import { MeterPage } from './MeterPage';
import { usePoll, useStored } from './hooks';
import { PeriodPicker } from './Periods';
import { RateNote } from './RateNote';
import {
  carbonNote, circuitsByWatts, formatKwh, formatMoney, formatWatts, houseUsageBasis, nestCircuits, periodRange, pcSlices, sumByTime, unmetered,
  type Meter, type Live, type PeriodKey, type Summary,
} from './lib';

interface Period {
  summary: Summary;
  line: Line | undefined;
  domain: [number, number];
}

/** The live page: the whole house, or (when some meters are marked as favorites) one click to any of them instead: the computer, the EV
 * charger, a sub-panel. A favorite opens the same page it has in the Hierarchy tab. */
export function LiveView({ meters, onOpenSettings }: { meters: Meter[]; onOpenSettings: () => void }) {
  const [focus, setFocus] = useStored('live-focus', '');
  const favorites = meters.filter((m) => m.favorite && m.enabled);
  const chosen = favorites.find((m) => m.id === focus);
  if (favorites.length === 0) return <House meters={meters} onOpenSettings={onOpenSettings} />;
  return (
    <>
      <div class="chips" role="group" aria-label="What to show">
        <button type="button" aria-pressed={!chosen} onClick={() => setFocus('')}>
          House
        </button>
        {favorites.map((m) => (
          <button key={m.id} type="button" aria-pressed={chosen?.id === m.id} onClick={() => setFocus(m.id)}>
            {m.label}
          </button>
        ))}
      </div>
      {!chosen ? (
        <House meters={meters} onOpenSettings={onOpenSettings} />
      ) : chosen.scope === 'pc' && chosen.computer_id ? (
        <ComputerView id={chosen.computer_id} meters={meters} embedded />
      ) : (
        <MeterPage id={chosen.id} meters={meters} />
      )}
    </>
  );
}

function House({ meters, onOpenSettings }: { meters: Meter[]; onOpenSettings: () => void }) {
  const [period, setPeriod] = useStored('period', '24h');
  const { data: live, error } = usePoll<Live>(api.live, 2000, []);
  const mains = meters.filter((c) => c.enabled && c.scope === 'mains');

  // The period's figures change slowly: read them once a minute, and when the period or the clamps change.
  const { data: figures } = usePoll<Period>(
    async () => {
      const { from, to } = periodRange(period as PeriodKey, new Date());
      const [summary, series] = await Promise.all([api.summary(from, to), Promise.all(mains.map((m) => api.series(m.id, from, to)))]);
      const points = series.length > 0 ? sumByTime(series) : [];
      return {
        summary,
        // The house line is the mains legs added together, the one sum that is sound; every other meter keeps to its own row.
        line: points.length > 0 ? lineFrom('House', '--color-text', points) : undefined,
        domain: [Date.parse(from) / 1000, Date.parse(to) / 1000] as [number, number],
      };
    },
    60_000,
    [period, mains.map((m) => m.id).join(',')],
  );

  if (!live) return <p class="note">{error ? `Waiting for the server: ${error.message}` : 'Loading…'}</p>;

  if (meters.length === 0) {
    return (
      <section class="card">
        <h2>Nothing to show yet</h2>
        <p>No devices are being read. Add a smart plug to start, then the live figures appear here.</p>
        <div class="chips">
          <button type="button" onClick={onOpenSettings}>
            Add a device in Settings
          </button>
        </div>
      </section>
    );
  }

  // A sub-panel's feed is followed by the circuits inside it; the house is compared with the top level only.
  // The PC's wall plug is a slice of a circuit: once it says which circuit, it is shown beneath it (and counted in what that circuit carries).
  const base = circuitsByWatts(meters, live.readings);
  const slices = pcSlices(meters, live.readings);
  const placed = slices.filter((r) => r.meter.parent_id && base.some((b) => b.meter.id === r.meter.parent_id));
  const circuits = nestCircuits([...base, ...placed]);
  const house = live.house_watts ?? null;
  const rest = unmetered(house, circuits);
  // Bars are shares of the house when there is a house figure, else of the biggest load shown.
  const pc = slices.filter((r) => !placed.includes(r));
  const scale = house ?? Math.max(0, ...circuits.map((r) => r.watts), ...pc.map((r) => r.watts));

  const summary = figures?.summary;
  const kwh = summary?.house_kwh ?? null;
  const cost = summary?.house_cost ?? null;
  const peak = summary?.house_peak_watts ?? null;
  const carbon = carbonNote(summary?.house_carbon_g, kwh, summary?.house_carbon_estimated_kwh ?? 0);

  return (
    <>
      <section class="card hero">
        <div class="row">
          <h2>House</h2>
          <PeriodPicker value={period as PeriodKey} onChange={setPeriod} />
        </div>
        <div class="stats">
          <div class="stat">
            <span class="caption">Energy</span>
            <p class="big small">{kwh != null ? formatKwh(kwh) : '—'}</p>
          </div>
          <div class="stat">
            <span class="caption">Cost</span>
            <p class="big small">{cost != null ? formatMoney(cost, summary?.cost_estimated) : '—'}</p>
          </div>
          <div class="stat">
            <span class="caption">Right now</span>
            <p class="big small">{formatWatts(house)}</p>
          </div>
          <div class="stat">
            <span class="caption">Max draw</span>
            <p class="big small">{peak != null ? `≤ ${formatWatts(peak)}` : '—'}</p>
          </div>
        </div>
        <p class="caption">
          {house === null
            ? 'No whole-house clamp is reporting yet.'
            : 'The mains clamps added together. The highest draw is an upper bound: the two legs need not peak in the same second.'}
        </p>
        {carbon && <p class="caption">{carbon}</p>}
        <RateNote kwh={kwh} cost={cost} />
      </section>

      {live.notes.length > 0 && (
        <section class="card warn" aria-live="polite">
          {live.notes.map((n) => (
            <p key={n}>{n}</p>
          ))}
        </section>
      )}

      <section class="card">
        <h2>Circuits</h2>
        {circuits.length === 0 ? <p class="note">No circuits are reporting.</p> : <Bars rows={circuits} of={scale} summary={summary} baseKwh={kwh} />}
        {rest !== null && (
          <p class="derived">
            <span>Other (not metered)</span>
            <span class="num">{formatWatts(rest)}</span>
          </p>
        )}
        {summary?.unmetered_kwh != null && (
          <p class="caption">Over the period, {formatKwh(summary.unmetered_kwh)} of the house's energy went through no circuit clamp.</p>
        )}
      </section>

      {summary && circuits.length > 0 && (
        <section class="card">
          <h2>Where it goes</h2>
          <FlowChart meters={meters} summary={summary} />
          <p class="caption">Over the period picked above: each band is as wide as the energy through it.</p>
        </section>
      )}

      {pc.length > 0 && (
        <section class="card">
          <h2>Also measured</h2>
          <Bars rows={pc} of={scale} summary={summary} baseKwh={kwh} />
          <p class="caption">
            The PC is plugged into one of the circuits above, so this is part of it, never added to the house or circuit
            figures.
          </p>
        </section>
      )}

      {mains.length > 0 && (
        <Estimator
          load={api.houseEstimate}
          deps={[]}
          basisOf={houseUsageBasis}
          who="the house"
          nowWatts={house}
          nowWhat="the mains clamps"
        />
      )}

      {figures?.line && (
        <section class="card">
          <h2>Power over time</h2>
          <Chart lines={[figures.line]} domain={figures.domain} />
          <p class="caption">The mains clamps added together. Compare circuits with the house in History.</p>
        </section>
      )}

      {mains.length > 0 && <HouseHistoryCard period={period as PeriodKey} />}
    </>
  );
}
