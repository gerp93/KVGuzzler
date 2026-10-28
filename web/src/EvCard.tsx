// The card on a charging meter's page: the car's odometer, typed in now and then, set against what the meter used between readings, so a mile has a
// price that can be set beside a gas car's. The odometer is the user's own figure; nothing is read from the car. The sums are the server's.
import { useState } from 'preact/hooks';
import { api } from './api';
import { dayLabel, formatDistance, formatEfficiency, lowCoverage, parseMiles, stretchLabel, toMiles, today, type DistanceUnit, type MeterEv } from './ev';
import { FordCard } from './FordCard';
import { useDistanceUnit, usePoll } from './hooks';
import { formatKwh, formatMoney, formatPerMile, type Meter } from './lib';
import { Kwh } from './Watts';

/** A cost per distance without its unit, for the big figure (the unit is in the caption under it). */
const perMile = (m: string | null | undefined, estimated: boolean | undefined, unit: DistanceUnit): string =>
  formatPerMile(m, estimated, unit).replace(/\/(mi|km)$/, '');

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

/** The newest odometer reading typed in for a charging meter, wherever the meter is shown: "18,240 mi" with its day, or a plain "no odometer reading yet". */
export function OdometerNow({ meterId, brief }: { meterId: string; brief?: boolean }) {
  const { data } = usePoll<MeterEv>(() => api.meterEv(meterId), 60_000, [meterId]);
  const [unit] = useDistanceUnit();
  if (!data) return null;
  const latest = data.readings[data.readings.length - 1];
  if (!latest) return <span class="odometer none">{brief ? 'no odometer yet' : 'No odometer reading yet'}</span>;
  return (
    <span class="odometer" title={`Odometer as typed on ${dayLabel(latest.day)}`}>
      <strong class="num">{formatDistance(latest.miles, unit)}</strong>
      {!brief && <span class="caption"> as of {dayLabel(latest.day)}</span>}
    </span>
  );
}

export function EvCard({ meter }: { meter: Meter }) {
  const [version, setVersion] = useState(0);
  const { data, error } = usePoll<MeterEv>(() => api.meterEv(meter.id), 60_000, [meter.id, version]);
  const [unit, setUnit] = useDistanceUnit();
  const [day, setDay] = useState(today());
  const [miles, setMiles] = useState('');
  const [problem, setProblem] = useState('');
  const [saving, setSaving] = useState(false);

  const save = async (e: Event) => {
    e.preventDefault();
    const typed = parseMiles(miles);
    if (typed === null) {
      setProblem(`Enter the odometer in ${unit === 'km' ? 'kilometers' : 'miles'}, like 18,240.`);
      return;
    }
    const value = toMiles(typed, unit);
    setSaving(true);
    try {
      await api.putOdometer(meter.id, day, value);
      setProblem('');
      setMiles('');
      setVersion((v) => v + 1);
    } catch (err) {
      setProblem((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const remove = async (d: string) => {
    try {
      await api.deleteOdometer(meter.id, d);
      setProblem('');
      setVersion((v) => v + 1);
    } catch (err) {
      setProblem((err as Error).message);
    }
  };

  const totals = data?.totals ?? null;
  const readings = data?.readings ?? [];
  const stretches = [...(data?.intervals ?? [])].reverse();
  const latest = readings[readings.length - 1];
  return (
    <section class="card ev">
      <h2>Electric vehicle</h2>
      <label class="field ev-unit">
        <span>Show distances in</span>
        <select value={unit} onChange={(e) => setUnit((e.target as HTMLSelectElement).value === 'km' ? 'km' : 'miles')}>
          <option value="miles">Miles</option>
          <option value="km">Kilometers</option>
        </select>
      </label>
      {error && !data && <p class="note error">{error.message}</p>}
      {data && (
        <div class="stat ev-odometer">
          <span class="caption">Odometer</span>
          <p class="big ev-figure">{latest ? formatDistance(latest.miles, unit) : '—'}</p>
          <span class="caption">{latest ? `last reading, ${dayLabel(latest.day)}` : 'no reading yet: type one below'}</span>
        </div>
      )}
      {totals && (
        <div class="stats three">
          <div class="stat">
            <span class="caption">Cost per mile</span>
            <p class="big small ev-figure">{perMile(totals.cost_per_mile, data?.cost_estimated, unit)}</p>
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
            {totals.cost != null && <span class="caption">{formatMoney(totals.cost, data?.cost_estimated)}</span>}
          </div>
        </div>
      )}
      {data && readings.length === 0 && <p class="note">Add the first odometer reading below.</p>}
      {data && readings.length === 1 && <p class="note">Add a second reading to see cost per mile.</p>}

      {stretches.length > 0 && (
        <ol class="ev-stretches" aria-label="Between readings, newest first">
          {stretches.map((s) => (
            <li key={s.from_day}>
              <span class="when">{stretchLabel(s)}</span>
              <strong class="num">{formatPerMile(s.cost_per_mile, s.cost_estimated, unit)}</strong>
              <span class="caption">
                {formatDistance(s.miles, unit)} · <Kwh k={s.kwh} />
                {s.cost != null ? ` · ${formatMoney(s.cost, s.cost_estimated)}` : ''}
                {s.miles_per_kwh != null ? ` · ${formatEfficiency(s.miles_per_kwh, unit)}` : ''}
              </span>
              {lowCoverage(s.coverage) ? (
                <span class="badge" title={`Measured ${Math.round(s.coverage * 100)}% of these days: energy and cost per mile are understated`}>
                  {Math.round(s.coverage * 100)}% measured
                </span>
              ) : (
                <span />
              )}
            </li>
          ))}
        </ol>
      )}
      {data?.since_last && readings.length > 0 && (
        <p class="caption">
          Since {dayLabel(data.since_last.from_day)}: <Kwh k={data.since_last.kwh} />
          {data.since_last.cost != null ? ` · ${formatMoney(data.since_last.cost, data.cost_estimated)}` : ''}
        </p>
      )}

      <form class="ev-form" onSubmit={(e) => void save(e)}>
        <label class="field">
          <span>Date</span>
          <input type="date" value={day} max={today()} onInput={(e) => setDay((e.target as HTMLInputElement).value)} required />
        </label>
        <label class="field">
          <span>Odometer ({unit === 'km' ? 'kilometers' : 'miles'})</span>
          <input type="text" inputMode="decimal" value={miles} placeholder="18,240" onInput={(e) => setMiles((e.target as HTMLInputElement).value)} required />
        </label>
        <button type="submit" disabled={saving}>
          Save reading
        </button>
      </form>
      {problem && <p class="note error">{problem}</p>}

      <FordCard meter={meter} onRead={() => setVersion((v) => v + 1)} />

      {readings.length > 0 && (
        <details>
          <summary>
            {readings.length} {readings.length === 1 ? 'reading' : 'readings'}
          </summary>
          <ul class="ev-readings">
            {[...readings].reverse().map((r) => (
              <li key={r.day}>
                <span>{dayLabel(r.day)}</span>
                <span class="num">{formatDistance(r.miles, unit)}</span>
                <button type="button" class="icon" title="Remove this reading" aria-label={`Remove the reading of ${dayLabel(r.day)}`} onClick={() => void remove(r.day)}>
                  ✕
                </button>
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}
