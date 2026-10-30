// The card on a charging meter's page, laid out as the questions it answers: a header (is the car being read, and its odometer), four headline tiles (what driving
// it saved, what a mile costs, how efficiently it uses energy, how much of the wall's energy reaches the battery), then the charts as one tabbed panel with the
// stretches between odometer readings under it, and the charges as a list beside them. What is set once (the unit, typed readings, the battery's size, the gas car,
// the gas cars) is behind the Settings button; how everything is counted is in one note at the foot. The sums are the server's; the pieces only show them.
import { useMemo, useRef, useState } from 'preact/hooks';
import { api } from './api';
import { dayLabel, formatDistance, whenText, type EvChargesAnswer, type EvSavings, type MeterEv } from './ev';
import { EvChargeList } from './EvChargeList';
import { compKey, parsePicks, picksFromOldHidden, togglePick, visibleComparisons } from './evGas';
import { EvHeadline } from './EvHeadline';
import { EvNotes } from './EvNotes';
import { EvSettings } from './EvSettings';
import { trendTabs } from './evTabs';
import { EvTrendPanel } from './EvTrendPanel';
import { EvTrips } from './EvTrips';
import { useDistanceUnit, usePoll, useStored } from './hooks';
import type { FordLink, Meter } from './lib';
import { routePath } from './route';

/** The old saved choice of gas cars switched off, as the choices now are saved: what the viewer had ticked stays ticked. */
function legacyPicks(): string {
  try {
    const old = localStorage.getItem('kvg.ev.gas.hidden');
    if (old) return JSON.stringify(picksFromOldHidden(old));
  } catch {
    // storage unavailable: nothing to carry over
  }
  return '{"off":[],"on":[]}';
}

/** A meter that is probably the car's charger by its name. */
const LOOKS_LIKE_CHARGER = /\b(ev|tesla|car|charger|charging|vehicle)\b/i;

/** On the page of a circuit or plug named like a charger and not marked yet: one button that turns on the odometer and the cost per mile. */
export function EvPrompt({ meter, onChanged }: { meter: Meter; onChanged: () => void }) {
  const [problem, setProblem] = useState('');
  if (meter.ev || (meter.scope !== 'circuit' && meter.scope !== 'plug') || !LOOKS_LIKE_CHARGER.test(meter.label)) return null;
  return (
    <section class="card ev">
      <h2>Electric vehicle</h2>
      <p>Track miles and cost per mile for {meter.label}.</p>
      <button type="button" onClick={() => void api.updateMeter(meter.id, { ev: true }).then(onChanged, (e: Error) => setProblem(e.message))}>
        Track miles
      </button>
      {problem && <p class="note error">{problem}</p>}
    </section>
  );
}

/** The newest odometer reading for a charging meter, wherever the meter is shown: "58,205 mi" with its day, or a plain "no odometer reading yet". */
export function OdometerNow({ meterId, brief }: { meterId: string; brief?: boolean }) {
  const { data } = usePoll<MeterEv>(() => api.meterEv(meterId), 60_000, [meterId]);
  const [unit] = useDistanceUnit();
  if (!data) return null;
  const latest = data.readings[data.readings.length - 1];
  if (!latest) return <span class="odometer none">{brief ? 'no odometer yet' : 'No odometer reading yet'}</span>;
  return (
    <span class="odometer" title={`Odometer on ${dayLabel(latest.day)}`}>
      <strong class="num">{formatDistance(latest.miles, unit)}</strong>
      {!brief && <span class="caption"> as of {dayLabel(latest.day)}</span>}
    </span>
  );
}

/** The one line under the card's title that says how the odometer is kept up to date, or nothing when Ford is not set up. */
function syncLine(ford: FordLink | undefined): { text: string; tone: 'ok' | 'bad' | 'off' } | null {
  if (!ford?.configured) return null;
  if (ford.needs_link) return { text: 'Ford needs to be linked again', tone: 'bad' };
  if (!ford.linked) return { text: 'Ford is not linked yet', tone: 'off' };
  if (ford.last_miles == null) return ford.last_outcome ? { text: `Ford: ${ford.last_outcome}`, tone: 'bad' } : null;
  return { text: `Synced from Ford${ford.last_check ? `, ${whenText(ford.last_check)}` : ''}`, tone: 'ok' };
}

export function EvCard({ meter }: { meter: Meter }) {
  const [version, setVersion] = useState(0);
  const { data, error } = usePoll<MeterEv>(() => api.meterEv(meter.id), 60_000, [meter.id, version]);
  const { data: ford } = usePoll<FordLink>(() => api.ford(), 60_000, [version]);
  const { data: savings } = usePoll<EvSavings>(() => api.evSavings(meter.id), 300_000, [meter.id, version]);
  const { data: charges } = usePoll<EvChargesAnswer>(() => api.evCharges(meter.id), 30_000, [meter.id, version]);
  const [unit] = useDistanceUnit();
  const [open, setOpen] = useState(false);
  const [problem, setProblem] = useState('');
  // The cars the viewer has ticked, remembered in this browser: the gas cars are drawn unless switched off (so one named later is drawn at first), the diesel ones
  // only when switched on. What was saved before there was diesel is read as the gas cars switched off.
  const [picksText, setPicksText] = useStored('kvg.ev.fuel.picks', legacyPicks());
  const picks = parsePicks(picksText);
  const togglePickKey = (key: string) => setPicksText(JSON.stringify(togglePick(picks, key)));
  const reload = () => setVersion((v) => v + 1);

  const tabs = useMemo(() => (data ? trendTabs({ ev: data, savings, charges, unit, picks }) : []), [data, savings, charges, unit, picksText]); // eslint-disable-line react-hooks/exhaustive-deps
  const comparisons = savings?.comparisons ?? [];
  const visible = visibleComparisons(comparisons, picks).map(compKey);
  const removeCharge = (id: number) => {
    setProblem('');
    void api.deleteEvCharge(meter.id, id).then(reload, (e: Error) => setProblem(e.message));
  };

  const readings = data?.readings ?? [];
  const latest = readings[readings.length - 1];
  const sync = syncLine(ford);
  // The settings are a panel at the foot of the card; the buttons elsewhere that ask for one setting open it and scroll to it.
  const panel = useRef<HTMLDetailsElement>(null);
  const openSettings = () => {
    setOpen(true);
    setTimeout(() => panel.current?.scrollIntoView({ block: 'start' }), 50);
  };
  // A car with no odometer reading and no charge yet has nothing to lay out: it is asked to be connected instead.
  const hasContent = readings.length > 0 || (charges?.charges.length ?? 0) > 0;

  return (
    <section class="card ev">
      <header class="ev-head">
        <div>
          <h2>Electric vehicle</h2>
          {sync && (
            <p class={`ev-sync ${sync.tone}`}>
              <span class="dot" aria-hidden="true" />
              {sync.text}
            </p>
          )}
        </div>
        <div class="ev-head-right">
          {latest && (
            <div class="ev-odo">
              <span class="caption">Odometer</span>
              <strong class="num">{formatDistance(latest.miles, unit)}</strong>
              <span class="caption">as of {dayLabel(latest.day)}</span>
            </div>
          )}
        </div>
      </header>
      {error && !data && <p class="note error">{error.message}</p>}

      {data && !hasContent && (
        <div class="ev-empty">
          <p>No odometer reading yet.</p>
          <p class="caption">Connect the car to Ford to keep it up to date, or type a reading in the settings.</p>
          <div class="chips">
            {!ford?.configured && (
              <button type="button" class="setup-primary" onClick={() => (window.location.hash = routePath('connectors'))}>
                Connect Ford
              </button>
            )}
            {!open && (
              <button type="button" onClick={openSettings}>
                Open settings
              </button>
            )}
          </div>
        </div>
      )}

      {data && hasContent && (
        <>
          <EvHeadline ev={data} savings={savings} charges={charges} unit={unit} visible={visible} onOpenSettings={openSettings} />
          <div class="ev-body">
            <div class="ev-main">
              <EvTrendPanel tabs={tabs} comparisons={comparisons} picks={picks} onTogglePick={togglePickKey} />
              <EvTrips ev={data} unit={unit} />
            </div>
            <div class="ev-side">
              {charges && <EvChargeList data={charges} onRemove={removeCharge} problem={problem} onOpenSettings={openSettings} />}
            </div>
          </div>
        </>
      )}

      <div class="ev-foot">
        {data && hasContent && <EvNotes areaName={savings?.area_name ?? null} gas={savings?.configured ?? false} dieselArea={savings?.diesel_area_name ?? null} battery={charges?.battery_kwh != null} />}
        <details class="ev-panel" ref={panel} open={open} onToggle={(e) => setOpen((e.currentTarget as HTMLDetailsElement).open)}>
          <summary>Vehicle settings</summary>
          {open && <EvSettings meter={meter} data={data} reload={reload} />}
        </details>
      </div>
    </section>
  );
}
