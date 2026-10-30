// The house's gas and diesel prices, a section of the EIA card in the Connectors tab: the one area whose weekly price every electric vehicle's gas cars are
// compared at. The prices are EIA's, read with the key saved in the card; saving the area reads them at once. The area is where the house is, so it is set here
// and not on a vehicle.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import type { FuelPrices } from './lib';

export function FuelPricesSection() {
  const [view, setView] = useState<FuelPrices>();
  const [area, setArea] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  const load = (v: FuelPrices) => {
    setView(v);
    setArea(v.area_id ?? '');
  };
  useEffect(() => {
    api.fuelPrices().then(load, (e: Error) => setMessage(e.message));
  }, []);

  const save = (e: Event) => {
    e.preventDefault();
    setBusy(true);
    setMessage('');
    api.setFuelArea(area).then(
      (v) => {
        load(v);
        setBusy(false);
      },
      (err: Error) => {
        setBusy(false);
        setMessage(err.message);
      },
    );
  };

  const remove = () =>
    api.removeFuelArea().then(
      () => {
        setMessage('');
        return api.fuelPrices().then(load);
      },
      (e: Error) => setMessage(e.message),
    );

  const failed = view?.last_outcome != null && view.last_outcome !== 'Prices saved.';
  return (
    <section class="sub">
      <h3>Gas and diesel prices</h3>
      <p class="caption">Weekly prices for the gas cars an electric vehicle is compared with.</p>
      <form onSubmit={save}>
        <label class="field">
          <span>Area</span>
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
        <div class="chips save">
          <button type="submit" disabled={busy || area === '' || area === view?.area_id}>
            {busy ? 'Reading…' : 'Save'}
          </button>
          {view?.area_id != null && (
            <button type="button" disabled={busy} onClick={() => void remove()}>
              Remove
            </button>
          )}
        </div>
      </form>
      {view && !view.key_saved && <p class="caption">Needs the key saved above.</p>}
      {view?.last_outcome != null && (
        <p class={`caption ${failed ? 'bad' : ''}`}>
          {view.last_outcome}
          {!failed && view.latest_price != null && view.latest_week != null ? ` Gas $${Number(view.latest_price).toFixed(2)} per gallon, week of ${view.latest_week}.` : ''}
          {!failed && view.latest_diesel_price != null ? ` Diesel $${Number(view.latest_diesel_price).toFixed(2)}${view.diesel_area_name ? ` (${view.diesel_area_name})` : ''}.` : ''}
        </p>
      )}
      {message && (
        <p class="note error" role="alert">
          {message}
        </p>
      )}
    </section>
  );
}
