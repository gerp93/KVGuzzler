// "Price from a utility's published rate": the OpenEI key (write-only), a search by address, the rates found, and the one in use. The key is
// saved on the server and never shown again; it is sent to api.openei.org only.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import type { TariffCandidate, TariffInfo } from './lib';

export function TariffPicker({ current, label, onChoose }: {
  /** The tariff in use. */
  current: TariffInfo | undefined;
  /** The label chosen but not saved yet. */
  label: string | null | undefined;
  onChoose: (label: string) => void;
}) {
  const [hasKey, setHasKey] = useState<boolean>();
  const [key, setKey] = useState('');
  const [address, setAddress] = useState('');
  const [found, setFound] = useState<TariffCandidate[]>();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  useEffect(() => {
    api.openEi().then((s) => setHasKey(s.has_key), () => setHasKey(false));
  }, []);

  const saveKey = (value: string) =>
    api.setOpenEiKey(value).then(
      (s) => {
        setHasKey(s.has_key);
        setKey('');
        setMessage(s.has_key ? 'Key saved.' : 'Key removed.');
      },
      (e: Error) => setMessage(e.message),
    );

  const search = () => {
    setBusy(true);
    setMessage('');
    api.lookupRates(address.trim()).then(
      (list) => {
        setFound(list);
        setBusy(false);
        if (list.length === 0) setMessage('OpenEI had no usable residential rate for that place. Try a ZIP code, or a nearby address.');
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
      },
    );
  };

  return (
    <div class="tariff">
      {current && (
        <p class="note">
          In use: {current.summary}. Read from OpenEI on {new Date(current.fetched_at).toLocaleDateString()}; read again by its label when a week old.
        </p>
      )}

      <h3>OpenEI key</h3>
      {hasKey ? (
        <p class="caption">
          A key is saved on the server and is not shown again.{' '}
          <button type="button" onClick={() => void saveKey('')}>
            Remove it
          </button>
        </p>
      ) : (
        <>
          <p class="caption">
            Lookups use the free Utility Rate Database at openei.org: sign up there for an API key and paste it here once. The server keeps it and sends it
            to api.openei.org only.
          </p>
          <label class="field">
            <span>API key</span>
            <input type="password" autocomplete="off" value={key} onInput={(e) => setKey((e.target as HTMLInputElement).value)} />
          </label>
          <button type="button" disabled={key.trim().length < 8} onClick={() => void saveKey(key.trim())}>
            Save key
          </button>
        </>
      )}

      {hasKey && (
        <>
          <h3>Find your rate</h3>
          <label class="field">
            <span>Address or ZIP code</span>
            <input value={address} onInput={(e) => setAddress((e.target as HTMLInputElement).value)} onKeyDown={(e) => e.key === 'Enter' && address.trim() && !busy && search()} />
          </label>
          <button type="button" disabled={busy || !address.trim()} onClick={search}>
            {busy ? 'Looking…' : 'Look up rates'}
          </button>
          {found && found.length > 0 && (
            <ul class="settings">
              {found.map((c) => (
                <li key={c.label}>
                  <span class="grow">
                    {c.summary}
                    <span class="caption block">
                      {c.is_default ? 'most common for its kind · ' : ''}
                      {c.approved ? 'checked by an OpenEI expert · ' : ''}
                      {c.tiers > 1 ? 'usage tiers: only the first is used · ' : ''}
                      {c.fixed_charge ? `fixed charge ${c.fixed_charge} is not included` : 'fixed and demand charges are not included'}
                    </span>
                  </span>
                  <button type="button" aria-pressed={label === c.label} onClick={() => onChoose(c.label)}>
                    {label === c.label ? 'Chosen' : 'Use this rate'}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      {message && <p class="note" role="status">{message}</p>}
      <p class="caption">
        Only the energy charge is used, never fixed monthly or demand charges. Tiers depend on a whole house's monthly use, so the first tier applies.
        A time-of-use price becomes one price per day, weighted by the hours the house really uses power (learned from the last 60 days).
      </p>
    </div>
  );
}
