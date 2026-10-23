// The carbon footprint's setup: which source gives the grid's carbon intensity, the key (write-only) and the place. Saving takes a reading first and
// only keeps a setup that got one. The key is kept on the server and sent to its own source only.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { CARBON_SOURCES, type CarbonConfig } from './lib';

export function CarbonCard() {
  const [cfg, setCfg] = useState<CarbonConfig>();
  const [source, setSource] = useState<string>('electricitymaps');
  const [place, setPlace] = useState('');
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  const load = (c: CarbonConfig) => {
    setCfg(c);
    if (c.configured) {
      setSource(c.source ?? 'electricitymaps');
      setPlace(c.place ?? '');
    }
  };
  useEffect(() => {
    api.carbon().then(load, (e: Error) => setMessage(e.message));
  }, []);

  const meta = CARBON_SOURCES.find((s) => s.key === source) ?? CARBON_SOURCES[0];

  const save = () => {
    setBusy(true);
    setMessage('');
    api.setCarbon({ source, place: place.trim(), key: key.trim() || undefined }).then(
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
    <section class="card">
      <h2>Carbon footprint</h2>
      <p class="caption">
        An estimate of the CO₂ the grid emitted to make the electricity used: each hour's energy times that hour's grid intensity. It is the grid's average, not
        what one more kWh would cause. It needs a free key from one source; the server keeps it and sends it to that source only.
      </p>
      <label class="field">
        <span>Source</span>
        <select value={source} onChange={(e) => setSource((e.target as HTMLSelectElement).value)}>
          {CARBON_SOURCES.map((s) => (
            <option key={s.key} value={s.key}>
              {s.label}
            </option>
          ))}
        </select>
      </label>
      <label class="field">
        <span>{meta.placeLabel}</span>
        <input value={place} placeholder={meta.placeholder} onInput={(e) => setPlace((e.target as HTMLInputElement).value)} />
      </label>
      <label class="field">
        <span>API key {cfg?.configured ? '(leave empty to keep the saved one)' : ''}</span>
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
