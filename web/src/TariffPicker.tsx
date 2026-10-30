// "Price from a utility's published rate", in the OpenEI card of the Connectors tab. What it does, in order: you type your address or ZIP code and it lists the
// residential rates for it; you pick the one your bill names; from then on costs are worked out from that rate. The key is saved in the card above it.
import { useState } from 'preact/hooks';
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

/** The rate in use: its name, utility and prices, and when it was read. */
export function CurrentRate({ current, onChange }: { current: TariffInfo; onChange?: () => void }) {
  return (
    <div class="current-rate">
      <p>
        <strong>{current.name}</strong>
        <span class="caption block">
          {current.utility} · {rateRange(current)}
        </span>
        <Notes c={current} fixed />
      </p>
      <p class="caption">Updated {new Date(current.fetched_at).toLocaleDateString()}</p>
      {onChange && (
        <button type="button" onClick={onChange}>
          Change rate
        </button>
      )}
    </div>
  );
}

export function TariffPicker({ current, onChoose }: {
  /** The tariff in use. */
  current: TariffInfo | undefined;
  /** Saves the rate with this label as the price's source; throws the reason when it cannot. */
  onChoose: (label: string) => Promise<void>;
}) {
  const [address, setAddress] = useState('');
  const [found, setFound] = useState<TariffCandidate[]>();
  const [filter, setFilter] = useState('');
  const [showAll, setShowAll] = useState(false);
  const [changing, setChanging] = useState(false);
  const [busy, setBusy] = useState(false);
  const [choosing, setChoosing] = useState('');
  const [message, setMessage] = useState('');

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

  const choose = (label: string) => {
    setChoosing(label);
    setMessage('');
    onChoose(label).then(
      () => {
        setChoosing('');
        setChanging(false);
        setFound(undefined);
      },
      (e: Error) => {
        setChoosing('');
        setMessage(e.message);
      },
    );
  };

  const matching = found ? filterTariffs(found, filter) : [];
  const listed = showAll || filter.trim() ? matching : matching.slice(0, TARIFFS_SHOWN);
  const utility = found ? sharedUtility(found) : null;
  const searching = !current || changing;

  return (
    <div class="tariff">
      {current && <CurrentRate current={current} onChange={changing ? undefined : () => setChanging(true)} />}

      {searching && (
        <>
          <h4>{current ? 'Find a different rate' : 'Find your rate'}</h4>
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
                      <button type="button" disabled={choosing !== ''} onClick={() => choose(c.label)}>
                        {choosing === c.label ? 'Saving…' : 'Use this'}
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
          {changing && (
            <button type="button" onClick={() => setChanging(false)}>
              Cancel
            </button>
          )}
        </>
      )}

      {message && (
        <p class="note error" role="alert">
          {message}
        </p>
      )}

      <details>
        <summary>Method</summary>
        <p class="caption">
          Energy charge only (price per kWh): fixed and demand charges are left out, and only the first usage tier applies. A time-of-day rate becomes one
          price per day, weighted by when the house uses power (last 60 days). Estimates; your bill is exact.
        </p>
      </details>
    </div>
  );
}
