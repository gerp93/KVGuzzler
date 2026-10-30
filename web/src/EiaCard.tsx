// The US EIA connection, in the Connectors tab: its free API key (write-only) and your balancing authority's code, which the carbon footprint reads and which the
// gas and diesel prices on the EV card share the key of. Saving takes a reading first and only keeps a setup that got one. The key is kept in a private file
// on the server and sent to EIA only.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { Hint } from './Hint';
import type { CarbonConfig } from './lib';

export function EiaCard() {
  const [cfg, setCfg] = useState<CarbonConfig>();
  const [place, setPlace] = useState('');
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  const load = (c: CarbonConfig) => {
    setCfg(c);
    if (c.configured) setPlace(c.place ?? '');
  };
  useEffect(() => {
    api.carbon().then(load, (e: Error) => setMessage(e.message));
  }, []);

  const save = () => {
    setBusy(true);
    setMessage('');
    api.setCarbon({ place: place.trim(), key: key.trim() || undefined }).then(
      (c) => {
        load(c);
        setKey('');
        setBusy(false);
        setMessage(`Saved. ${c.last_grams != null ? `The grid is at ${Math.round(c.last_grams)} g CO₂e per kWh right now.` : ''}`);
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
      },
    );
  };

  const remove = () =>
    api.removeCarbon().then(
      () => {
        setKey('');
        setMessage('Removed. The hours already saved are kept.');
        return api.carbon().then(load);
      },
      (e: Error) => setMessage(e.message),
    );

  return (
    <section class="card page-narrow">
      <h2>EIA</h2>
      <Hint>
        Used for the carbon footprint (each hour's energy times that hour's grid intensity, from EIA hourly generation by fuel for your balancing authority, US
        only, grid average and not marginal) and for the gas and diesel prices on the EV card. Needs a free EIA API key (eia.gov/opendata), stored on the
        server and sent to EIA only.
      </Hint>
      <label class="field">
        <span>Balancing authority's code (like MISO, PJM, ERCO, CISO)</span>
        <input value={place} placeholder="MISO" onInput={(e) => setPlace((e.target as HTMLInputElement).value)} />
      </label>
      <label class="field">
        <span>EIA API key {cfg?.configured ? '(leave empty to keep the saved one)' : ''}</span>
        <input type="password" autocomplete="off" value={key} onInput={(e) => setKey((e.target as HTMLInputElement).value)} />
      </label>
      <div class="chips save">
        <button type="button" disabled={busy || !place.trim() || (!key.trim() && !cfg?.configured)} onClick={save}>
          {busy ? 'Checking…' : 'Save and check'}
        </button>
        {cfg?.configured && (
          <button type="button" onClick={() => void remove()}>
            Remove
          </button>
        )}
      </div>
      {message && <p class="note" role="status">{message}</p>}
      {cfg?.last_outcome && (
        <p class="caption">
          Last check{cfg.last_fetch ? ` ${new Date(cfg.last_fetch).toLocaleString()}` : ''}: {cfg.last_outcome}
          {cfg.last_grams != null ? ` ${Math.round(cfg.last_grams)} g/kWh${cfg.last_hour ? ` for the hour of ${new Date(cfg.last_hour).toLocaleString()}` : ''}.` : ''}
        </p>
      )}
    </section>
  );
}
