// The battery's usable energy, in the EV card's settings: one figure from the car's specification that turns the car's battery percentages into kWh, so the
// charges can say how much of the wall's energy reached the battery. Ford's telemetry has a battery capacity member but this car leaves it empty.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { parseKwh } from './ev';

export function EvBatterySettings({ meterId, reload }: { meterId: string; reload: () => void }) {
  const [saved, setSaved] = useState<number | null>(null);
  const [typed, setTyped] = useState('');
  const [problem, setProblem] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void api.evBattery(meterId).then(
      (v) => {
        setSaved(v.kwh);
        setTyped(v.kwh != null ? String(v.kwh) : '');
      },
      (e: Error) => setProblem(e.message),
    );
  }, [meterId]);

  const save = async (e: Event) => {
    e.preventDefault();
    const n = parseKwh(typed);
    if (n === null) {
      setProblem("Enter the battery's usable energy in kWh, a number from 10 to 300, like 88.");
      return;
    }
    setSaving(true);
    try {
      const v = await api.putEvBattery(meterId, n);
      setSaved(v.kwh);
      setProblem('');
      reload();
    } catch (err) {
      setProblem((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    try {
      await api.deleteEvBattery(meterId);
      setSaved(null);
      setTyped('');
      setProblem('');
      reload();
    } catch (err) {
      setProblem((err as Error).message);
    }
  };

  return (
    <>
      <h3>Battery</h3>
      <form class="ev-form" onSubmit={(e) => void save(e)}>
        <label class="field">
          <span>Usable energy (kWh)</span>
          <input type="text" inputMode="decimal" value={typed} placeholder="88" onInput={(e) => setTyped((e.target as HTMLInputElement).value)} required />
        </label>
        <button type="submit" disabled={saving}>
          Save
        </button>
        {saved != null && (
          <button type="button" onClick={() => void remove()}>
            Take off
          </button>
        )}
      </form>
      <p class="caption">The usable energy of a full battery, from the car's specification. It turns the battery percentages into kWh, so each charge can show how much of the wall's energy reached the battery.</p>
      {problem && <p class="note error">{problem}</p>}
    </>
  );
}
