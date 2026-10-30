// The OpenEI connection, in the Connectors tab: the free key for the Utility Rate Database and, once there is a key, the house's rate (address or ZIP code, then the
// rate its bill names). The key is write-only: it is saved in a private file on the server, never shown again, and sent to api.openei.org only. Choosing a rate
// makes it the source of the electricity price; the choice between that, one rate and bills stays in Settings.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { ApiKeyField } from './ApiKeyField';
import type { RateConfig, TariffInfo } from './lib';
import { routePath } from './route';
import { TariffPicker } from './TariffPicker';

const SOURCE_NAMES: Record<string, string> = { one: 'One rate', bills: 'Bills', lookup: "A utility's published rate" };

export function OpenEiCard() {
  const [hasKey, setHasKey] = useState<boolean>();
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [tariff, setTariff] = useState<TariffInfo>();
  const [mode, setMode] = useState<RateConfig['mode']>();

  useEffect(() => {
    api.openEi().then((s) => setHasKey(s.has_key), (e: Error) => setMessage(e.message));
    api.tariff().then(setTariff, () => undefined);
    api.rates().then((r) => setMode(r.mode), () => undefined);
  }, []);

  const save = (value: string) => {
    setBusy(true);
    setMessage('');
    api.setOpenEiKey(value).then(
      (s) => {
        setHasKey(s.has_key);
        setKey('');
        setBusy(false);
        setMessage(s.has_key ? 'Key saved.' : 'Key removed.');
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
      },
    );
  };

  // The chosen rate becomes what the price comes from; the server reads it again from OpenEI by its label and says why when it cannot.
  const choose = async (label: string) => {
    const cfg = await api.rates();
    const saved = await api.setRates({ ...cfg, mode: 'lookup', tariff_label: label });
    setMode(saved.mode);
    setTariff(await api.tariff());
  };

  return (
    <section class="card page-narrow">
      <h2>Open Energy Information (OpenEI)</h2>
      <p class="caption">Utility electricity rates, for the electricity price. Free key from openei.org.</p>
      <ApiKeyField saved={hasKey} value={key} onInput={setKey} />
      <div class="chips save">
        <button type="button" disabled={busy || key.trim().length < 8} onClick={() => save(key.trim())}>
          Save
        </button>
        {hasKey && (
          <button type="button" disabled={busy} onClick={() => save('')}>
            Remove
          </button>
        )}
      </div>
      {message && <p class="note" role="status">{message}</p>}

      {hasKey && (
        <section class="sub">
          <h3>Your rate</h3>
          <TariffPicker current={tariff} onChoose={choose} />
          {tariff && mode && mode !== 'lookup' && (
            <p class="note">
              Not used yet: the price comes from {SOURCE_NAMES[mode] ?? mode}. Change it in <a href={routePath('settings')}>Settings</a>.
            </p>
          )}
        </section>
      )}
    </section>
  );
}
