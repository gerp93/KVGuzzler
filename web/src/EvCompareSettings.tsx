// The car comparison, in the EV card's settings: any number of cars of the owner's own (up to the server's limit), each with its miles per gallon and what it burns,
// beside the presets (15 to 40 miles per gallon, in both fuels, always compared; which are drawn is ticked under the chart). The area whose price is used is the
// house's, set in the Connectors tab with the EIA key.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { parseMpg, type CustomCar, type EvCompare } from './ev';
import { routePath } from './route';

const MAX_CARS = 8;

interface Row {
  key: number;
  mpg: string;
  fuel: CustomCar['fuel'];
}

let nextKey = 1;
const rowsOf = (cars: CustomCar[]): Row[] => cars.map((c) => ({ key: nextKey++, mpg: String(c.mpg), fuel: c.fuel }));

export function EvCompareSettings({ meterId, reload }: { meterId: string; reload: () => void }) {
  const [view, setView] = useState<EvCompare | undefined>();
  const [rows, setRows] = useState<Row[]>([]);
  const [problem, setProblem] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void api.evCompare(meterId).then(
      (v) => {
        setView(v);
        setRows(rowsOf(v.custom_cars));
      },
      (e: Error) => setProblem(e.message),
    );
  }, [meterId]);

  const change = (key: number, patch: Partial<Row>) => setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));

  const save = async (e: Event) => {
    e.preventDefault();
    const cars: CustomCar[] = [];
    for (const r of rows) {
      if (r.mpg.trim() === '') continue; // an empty row is no car
      const mpg = parseMpg(r.mpg);
      if (mpg === null) {
        setProblem("Enter each car's miles per gallon, a number from 5 to 200, like 27, or remove the row.");
        return;
      }
      cars.push({ mpg, fuel: r.fuel });
    }
    setSaving(true);
    try {
      const v = await api.putEvCompare(meterId, cars);
      setView(v);
      setRows(rowsOf(v.custom_cars));
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
      <h3>Cars to compare with</h3>
      {view && (
        <p class="caption">
          {view.area_id == null ? 'Pick the gas price area' : `Prices for ${view.area_name}, set`} in the <a href={routePath('connectors')}>Connectors tab</a>
          {view.area_id == null ? ' to compare with gas cars.' : '.'}
        </p>
      )}
      <form class="ev-form" onSubmit={(e) => void save(e)}>
        {rows.map((r) => (
          <div class="ev-carrow" key={r.key}>
            <label class="field">
              <span>Miles per gallon</span>
              <input type="text" inputMode="decimal" value={r.mpg} placeholder="27" onInput={(e) => change(r.key, { mpg: (e.target as HTMLInputElement).value })} />
            </label>
            <label class="field">
              <span>Fuel</span>
              <select value={r.fuel} onChange={(e) => change(r.key, { fuel: (e.target as HTMLSelectElement).value as Row['fuel'] })}>
                <option value="gas">Gas</option>
                <option value="diesel">Diesel</option>
              </select>
            </label>
            <button type="button" onClick={() => setRows((rs) => rs.filter((x) => x.key !== r.key))}>
              Remove
            </button>
          </div>
        ))}
        <div class="chips save">
          <button type="button" disabled={rows.length >= MAX_CARS} onClick={() => setRows((rs) => [...rs, { key: nextKey++, mpg: '', fuel: 'gas' }])}>
            Add car
          </button>
          <button type="submit" disabled={saving || view?.area_id == null}>
            Save
          </button>
        </div>
      </form>
      {problem && <p class="note error">{problem}</p>}
    </>
  );
}
