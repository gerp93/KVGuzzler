// What the price source does to the days that are over: nothing. A finished day keeps the price it was saved with, so a new source starts today. The only way to
// change them is to price a range again, which shows the difference first.
import { useState } from 'preact/hooks';
import { api } from './api';
import { usePoll } from './hooks';
import { formatMoney, type RepriceAnswer } from './lib';

const SOURCE_NAMES: Record<string, string> = { one: 'one rate', bills: 'bills', lookup: "the utility's published rate" };

function signed(m: string | null | undefined): string {
  if (m == null) return '—';
  const n = Number(m);
  return `${n > 0 ? '+' : ''}${formatMoney(m)}`;
}

export function PriceHistory({ mode }: { mode: string }) {
  const [version, setVersion] = useState(0);
  const { data } = usePoll(api.frozenPrices, 0, [version]);
  const [open, setOpen] = useState(false);
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [answer, setAnswer] = useState<RepriceAnswer>();
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  const ask = async (apply: boolean) => {
    setBusy(true);
    setMessage('');
    try {
      const r = await api.reprice({ from, ...(to ? { to } : {}), apply });
      setAnswer(r);
      setConfirm(false);
      if (apply) setVersion((v) => v + 1);
    } catch (e) {
      setAnswer(undefined);
      setMessage((e as Error).message);
    }
    setBusy(false);
  };

  return (
    <div class="price-history">
      <p class="note">
        {data?.through
          ? `A new source applies from today. Days through ${data.through} keep their price.`
          : 'A new source applies from today. Days that are over keep their price.'}
      </p>
      <details open={open} onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}>
        <summary>Price past days again</summary>
        <div class="bill-form">
          <label class="field">
            <span>First day</span>
            <input
              type="date"
              value={from}
              onInput={(e) => {
                setFrom((e.target as HTMLInputElement).value);
                setAnswer(undefined);
              }}
            />
          </label>
          <label class="field">
            <span>Last day (blank: yesterday)</span>
            <input
              type="date"
              value={to}
              onInput={(e) => {
                setTo((e.target as HTMLInputElement).value);
                setAnswer(undefined);
              }}
            />
          </label>
          <div class="chips">
            <button type="button" disabled={!from || busy} onClick={() => void ask(false)}>
              Show change
            </button>
          </div>
        </div>
        {message && <p class="note error" role="alert">{message}</p>}
        {answer && (
          <div class="reprice-answer" role="status">
            {answer.days === 0 ? (
              <p class="note">{`No day from ${answer.from} to ${answer.to} would change.`}</p>
            ) : (
              <>
                <p>
                  {`${answer.days} day${answer.days === 1 ? '' : 's'} from ${answer.from} to ${answer.to} would take their price from ${SOURCE_NAMES[answer.source] ?? answer.source}.`}
                </p>
                {answer.old_cost != null && answer.new_cost != null && (
                  <p>{`House cost ${formatMoney(answer.old_cost)} → ${formatMoney(answer.new_cost)} (${signed(answer.difference)}).`}</p>
                )}
              </>
            )}
            {answer.applied ? (
              <p class="note">Done.</p>
            ) : (
              answer.days > 0 && (
                <div class="chips">
                  <button type="button" disabled={busy} onClick={() => (confirm ? void ask(true) : setConfirm(true))} onBlur={() => setConfirm(false)}>
                    {confirm ? 'Apply?' : 'Apply'}
                  </button>
                </div>
              )
            )}
          </div>
        )}
        {mode === 'bills' && <p class="caption">Entering or correcting a bill prices the days it covers.</p>}
      </details>
    </div>
  );
}
