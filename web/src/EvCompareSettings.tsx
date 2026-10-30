// The gas car an electric vehicle is compared with, in the EV card's settings: its miles per gallon and the area whose gasoline price is used. Saving reads the
// area's weekly prices from EIA at once, with the key saved under Carbon.
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
        setMpg(v.mpg != null ? String(v.mpg) : '');
        setArea(v.area_id ?? '');
      },
      (e: Error) => setProblem(e.message),
    );
  }, [meterId]);

  const save = async (e: Event) => {
    e.preventDefault();
    const n = parseMpg(mpg);
    if (n === null) {
      setProblem("Enter the gas car's miles per gallon, a number from 5 to 200, like 25.");
      return;
    }
    if (area === '') {
      setProblem('Pick the area whose gasoline price to use.');
      return;
    }
    setSaving(true);
    try {
      setView(await api.putEvCompare(meterId, n, area));
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
  return (
    <>
      <h3>Gas car comparison</h3>
      <form class="ev-form" onSubmit={(e) => void save(e)}>
        <label class="field">
          <span>Gas car miles per gallon</span>
          <input type="text" inputMode="decimal" value={mpg} placeholder="25" onInput={(e) => setMpg((e.target as HTMLInputElement).value)} required />
        </label>
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
        <button type="submit" disabled={saving}>
          {view?.mpg != null ? 'Save' : 'Compare'}
        </button>
        {view?.mpg != null && (
          <button type="button" onClick={() => void remove()}>
            Take off
          </button>
        )}
      </form>
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
