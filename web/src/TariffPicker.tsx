// "Price from a utility's published rate". What it does, in order: you give the free OpenEI key once; you type your address or ZIP code and it lists
// the residential rates for it; you pick the one your bill names; from then on costs are worked out from that rate. The key is saved on the server
// (a private file, not the database), never shown again, and sent to api.openei.org only.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { TARIFFS_SHOWN, filterTariffs, rateRange, sharedUtility, type TariffCandidate, type TariffInfo } from './lib';

function Notes({ c, fixed = false }: { c: TariffCandidate | TariffInfo; fixed?: boolean }) {
  const bits = [
    'is_default' in c && c.is_default ? 'most common' : '',
    'approved' in c && c.approved ? 'checked by OpenEI' : '',
    c.tiers > 1 ? 'first usage tier only' : '',
    fixed && c.fixed_charge ? `${c.fixed_charge} fixed charge not included in the price` : '',
  ].filter(Boolean);
  return bits.length > 0 ? <span class="caption block">{bits.join(' · ')}</span> : null;
}

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
  const [filter, setFilter] = useState('');
  const [showAll, setShowAll] = useState(false);
  const [changing, setChanging] = useState(false);
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
    setShowAll(false);
    setFilter('');
    api.lookupRates(address.trim()).then(
      (list) => {
        setFound(list);
        setBusy(false);
        if (list.length === 0) setMessage('No residential rates found. Try a ZIP code.');
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
      },
    );
  };

  const matching = found ? filterTariffs(found, filter) : [];
  const listed = showAll || filter.trim() ? matching : matching.slice(0, TARIFFS_SHOWN);
  const utility = found ? sharedUtility(found) : null;
  const chosenButNotSaved = label && label !== current?.label;
  const searching = !current || changing;

  return (
    <div class="tariff">
      {current && (
        <div class="current-rate">
          <h3>Your rate</h3>
          <p>
            <strong>{current.name}</strong>
            <span class="caption block">
              {current.utility} · {rateRange(current)}
            </span>
            <Notes c={current} fixed />
          </p>
          <p class="caption">Updated {new Date(current.fetched_at).toLocaleDateString()}</p>
          {!changing && (
            <button type="button" onClick={() => setChanging(true)}>
              Change rate
            </button>
          )}
        </div>
      )}

      {hasKey === false && (
        <>
          <h3>OpenEI key</h3>
          <p class="caption">Free key from openei.org (Utility Rate Database). Stored on the server and sent to OpenEI only.</p>
          <label class="field">
            <span>API key</span>
            <input type="password" autocomplete="off" value={key} onInput={(e) => setKey((e.target as HTMLInputElement).value)} />
          </label>
          <button type="button" disabled={key.trim().length < 8} onClick={() => void saveKey(key.trim())}>
            Save key
          </button>
        </>
      )}

      {hasKey && searching && (
        <>
          <h3>{current ? 'Find a different rate' : 'Find your rate'}</h3>
          <label class="field">
            <span>Address or ZIP code</span>
            <input value={address} onInput={(e) => setAddress((e.target as HTMLInputElement).value)} onKeyDown={(e) => e.key === 'Enter' && address.trim() && !busy && search()} />
          </label>
          <button type="button" disabled={busy || !address.trim()} onClick={search}>
            {busy ? 'Looking…' : 'Look up rates'}
          </button>

          {found && found.length > 0 && (
            <>
              <p class="caption">
                {utility ? `${utility} · ` : ''}
                {found.length} rate{found.length === 1 ? '' : 's'}
              </p>
              {found.length > TARIFFS_SHOWN && (
                <label class="field">
                  <span>Narrow the list (words from the name)</span>
                  <input value={filter} placeholder="residential service" onInput={(e) => setFilter((e.target as HTMLInputElement).value)} />
                </label>
              )}
              {listed.length === 0 ? (
                <p class="note">Nothing matches those words.</p>
              ) : (
                <ul class="settings rates">
                  {listed.map((c) => (
                    <li key={c.label}>
                      <span class="grow">
                        <strong>{c.name}</strong>
                        <span class="caption block">
                          {utility ? '' : `${c.utility} · `}
                          {rateRange(c)}
                        </span>
                        <Notes c={c} />
                      </span>
                      <button type="button" aria-pressed={label === c.label} onClick={() => onChoose(c.label)}>
                        {label === c.label ? 'Chosen' : 'Use this'}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {!showAll && !filter.trim() && matching.length > TARIFFS_SHOWN && (
                <button type="button" onClick={() => setShowAll(true)}>
                  Show all {matching.length}
                </button>
              )}
            </>
          )}
        </>
      )}

      {chosenButNotSaved && <p class="note">Selected. Save the price to apply it.</p>}
      {message && (
        <p class="note" role="status">
          {message}
        </p>
      )}

      {hasKey && (
        <details>
          <summary>Method</summary>
          <p class="caption">
            Energy charge only (price per kWh): fixed and demand charges are left out, and only the first usage tier applies. A time-of-day rate becomes one
            price per day, weighted by when the house uses power (last 60 days). Estimates; your bill is exact.
          </p>
          <p class="caption">
            <button type="button" onClick={() => void saveKey('')}>
              Remove OpenEI key
            </button>
          </p>
        </details>
      )}
    </div>
  );
}
