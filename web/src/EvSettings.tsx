// What is set up once and rarely touched on a charging meter's vehicle card, behind its Settings button: how distances are shown, the odometer
// readings (typed by hand, or kept by the Ford connection), and the Ford connection itself.
import { useState } from 'preact/hooks';
import { api } from './api';
import { dayLabel, formatDistance, parseMiles, toMiles, today, type DistanceUnit, type MeterEv } from './ev';
import { EvBatterySettings } from './EvBatterySettings';
import { EvCompareSettings } from './EvCompareSettings';
import { FordConnection } from './FordConnection';
import type { SetupStep } from './FordSetup';
import { useDistanceUnit } from './hooks';
import type { FordLink, Meter } from './lib';

const UNITS: { value: DistanceUnit; label: string }[] = [
  { value: 'miles', label: 'Miles' },
  { value: 'km', label: 'Kilometers' },
];

export function EvSettings({
  meter,
  data,
  ford,
  reload,
  waitLeft,
  wait,
  onSetup,
}: {
  meter: Meter;
  data: MeterEv | undefined;
  ford: FordLink | undefined;
  reload: () => void;
  waitLeft: number;
  wait: (seconds: number) => void;
  onSetup: (step: SetupStep) => void;
}) {
  const [unit, setUnit] = useDistanceUnit();
  const [day, setDay] = useState(today());
  const [typed, setTyped] = useState('');
  const [problem, setProblem] = useState('');
  const [saving, setSaving] = useState(false);

  const save = async (e: Event) => {
    e.preventDefault();
    const value = parseMiles(typed);
    if (value === null) {
      setProblem(`Enter the odometer in ${unit === 'km' ? 'kilometers' : 'miles'}, like 18,240.`);
      return;
    }
    setSaving(true);
    try {
      await api.putOdometer(meter.id, day, toMiles(value, unit));
      setProblem('');
      setTyped('');
      reload();
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
      reload();
    } catch (err) {
      setProblem((err as Error).message);
    }
  };

  const readings = data?.readings ?? [];
  return (
    <div class="ev-settings" id="ev-settings">
      <section class="ev-set">
        <h3>Display</h3>
        <div class="field">
          <span>Show distances in</span>
          <div class="chips" role="group" aria-label="Show distances in">
            {UNITS.map((u) => (
              <button type="button" key={u.value} aria-pressed={unit === u.value} onClick={() => setUnit(u.value)}>
                {u.label}
              </button>
            ))}
          </div>
        </div>

        <h3>Odometer readings</h3>
        <form class="ev-form" onSubmit={(e) => void save(e)}>
          <label class="field">
            <span>Date</span>
            <input type="date" value={day} max={today()} onInput={(e) => setDay((e.target as HTMLInputElement).value)} required />
          </label>
          <label class="field">
            <span>Odometer ({unit === 'km' ? 'kilometers' : 'miles'})</span>
            <input type="text" inputMode="decimal" value={typed} placeholder="18,240" onInput={(e) => setTyped((e.target as HTMLInputElement).value)} required />
          </label>
          <button type="submit" disabled={saving}>
            Save reading
          </button>
        </form>
        {problem && <p class="note error">{problem}</p>}
        {readings.length > 0 && (
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
        )}
      </section>

      <section class="ev-set">
        <EvBatterySettings meterId={meter.id} reload={reload} />
        <EvCompareSettings meterId={meter.id} reload={reload} />
      </section>

      <section class="ev-set">
        <h3>Ford connection</h3>
        <FordConnection cfg={ford} reload={reload} waitLeft={waitLeft} wait={wait} onSetup={onSetup} />
      </section>
    </div>
  );
}
