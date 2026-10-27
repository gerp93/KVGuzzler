// The card on a charging meter's page: the car's odometer, typed in now and then, set against what the meter used between readings, so a mile has a
// price that can be set beside a gas car's. The odometer is the user's own figure; nothing is read from the car. The sums are the server's.
import { useState } from 'preact/hooks';
import { api } from './api';
import { dayLabel, formatEfficiency, formatMiles, lowCoverage, parseMiles, stretchLabel, today, type MeterEv } from './ev';
import { usePoll } from './hooks';
import { formatKwh, formatMoney, formatPerMile, type Meter } from './lib';
import { Kwh } from './Watts';

/** A cost per mile without its unit, for the big figure (the unit is in the caption under it). */
const perMile = (m: string | null | undefined, estimated?: boolean): string => formatPerMile(m, estimated).replace('/mi', '');

export function EvCard({ meter }: { meter: Meter }) {
  const [version, setVersion] = useState(0);
  const { data, error } = usePoll<MeterEv>(() => api.meterEv(meter.id), 60_000, [meter.id, version]);
  const [day, setDay] = useState(today());
  const [miles, setMiles] = useState('');
  const [problem, setProblem] = useState('');
  const [saving, setSaving] = useState(false);

  const save = async (e: Event) => {
    e.preventDefault();
    const value = parseMiles(miles);
    if (value === null) {
      setProblem("Type the odometer as a number of miles, like 18,240.");
      return;
    }
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
  return (
    <section class="card ev">
      <h2>Electric vehicle</h2>
      {error && !data && <p class="note error">{error.message}</p>}
      {totals && (
        <div class="stats three">
          <div class="stat">
            <span class="caption">Cost per mile</span>
            <p class="big small ev-figure">{perMile(totals.cost_per_mile, data?.cost_estimated)}</p>
            <span class="caption">per mile, over {formatMiles(totals.miles)}</span>
          </div>
          <div class="stat">
            <span class="caption">Efficiency</span>
            <p class="big small ev-figure">{totals.miles_per_kwh != null ? totals.miles_per_kwh.toFixed(1) : '—'}</p>
            <span class="caption">miles per kWh at the wall, charging losses included</span>
          </div>
          <div class="stat">
            <span class="caption">Energy for those miles</span>
            <p class="big small ev-figure">{formatKwh(totals.kwh).replace(' kWh', '')}</p>
            <span class="caption">kWh for those miles{totals.cost != null ? `, ${formatMoney(totals.cost, data?.cost_estimated)}` : ''}</span>
          </div>
        </div>
      )}
      {data && readings.length === 0 && <p class="note">Type the car's odometer below, then again next month: the miles in between, over what this meter used, give the cost of a mile.</p>}
      {data && readings.length === 1 && <p class="note">One reading so far. Add the next one (next month is fine) to see the miles and what each cost.</p>}

      {stretches.length > 0 && (
        <ol class="ev-stretches" aria-label="Between readings, newest first">
          {stretches.map((s) => (
            <li key={s.from_day}>
              <span class="when">{stretchLabel(s)}</span>
              <strong class="num">{formatPerMile(s.cost_per_mile, s.cost_estimated)}</strong>
              <span class="caption">
                {formatMiles(s.miles)} · <Kwh k={s.kwh} />
                {s.cost != null ? ` · ${formatMoney(s.cost, s.cost_estimated)}` : ''}
                {s.miles_per_kwh != null ? ` · ${formatEfficiency(s.miles_per_kwh)}` : ''}
              </span>
              {lowCoverage(s.coverage) ? (
                <span class="badge" title={`The meter was measured for ${Math.round(s.coverage * 100)}% of these days, so the energy and the cost per mile are too low.`}>
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
          Since {dayLabel(data.since_last.from_day)} this meter has used <Kwh k={data.since_last.kwh} />
          {data.since_last.cost != null ? ` (${formatMoney(data.since_last.cost, data.cost_estimated)})` : ''}; its miles come with the next reading.
        </p>
      )}

      <form class="ev-form" onSubmit={(e) => void save(e)}>
        <label class="field">
          <span>Date</span>
          <input type="date" value={day} max={today()} onInput={(e) => setDay((e.target as HTMLInputElement).value)} required />
        </label>
        <label class="field">
          <span>Odometer (miles)</span>
          <input type="text" inputMode="decimal" value={miles} placeholder="18,240" onInput={(e) => setMiles((e.target as HTMLInputElement).value)} required />
        </label>
        <button type="submit" disabled={saving}>
          Save reading
        </button>
      </form>
      {problem && <p class="note error">{problem}</p>}

      {readings.length > 0 && (
        <details>
          <summary>
            {readings.length} {readings.length === 1 ? 'reading' : 'readings'}
          </summary>
          <ul class="ev-readings">
            {[...readings].reverse().map((r) => (
              <li key={r.day}>
                <span>{dayLabel(r.day)}</span>
                <span class="num">{formatMiles(r.miles)}</span>
                <button type="button" class="icon" title="Remove this reading" aria-label={`Remove the reading of ${dayLabel(r.day)}`} onClick={() => void remove(r.day)}>
                  ✕
                </button>
              </li>
            ))}
          </ul>
        </details>
      )}
      <p class="caption">
        A reading counts as taken at the start of its day, so a stretch runs from the start of one day to the start of the next. The cost is this meter's energy at the
        price of each day; the meter keeps recording as before. An odometer only goes up.
      </p>
    </section>
  );
}
