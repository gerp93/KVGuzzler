// The electricity bills. They are kept whatever the price comes from: when it comes from the bills they set the price of the days they cover, and either way
// they are what the recorded energy is checked against.
import { useState } from 'preact/hooks';
import { api } from './api';
import { usePoll } from './hooks';
import { currencySymbol, formatKwh, formatMoney, type Bill, type FixedFee } from './lib';
import { hints, shareText, verdictLabel, type BillCheck } from './reconcile';

const blankBill = { start: '', end: '', total: '', kwh: '', fees: [] as { label: string; amount: string }[] };

export function describeBill(b: Bill): string {
  const fees = (b.fixed_fees ?? []).length;
  return `${b.start} to ${b.end}: ${formatMoney(b.total)} for ${b.kwh} kWh${fees ? `, ${fees} flat fee${fees > 1 ? 's' : ''} left out` : ''}`;
}

function signedMoney(m: string | null | undefined): string {
  if (m == null) return '—';
  return `${Number(m) > 0 ? '+' : ''}${formatMoney(m)}`;
}

/** One bill's check against the recording: the energy billed and recorded, how far apart, and what is worth knowing about the difference. */
function Check({ c, priceFromBills }: { c: BillCheck; priceFromBills: boolean }) {
  const notes = hints(c);
  return (
    <div class="recon">
      {c.recorded_kwh != null && (
        <p class="recon-line">
          <span>{`Billed ${formatKwh(c.billed_kwh)}`}</span>
          <span>{`Recorded ${formatKwh(c.recorded_kwh)}`}</span>
          {c.diff_share != null && <span>{`${shareText(c.diff_share)}`}</span>}
          {c.verdict && <span class={`recon-verdict ${c.verdict}`}>{verdictLabel(c.verdict)}</span>}
        </p>
      )}
      {c.recorded_cost != null && c.diff_cost != null && !priceFromBills && (
        <p class="recon-line">
          <span>{`Energy charge ${formatMoney(c.billed_variable)}`}</span>
          <span>{`Recorded at the price used ${formatMoney(c.recorded_cost)}`}</span>
          <span>{signedMoney(c.diff_cost)}</span>
        </p>
      )}
      {notes.map((n) => (
        <p class="caption" key={n}>
          {n}
        </p>
      ))}
    </div>
  );
}

export function BillsCard() {
  const [version, setVersion] = useState(0);
  const { data, error } = usePoll(api.bills, 0, [version]);
  const { data: rates } = usePoll(api.rates, 0, []);
  const { data: recon } = usePoll(api.billReconciliation, 0, [version]);
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
      {recon?.typical_share != null && (
        <p class="note">{`Across ${recon.typical_bills} fully recorded bills the house recorded ${(recon.typical_share * 100).toFixed(1)}% of the energy billed.`}</p>
      )}
      {recon && !recon.have_house && bills.length > 0 && <p class="note">Bills are checked against a whole-house meter. None is set up.</p>}
      {bills.length === 0 ? (
        <p class="note">No bills added.</p>
      ) : (
        <ul class="settings bill-list">
          {bills
            .map((b, i) => ({ b, i }))
            .reverse()
            .map(({ b, i }) => {
              const check = recon?.bills.find((c) => c.start === b.start && c.end === b.end);
              return (
            <li key={`${b.start}-${i}`}>
              <span class="grow">
                {describeBill(b)}
                {check && <Check c={check} priceFromBills={recon?.price_source === 'bills'} />}
              </span>
              <button
                type="button"
                disabled={busy}
                onClick={() => (removing === i ? void save(bills.filter((_, j) => j !== i), () => setRemoving(undefined)) : setRemoving(i))}
                onBlur={() => setRemoving(undefined)}
              >
                {removing === i ? 'Remove?' : 'Remove'}
              </button>
            </li>
              );
            })}
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
