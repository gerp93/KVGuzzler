import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { TariffPicker } from './TariffPicker';
import { SUSPICIOUSLY_HIGH_RATE, currencySymbol, formatMoney, setCurrencyCode, type Bill, type FixedFee, type RateConfig, type TariffInfo } from './lib';

const blankBill = { start: '', end: '', total: '', kwh: '', fees: [] as { label: string; amount: string }[] };

function describe(b: Bill): string {
  const fees = (b.fixed_fees ?? []).length;
  return `${b.start} to ${b.end}: ${formatMoney(b.total)} for ${b.kwh} kWh${fees ? `, ${fees} flat fee${fees > 1 ? 's' : ''} left out` : ''}`;
}

export function RatesCard() {
  const [cfg, setCfg] = useState<RateConfig>();
  const [message, setMessage] = useState('');
  const [saved, setSaved] = useState(false);
  const [draft, setDraft] = useState(blankBill);
  const [tariff, setTariff] = useState<TariffInfo>();

  useEffect(() => {
    api.rates().then(setCfg, (e: Error) => setMessage(e.message));
    api.tariff().then(setTariff, () => undefined);
  }, []);
  if (!cfg) return <section class="card"><h2>Electricity price</h2><p class="note">{message || 'Loading…'}</p></section>;

  const change = (next: RateConfig) => {
    setCfg(next);
    setSaved(false);
    setMessage('');
  };
  const bills = cfg.bills ?? [];
  const rate = Number(cfg.fixed_rate);
  const looksLikeCents = Number.isFinite(rate) && rate > SUSPICIOUSLY_HIGH_RATE;

  const addBill = () => {
    const fees: FixedFee[] = draft.fees.filter((f) => f.label.trim() || f.amount.trim()).map((f) => ({ label: f.label.trim(), amount: f.amount.trim() }));
    const bill: Bill = { start: draft.start, end: draft.end, total: draft.total.trim(), kwh: Number(draft.kwh) };
    if (fees.length > 0) bill.fixed_fees = fees;
    // The server checks everything (dates, overlap, fees); it says what is wrong when it saves.
    change({ ...cfg, bills: [...bills, bill].sort((a, b) => a.start.localeCompare(b.start)) });
    setDraft(blankBill);
  };

  const save = () =>
    api.setRates(cfg).then(
      (r) => {
        setCfg(r);
        setCurrencyCode(r.currency);
        setSaved(true);
        setMessage('');
        api.tariff().then(setTariff, () => undefined);
      },
      (e: Error) => setMessage(e.message),
    );

  // Preact's input typing wants a single literal for type; the three used here are all plain text-like inputs.
  const field = (key: 'start' | 'end' | 'total' | 'kwh', label: string, type: 'text' | 'date' | 'number' = 'text') => (
    <label class="field">
      <span>{label}</span>
      <input type={type as 'text'} value={draft[key]} onInput={(e) => {
          const v = (e.target as HTMLInputElement).value;
          setDraft((d) => ({ ...d, [key]: v }));
        }} />
    </label>
  );

  return (
    <section class="card">
      <h2>Electricity price</h2>
      <label class="field">
        <span>Price from</span>
        <select value={cfg.mode} onChange={(e) => change({ ...cfg, mode: (e.target as HTMLSelectElement).value as RateConfig['mode'] })}>
          <option value="one">One rate</option>
          <option value="bills">My bills</option>
          <option value="lookup">A utility's published rate</option>
        </select>
      </label>

      <label class="field">
        <span>Currency (three-letter code)</span>
        <input
          maxLength={3}
          placeholder="USD"
          value={cfg.currency ?? 'USD'}
          onInput={(e) => change({ ...cfg, currency: (e.target as HTMLInputElement).value.toUpperCase() })}
        />
      </label>

      <label class="field">
        <span>{cfg.mode === 'bills' ? `Rate for days before the first bill (${currencySymbol(cfg.currency).trim()} per kWh)` : cfg.mode === 'lookup' ? `Rate to use until a published rate is chosen (${currencySymbol(cfg.currency).trim()} per kWh)` : `Rate (${currencySymbol(cfg.currency).trim()} per kWh)`}</span>
        <input
          inputMode="decimal"
          placeholder="0.15"
          value={cfg.fixed_rate ?? ''}
          onInput={(e) => {
            const v = (e.target as HTMLInputElement).value.trim();
            const { fixed_rate: _drop, ...rest } = cfg;
            change(v ? { ...rest, fixed_rate: v } : rest);
          }}
        />
      </label>
      {looksLikeCents && <p class="note">That is more than $1 per kWh. Did you type cents? 15¢ is 0.15.</p>}

      {cfg.mode === 'lookup' && (
        <TariffPicker current={tariff} label={cfg.tariff_label} onChoose={(l) => change({ ...cfg, tariff_label: l })} />
      )}

      {cfg.mode === 'bills' && (
        <>
          <h3>Bills</h3>
          {bills.length === 0 ? (
            <p class="note">No bills yet. Each bill's price (its total minus flat fees, over its kWh) applies to the dates it covers.</p>
          ) : (
            <ul class="settings">
              {bills.map((b, i) => (
                <li key={`${b.start}-${i}`}>
                  <span class="grow">{describe(b)}</span>
                  <button type="button" onClick={() => change({ ...cfg, bills: bills.filter((_, j) => j !== i) })}>
                    Remove
                  </button>
                </li>
              ))}
            </ul>
          )}

          <fieldset class="bill-form">
            <legend>Add a bill</legend>
            {field('start', 'First day', 'date')}
            {field('end', 'Last day', 'date')}
            {field('total', `Total (${currencySymbol(cfg.currency)})`)}
            {field('kwh', 'kWh billed', 'number')}
            {draft.fees.map((f, i) => (
              <div class="fee" key={i}>
                <input
                  aria-label="Flat fee name"
                  placeholder="Flat fee (e.g. Basic service)"
                  value={f.label}
                  onInput={(e) => {
                    const v = (e.target as HTMLInputElement).value;
                    setDraft((d) => ({ ...d, fees: d.fees.map((x, j) => (j === i ? { ...x, label: v } : x)) }));
                  }}
                />
                <input
                  aria-label="Flat fee amount"
                  placeholder="6.00 (a credit is negative)"
                  value={f.amount}
                  onInput={(e) => {
                    const v = (e.target as HTMLInputElement).value;
                    setDraft((d) => ({ ...d, fees: d.fees.map((x, j) => (j === i ? { ...x, amount: v } : x)) }));
                  }}
                />
              </div>
            ))}
            <div class="chips">
              <button type="button" onClick={() => setDraft((d) => ({ ...d, fees: [...d.fees, { label: '', amount: '' }] }))}>
                Add a flat fee
              </button>
              <button type="button" onClick={addBill} disabled={!draft.start || !draft.end || !draft.total || !draft.kwh}>
                Add bill
              </button>
            </div>
          </fieldset>
        </>
      )}

      <div class="chips save">
        <button type="button" aria-pressed="true" onClick={() => void save()}>
          Save price
        </button>
        {saved && <span class="note">Saved.</span>}
      </div>
      {message && <p class="note error" role="alert">{message}</p>}
    </section>
  );
}
