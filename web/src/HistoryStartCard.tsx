// The day history starts on: nothing recorded before it is looked at by any period, not even "all time", and that day counts as the start of history.
// Nothing is deleted, so a different day or taking it off brings the older data back.
import { useState } from 'preact/hooks';
import { api } from './api';
import { Facts } from './Facts';
import { usePoll } from './hooks';

/** Today as a date for the picker's upper limit, in the viewer's own zone. */
function todayText(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

export function HistoryStartCard() {
  const { data, error } = usePoll(api.historyStart, 0, []);
  const [draft, setDraft] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  if (!data) return <section class="card page-narrow"><h2>History starts</h2><p class="note">{error?.message || 'Loading…'}</p></section>;

  const day = draft ?? data.day ?? '';
  // Every page shows figures for periods read a moment ago: reading them all again is a reload.
  const done = (fn: () => Promise<unknown>) => async () => {
    setBusy(true);
    setMessage('');
    try {
      await fn();
      window.location.reload();
    } catch (e) {
      setMessage((e as Error).message);
      setBusy(false);
    }
  };

  return (
    <section class="card page-narrow">
      <h2>History starts</h2>
      <label>
        First day
        <input type="date" value={day} max={todayText()} onInput={(e) => setDraft((e.target as HTMLInputElement).value)} />
      </label>
      <div class="chips">
        <button type="button" disabled={busy || !day || day === data.day} onClick={done(() => api.putHistoryStart(day))}>
          Save
        </button>
        {data.day && (
          <button type="button" disabled={busy} onClick={done(api.deleteHistoryStart)}>
            Remove
          </button>
        )}
      </div>
      {message && <p class="note warn">{message}</p>}
      {data.recorded_since && <Facts items={[{ label: 'Recorded since', value: new Date(data.recorded_since).toLocaleDateString() }]} />}
    </section>
  );
}
