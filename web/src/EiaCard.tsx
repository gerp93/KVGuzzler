// The US EIA connection, in the Connectors tab: its free API key (write-only) and your balancing authority's code, which the carbon footprint reads and which the
// gas and diesel prices on the EV card share the key of. Saving takes a reading first and only keeps a setup that got one. The key is kept in a private file
// on the server and sent to EIA only.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { ApiKeyField } from './ApiKeyField';
import { FuelPricesSection } from './FuelPricesSection';
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
      <h2>U.S. Energy Information Administration (EIA)</h2>
      <p class="caption">Grid carbon, and gas and diesel prices. Free key from eia.gov/opendata.</p>
      <section class="sub">
        <h3>Grid carbon</h3>
        <p class="caption">How clean the electricity was each hour, for the carbon footprint.</p>
        <ApiKeyField saved={cfg ? cfg.configured : undefined} value={key} onInput={setKey} />
        <label class="field">
          <span>Balancing authority's code (like MISO, PJM, ERCO, CISO)</span>
          <input value={place} placeholder="MISO" onInput={(e) => setPlace((e.target as HTMLInputElement).value)} />
        </label>
        <div class="chips save">
          <button type="button" disabled={busy || !place.trim() || (!key.trim() && !cfg?.configured)} onClick={save}>
            {busy ? 'Checking…' : 'Save'}
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
      <FuelPricesSection />
    </section>
  );
}
