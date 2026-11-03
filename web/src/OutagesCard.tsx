// The times nothing in the house was recorded, and why: a power outage (the server found it from when its plugs started again, or it was typed in), time the
// power was on and nothing was recording, or a stretch whose cause could not be told. Only a log: it changes no figure. Entries can be added and corrected by hand,
// for the times another source (the Ting app, the utility) knows better.
import { useState } from 'preact/hooks';
import { api } from './api';
import { Facts, type Fact } from './Facts';
import { usePoll } from './hooks';
import { formatDuration } from './lib';
import { fromLocalInput, kindLabel, kindMeaning, OUTAGE_KINDS, sourceLabel, toLocalInput, whenText, type Outage, type OutageKind } from './outages';

/** How many outages are listed before "Show all". */
const SHOWN = 8;

interface Draft {
  /** The outage being corrected; undefined for a new one. */
  id?: number;
  kind: OutageKind;
  start: string;
  end: string;
  note: string;
}

const blank = (): Draft => ({ kind: 'power', start: '', end: '', note: '' });
const draftOf = (o: Outage): Draft => ({
  id: o.id,
  kind: o.kind,
  start: toLocalInput(o.started_at),
  end: toLocalInput(o.ended_at),
  note: o.note,
});

export function OutagesCard() {
  const [version, setVersion] = useState(0);
  const { data, error } = usePoll(api.outages, 5 * 60_000, [version]);
  const [all, setAll] = useState(false);
  const [draft, setDraft] = useState<Draft | undefined>(undefined);
  const [removing, setRemoving] = useState<number | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  const edit = (patch: Partial<Draft>) => setDraft((d) => (d ? { ...d, ...patch } : d));

  const run = async (fn: () => Promise<unknown>, after?: () => void) => {
    setBusy(true);
    setMessage('');
    try {
      await fn();
      after?.();
      setVersion((v) => v + 1);
    } catch (e) {
      setMessage((e as Error).message);
    }
    setBusy(false);
  };

  const save = () => {
    if (!draft) return;
    const start = fromLocalInput(draft.start);
    const end = fromLocalInput(draft.end);
    if (!start || !end) {
      setMessage('Both the start and the end need a date and a time.');
      return;
    }
    const original = draft.id != null ? data?.outages.find((o) => o.id === draft.id) : undefined;
    // A time left as it was is not sent again: the list shows minutes, the saved time may have seconds.
    const changed = (now: string, was: string | undefined, iso: string) => (original && was != null && toLocalInput(was) === now ? undefined : iso);
    void run(
      () =>
        draft.id == null
          ? api.addOutage({
              kind: draft.kind,
              started_at: start,
              ended_at: end,
              note: draft.note,
            })
          : api.updateOutage(draft.id, {
              kind: draft.kind,
              started_at: changed(draft.start, original?.started_at, start),
              ended_at: changed(draft.end, original?.ended_at, end),
              note: draft.note,
            }),
      () => setDraft(undefined),
    );
  };

  if (!data) {
    return (
      <section class="card">
        <h2>Outages</h2>
        <p class="note">{error?.message || 'Loading…'}</p>
      </section>
    );
  }

  const list = all ? data.outages : data.outages.slice(0, SHOWN);
  const totals: Fact[] = [
    ...(data.power_seconds > 0 ? [{ label: 'Power out', value: formatDuration(data.power_seconds) }] : []),
    ...(data.server_seconds > 0 ? [{ label: 'Server off', value: formatDuration(data.server_seconds) }] : []),
    ...(data.unknown_seconds > 0 ? [{ label: 'Not recorded', value: formatDuration(data.unknown_seconds) }] : []),
  ];

  return (
    <section class="card outages">
      <h2>Outages</h2>
      {data.outages.length === 0 ? <p class="note">No outages recorded.</p> : <Facts items={totals} />}
      {list.length > 0 && (
        <div class="ev-table-wrap">
          <table class="ev-table outage-table">
            <caption class="visually-hidden">Times nothing was recorded, newest first</caption>
            <thead>
              <tr>
                <th scope="col">When</th>
                <th scope="col">Length</th>
                <th scope="col">What</th>
              </tr>
            </thead>
            <tbody>
              {list.map((o) => (
                <tr key={o.id}>
                  <td>{whenText(o)}</td>
                  <td>{formatDuration(o.seconds)}</td>
                  <td class="outage-what" title={kindMeaning(o.kind)}>
                    <span class={`outage-kind ${o.kind}`}>{kindLabel(o.kind)}</span>
                    <span class="outage-note">
                      {(o.source === 'manual' ? [sourceLabel(o.source), o.note] : [o.note || sourceLabel(o.source)]).filter(Boolean).join(' · ')}
                    </span>
                    <span class="outage-actions">
                      <button type="button" disabled={busy} onClick={() => setDraft(draftOf(o))}>
                        Edit
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() =>
                          removing === o.id
                            ? void run(
                                () => api.removeOutage(o.id),
                                () => setRemoving(undefined),
                              )
                            : setRemoving(o.id)
                        }
                        onBlur={() => setRemoving(undefined)}
                      >
                        {removing === o.id ? 'Remove?' : 'Remove'}
                      </button>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {data.outages.length > SHOWN && (
        <button type="button" class="ev-more" onClick={() => setAll(!all)}>
          {all ? 'Show fewer' : `Show all ${data.outages.length}`}
        </button>
      )}

      {draft ? (
        <form
          class="outage-form"
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <label>
            Kind
            <select value={draft.kind} onChange={(e) => edit({ kind: (e.target as HTMLSelectElement).value as OutageKind })}>
              {OUTAGE_KINDS.map((k) => (
                <option key={k} value={k}>
                  {kindLabel(k)}
                </option>
              ))}
            </select>
          </label>
          <label>
            Start
            <input type="datetime-local" value={draft.start} onInput={(e) => edit({ start: (e.target as HTMLInputElement).value })} />
          </label>
          <label>
            End
            <input type="datetime-local" value={draft.end} onInput={(e) => edit({ end: (e.target as HTMLInputElement).value })} />
          </label>
          <label>
            Note
            <input type="text" maxLength={200} value={draft.note} onInput={(e) => edit({ note: (e.target as HTMLInputElement).value })} />
          </label>
          <div class="chips">
            <button type="submit" disabled={busy || !draft.start || !draft.end}>
              Save
            </button>
            <button type="button" disabled={busy} onClick={() => setDraft(undefined)}>
              Cancel
            </button>
          </div>
        </form>
      ) : (
        <div class="chips">
          <button type="button" disabled={busy} onClick={() => setDraft(blank())}>
            Add outage
          </button>
        </div>
      )}
      {message && <p class="note warn">{message}</p>}
    </section>
  );
}
