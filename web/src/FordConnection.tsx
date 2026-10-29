// The Ford connection of an electric vehicle, inside the EV card's settings: whether the odometer is being read from Ford and when, and the buttons that
// follow from that. Connecting (registering the application, its details, signing in at Ford) is the dialog of `FordSetup`; nothing is typed here.
import { useState } from 'preact/hooks';
import { api } from './api';
import { FordProbe } from './FordProbe';
import type { SetupStep } from './FordSetup';
import { MIN_CALL_GAP_S } from './fordWait';
import type { FordLink } from './lib';

/** What the connection is, in a word, and how the card should colour it. */
export function fordState(cfg: FordLink | undefined): { word: string; tone: 'ok' | 'warn' | 'off' } {
  if (!cfg || !cfg.configured) return { word: 'Not connected', tone: 'off' };
  if (cfg.needs_link) return { word: 'Needs linking again', tone: 'warn' };
  if (!cfg.linked) return { word: 'Not linked yet', tone: 'off' };
  return { word: 'Connected', tone: 'ok' };
}

export function FordConnection({
  cfg,
  reload,
  waitLeft,
  wait,
  onSetup,
}: {
  cfg: FordLink | undefined;
  reload: () => void;
  waitLeft: number;
  wait: (seconds: number) => void;
  onSetup: (step: SetupStep) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [confirmRemove, setConfirmRemove] = useState(false);

  const run = (work: () => Promise<unknown>, done?: () => string) => {
    setBusy(true);
    setMessage('');
    work().then(
      () => {
        setBusy(false);
        if (done) setMessage(done());
        reload();
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
        reload();
      },
    );
  };

  const readNow = () => {
    wait(MIN_CALL_GAP_S);
    run(() => api.readFord());
  };

  const remove = () =>
    run(
      () => api.removeFord(),
      () => {
        setConfirmRemove(false);
        return 'Disconnected. The readings already saved are kept.';
      },
    );

  const state = fordState(cfg);
  return (
    <div class="ford">
      <p class={`ford-state ${state.tone}`}>
        <span class="dot" aria-hidden="true" />
        <strong>{state.word}</strong>
        {cfg?.last_check && cfg.linked && <span class="caption"> · last read {new Date(cfg.last_check).toLocaleString()}</span>}
      </p>
      {cfg?.last_outcome && (cfg.configured || cfg.linked) && (
        <p class={`caption ${cfg.needs_link || !cfg.last_miles ? 'bad' : ''}`}>
          {cfg.last_outcome}
          {cfg.last_raw != null && cfg.last_miles != null
            ? ` Ford said ${cfg.last_raw.toLocaleString()} ${cfg.odometer_unit === 'km' ? 'km' : 'miles'} (${cfg.last_found ?? 'odometer'}), ${cfg.last_miles.toLocaleString()} mi.`
            : ''}
        </p>
      )}

      <div class="chips save">
        {!cfg?.configured ? (
          <button type="button" class="setup-primary" onClick={() => onSetup('register')}>
            Connect Ford
          </button>
        ) : (
          <>
            {cfg.linked && (
              <button type="button" disabled={busy || waitLeft > 0} onClick={readNow}>
                {waitLeft > 0 ? `Read now · wait ${waitLeft} s` : 'Read now'}
              </button>
            )}
            <button type="button" disabled={busy} onClick={() => onSetup('signin')}>
              {cfg.linked ? 'Link again' : 'Link Ford account'}
            </button>
            <button type="button" disabled={busy} onClick={() => onSetup('details')}>
              Application details
            </button>
            {!confirmRemove ? (
              <button type="button" disabled={busy} onClick={() => setConfirmRemove(true)}>
                Disconnect
              </button>
            ) : (
              <>
                <button type="button" disabled={busy} onClick={remove}>
                  Yes, disconnect
                </button>
                <button type="button" onClick={() => setConfirmRemove(false)}>
                  Cancel
                </button>
              </>
            )}
          </>
        )}
      </div>
      {message && (
        <p class="note" role="status">
          {message}
        </p>
      )}

      {cfg?.last_signals && cfg.last_signals.length > 0 && (
        <details class="ford-signals">
          <summary>What Ford sent ({cfg.last_signals.length} fields)</summary>
          <p class="caption">Every field of the last answer. Only the battery, charging, plug, range, energy and odometer fields show a value.</p>
          <table>
            <thead>
              <tr>
                <th scope="col">Field</th>
                <th scope="col">Kind</th>
                <th scope="col">Value</th>
              </tr>
            </thead>
            <tbody>
              {cfg.last_signals.map((s) => (
                <tr key={s.path}>
                  <td>
                    <code>{s.path}</code>
                  </td>
                  <td>{s.kind}</td>
                  <td>{s.value ?? ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </details>
      )}

      {cfg?.linked && <FordProbe waitLeft={waitLeft} wait={wait} minGap={MIN_CALL_GAP_S} reload={reload} />}
    </div>
  );
}
