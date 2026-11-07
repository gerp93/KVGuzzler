// The electricity bills. They are kept whatever the price comes from: when it comes from the bills they set the price of the days they cover, and either way
// they are what the recorded energy is checked against.
import { useState } from 'preact/hooks';
import { api } from './api';
import { usePoll } from './hooks';
import { currencySymbol, formatMoney, type Bill, type FixedFee } from './lib';

const blankBill = { start: '', end: '', total: '', kwh: '', fees: [] as { label: string; amount: string }[] };

export function describeBill(b: Bill): string {
  const fees = (b.fixed_fees ?? []).length;
  return `${b.start} to ${b.end}: ${formatMoney(b.total)} for ${b.kwh} kWh${fees ? `, ${fees} flat fee${fees > 1 ? 's' : ''} left out` : ''}`;
}

export function BillsCard() {
  const [version, setVersion] = useState(0);
  const { data, error } = usePoll(api.bills, 0, [version]);
  const { data: rates } = usePoll(api.rates, 0, []);
  const [draft, setDraft] = useState(blankBill);
  const [removing, setRemoving] = useState<number>();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  if (!data) {
    return (
      <section class="card bills">
        <h2>Bills</h2>
        <p class="note">{error?.message || 'Loading…'}</p>
      </section>
    );
  }
  const bills = data.bills;

  const save = async (next: Bill[], after?: () => void) => {
    setBusy(true);
    setMessage('');
    try {
      await api.setBills(next);
      after?.();
      setVersion((v) => v + 1);
    } catch (e) {
      setMessage((e as Error).message);
    }
    setBusy(false);
  };

  const add = () => {
    const fees: FixedFee[] = draft.fees.filter((f) => f.label.trim() || f.amount.trim()).map((f) => ({ label: f.label.trim(), amount: f.amount.trim() }));
    const bill: Bill = { start: draft.start, end: draft.end, total: draft.total.trim(), kwh: Number(draft.kwh) };
    if (fees.length > 0) bill.fixed_fees = fees;
    void save([...bills, bill], () => setDraft(blankBill));
  };

  const field = (key: 'start' | 'end' | 'total' | 'kwh', label: string, type: 'text' | 'date' | 'number' = 'text') => (
    <label class="field">
      <span>{label}</span>
      <input
        type={type as 'text'}
        value={draft[key]}
        onInput={(e) => {
          const v = (e.target as HTMLInputElement).value;
          setDraft((d) => ({ ...d, [key]: v }));
        }}
      />
    </label>
  );

  return (
    <section class="card bills">
      <h2>Bills</h2>
      {bills.length === 0 ? (
        <p class="note">No bills added.</p>
      ) : (
        <ul class="settings">
          {bills.map((b, i) => (
            <li key={`${b.start}-${i}`}>
              <span class="grow">{describeBill(b)}</span>
              <button
                type="button"
                disabled={busy}
                onClick={() => (removing === i ? void save(bills.filter((_, j) => j !== i), () => setRemoving(undefined)) : setRemoving(i))}
                onBlur={() => setRemoving(undefined)}
              >
                {removing === i ? 'Remove?' : 'Remove'}
              </button>
            </li>
          ))}
        </ul>
      )}

      <fieldset class="bill-form">
        <legend>Add a bill</legend>
        {field('start', 'First day', 'date')}
        {field('end', 'Last day', 'date')}
        {field('total', `Total (${currencySymbol(rates?.currency)})`)}
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
          <button type="button" disabled={busy || !draft.start || !draft.end || !draft.total || !draft.kwh} onClick={add}>
            Add bill
          </button>
        </div>
      </fieldset>
      {message && <p class="note error" role="alert">{message}</p>}
    </section>
  );
}
