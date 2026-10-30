// The gas comparison, in the EV card's settings: the area whose gasoline price is used, and optionally a gas car of the owner's own beside the presets. The
// presets (15 to 40 miles per gallon) are always compared; which are drawn is ticked under the chart. Saving reads the area's weekly prices from EIA at once,
// with the key saved under Carbon.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { parseMpg, type EvCompare } from './ev';

export function EvCompareSettings({ meterId, reload }: { meterId: string; reload: () => void }) {
  const [view, setView] = useState<EvCompare | undefined>();
  const [mpg, setMpg] = useState('');
  const [area, setArea] = useState('');
  const [problem, setProblem] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void api.evCompare(meterId).then(
      (v) => {
        setView(v);
        setMpg(v.custom_mpg != null ? String(v.custom_mpg) : '');
        setArea(v.area_id ?? '');
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
    if (area === '') {
      setProblem('Pick the area whose gasoline price to use.');
      return;
    }
    setSaving(true);
    try {
      const v = await api.putEvCompare(meterId, area, custom);
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

  const remove = async () => {
    try {
      await api.deleteEvCompare(meterId);
      setView(await api.evCompare(meterId));
      setMpg('');
      setArea('');
      setProblem('');
      reload();
    } catch (err) {
      setProblem((err as Error).message);
    }
  };

  const failed = view?.last_outcome != null && view.last_outcome !== 'Prices saved.';
  const presets = view?.presets ?? [];
  return (
    <>
      <h3>Gas car comparison</h3>
      <form class="ev-form" onSubmit={(e) => void save(e)}>
        <label class="field">
          <span>Gas price area</span>
          <select value={area} onChange={(e) => setArea((e.target as HTMLSelectElement).value)} required>
            <option value="" disabled>
              Pick an area
            </option>
            {(view?.areas ?? []).map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </label>
        <label class="field">
          <span>Your own gas car, miles per gallon (optional)</span>
          <input type="text" inputMode="decimal" value={mpg} placeholder="27" onInput={(e) => setMpg((e.target as HTMLInputElement).value)} />
        </label>
        <button type="submit" disabled={saving}>
          {view?.area_id != null ? 'Save' : 'Compare'}
        </button>
        {view?.area_id != null && (
          <button type="button" onClick={() => void remove()}>
            Take off
          </button>
        )}
      </form>
      {presets.length > 0 && <p class="caption">Always compared with gas cars of {presets.join(', ')} miles per gallon; tick which to draw under the chart.</p>}
      {view && !view.key_saved && <p class="caption">The prices come from EIA and need its key: save it under Carbon.</p>}
      {view?.last_outcome != null && (
        <p class={`caption ${failed ? 'bad' : ''}`}>
          {view.last_outcome}
          {!failed && view.latest_price != null && view.latest_week != null ? ` Latest: $${Number(view.latest_price).toFixed(2)} per gallon, week of ${view.latest_week}.` : ''}
        </p>
      )}
      {problem && <p class="note error">{problem}</p>}
    </>
  );
}
