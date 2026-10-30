// The gas car comparison, in the EV card's settings: optionally a gas car of the owner's own beside the presets (15 to 40 miles per gallon, always compared; which
// are drawn is ticked under the chart). The area whose price is used is the house's, set in the Connectors tab with the EIA key.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { parseMpg, type EvCompare } from './ev';
import { routePath } from './route';

export function EvCompareSettings({ meterId, reload }: { meterId: string; reload: () => void }) {
  const [view, setView] = useState<EvCompare | undefined>();
  const [mpg, setMpg] = useState('');
  const [problem, setProblem] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void api.evCompare(meterId).then(
      (v) => {
        setView(v);
        setMpg(v.custom_mpg != null ? String(v.custom_mpg) : '');
      },
      (e: Error) => setProblem(e.message),
    );
  }, [meterId]);

  const save = async (e: Event) => {
    e.preventDefault();
    const custom = mpg.trim() === '' ? null : parseMpg(mpg);
    if (mpg.trim() !== '' && custom === null) {
      setProblem("Enter the gas car's miles per gallon, a number from 5 to 200, like 27, or leave it empty.");
      return;
    }
    setSaving(true);
    try {
      const v = await api.putEvCompare(meterId, custom);
      setView(v);
      setMpg(v.custom_mpg != null ? String(v.custom_mpg) : '');
      setProblem('');
      reload();
    } catch (err) {
      setProblem((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <h3>Gas car comparison</h3>
      {view && (
        <p class="caption">
          {view.area_id == null ? 'Pick the gas price area' : `Prices for ${view.area_name}, set`} in the <a href={routePath('connectors')}>Connectors tab</a>
          {view.area_id == null ? ' to compare with gas cars.' : '.'}
        </p>
      )}
      <form class="ev-form" onSubmit={(e) => void save(e)}>
        <label class="field">
          <span>Your own gas car, miles per gallon (optional)</span>
          <input type="text" inputMode="decimal" value={mpg} placeholder="27" onInput={(e) => setMpg((e.target as HTMLInputElement).value)} />
        </label>
        <button type="submit" disabled={saving || view?.area_id == null}>
          Save
        </button>
      </form>
      {problem && <p class="note error">{problem}</p>}
    </>
  );
}
