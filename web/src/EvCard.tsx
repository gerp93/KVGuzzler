// The card on a charging meter's page: the car's odometer, set against what the meter used between readings, so a mile has a price that can be set
// beside a gas car's. The odometer is kept up to date from Ford, or typed in by hand. What is everyday (the odometer, cost per mile, efficiency, the
// latest trips) is the card; what is set once (the unit, typed readings, the Ford connection) is behind its Settings button. The sums are the server's.
import { useState } from 'preact/hooks';
import { api } from './api';
import { dayLabel, formatDistance, formatEfficiency, lowCoverage, stretchLabel, type DistanceUnit, type MeterEv } from './ev';
import { EvSettings } from './EvSettings';
import { FordSetup, type SetupStep } from './FordSetup';
import { useFordWait } from './fordWait';
import { useDistanceUnit, usePoll } from './hooks';
import { formatKwh, formatMoney, formatPerMile, type FordLink, type Meter } from './lib';
import { Kwh } from './Watts';

/** A cost per distance without its unit, for the big figure (the unit is in the caption under it). */
const perMile = (m: string | null | undefined, estimated: boolean | undefined, unit: DistanceUnit): string =>
  formatPerMile(m, estimated, unit).replace(/\/(mi|km)$/, '');

/** A meter that is probably the car's charger by its name. */
const LOOKS_LIKE_CHARGER = /\b(ev|tesla|car|charger|charging|vehicle)\b/i;

/** How many trips the card lists before "Show all": one a day adds up. */
const TRIPS_SHOWN = 6;

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

/** A moment as a short phrase: the time when it was today, else the day and the time. */
function whenText(iso: string, now: Date = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const time = d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
  return d.toDateString() === now.toDateString() ? time : `${d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}, ${time}`;
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
  const [unit] = useDistanceUnit();
  const [open, setOpen] = useState(false);
  const [showAll, setShowAll] = useState(false);
  // The step of the connecting dialog to open at, or nothing while it is closed.
  const [setup, setSetup] = useState<SetupStep | null>(null);
  const { waitLeft, wait } = useFordWait(ford);
  const reload = () => setVersion((v) => v + 1);

  const totals = data?.totals ?? null;
  const readings = data?.readings ?? [];
  const trips = [...(data?.intervals ?? [])].reverse();
  const latest = readings[readings.length - 1];
  const sync = syncLine(ford);
  const distanceWord = unit === 'km' ? 'kilometer' : 'mile';
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
        <button type="button" class="ev-toggle" aria-expanded={open} aria-controls="ev-settings" onClick={() => setOpen(!open)}>
          Settings
        </button>
      </header>
      {error && !data && <p class="note error">{error.message}</p>}

      {data && readings.length === 0 && (
        <div class="ev-empty">
          <p>No odometer reading yet.</p>
          <p class="caption">Connect the car to Ford to keep it up to date, or type a reading, from Settings.</p>
          <div class="chips">
            {!ford?.configured && (
              <button type="button" class="setup-primary" onClick={() => setSetup('register')}>
                Connect Ford
              </button>
            )}
            {!open && (
              <button type="button" onClick={() => setOpen(true)}>
                Open settings
              </button>
            )}
          </div>
        </div>
      )}

      {data && readings.length > 0 && (
        <>
          <div class="ev-figures">
            <div class="stat ev-odometer">
              <span class="caption">Odometer</span>
              <p class="big ev-figure">{latest ? formatDistance(latest.miles, unit) : '—'}</p>
              <span class="caption">last reading, {latest ? dayLabel(latest.day) : ''}</span>
            </div>
            {totals ? (
              <>
                <div class="stat">
                  <span class="caption">Cost per {distanceWord}</span>
                  <p class="big small ev-figure">{perMile(totals.cost_per_mile, data.cost_estimated, unit)}</p>
                  <span class="caption">over {formatDistance(totals.miles, unit)}</span>
                </div>
                <div class="stat">
                  <span class="caption">Efficiency</span>
                  <p class="big small ev-figure">{totals.miles_per_kwh != null ? formatEfficiency(totals.miles_per_kwh, unit).replace(/ .*$/, '') : '—'}</p>
                  <span class="caption">{unit === 'km' ? 'kilometers' : 'miles'} per kWh at the wall</span>
                </div>
                <div class="stat">
                  <span class="caption">Energy</span>
                  <p class="big small ev-figure">{formatKwh(totals.kwh).replace(' kWh', '')}</p>
                  <span class="caption">kWh{totals.cost != null ? ` · ${formatMoney(totals.cost, data.cost_estimated)}` : ''}</span>
                </div>
              </>
            ) : (
              <p class="note ev-wait">A second odometer reading gives the cost per {distanceWord}.</p>
            )}
          </div>
          {data.since_last && (
            <p class="caption ev-since">
              Since {dayLabel(data.since_last.from_day)}: <Kwh k={data.since_last.kwh} />
              {data.since_last.cost != null ? ` · ${formatMoney(data.since_last.cost, data.cost_estimated)}` : ''}
            </p>
          )}

          {trips.length > 0 && (
            <>
              <h3 class="ev-sub">Between readings</h3>
              <div class="ev-trips-wrap">
                <table class="ev-trips">
                  <caption class="visually-hidden">Between readings, newest first</caption>
                  <thead>
                    <tr>
                      <th scope="col">Between</th>
                      <th scope="col">Distance</th>
                      <th scope="col">Energy</th>
                      <th scope="col">Cost</th>
                      <th scope="col">Per {distanceWord}</th>
                      <th scope="col">Efficiency</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(showAll ? trips : trips.slice(0, TRIPS_SHOWN)).map((s) => (
                      <tr key={s.from_day}>
                        <td>
                          {stretchLabel(s)}
                          {lowCoverage(s.coverage) && (
                            <span class="badge" title={`Measured ${Math.round(s.coverage * 100)}% of these days: energy and cost are understated`}>
                              {Math.round(s.coverage * 100)}% measured
                            </span>
                          )}
                        </td>
                        <td>{formatDistance(s.miles, unit)}</td>
                        <td>
                          <Kwh k={s.kwh} />
                        </td>
                        <td>{s.cost != null ? formatMoney(s.cost, s.cost_estimated) : '—'}</td>
                        <td>
                          <strong>{formatPerMile(s.cost_per_mile, s.cost_estimated, unit)}</strong>
                        </td>
                        <td>{formatEfficiency(s.miles_per_kwh, unit)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {trips.length > TRIPS_SHOWN && (
                <button type="button" class="ev-more" onClick={() => setShowAll(!showAll)}>
                  {showAll ? 'Show fewer' : `Show all ${trips.length}`}
                </button>
              )}
            </>
          )}
        </>
      )}

      {open && <EvSettings meter={meter} data={data} ford={ford} reload={reload} waitLeft={waitLeft} wait={wait} onSetup={setSetup} />}
      {setup && <FordSetup meter={meter} cfg={ford} startAt={setup} waitLeft={waitLeft} wait={wait} reload={reload} onClose={() => setSetup(null)} />}
    </section>
  );
}
