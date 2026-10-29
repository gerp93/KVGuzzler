// The Ford connection of an electric vehicle, inside the EV card's settings: whether the odometer is being read from Ford and when, the buttons that
// follow from that, and (set once, rarely touched) the developer application it is read through. The client ID and secret are the owner's own and are
// typed here once; the secret is write-only and kept in a private file on the server. The owner's Ford password is never typed here: signing in and
// agreeing happen on Ford's own page, and only the address Ford sends the browser to afterwards is pasted back.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { FordProbe } from './FordProbe';
import type { FordLink, Meter } from './lib';

/** Ford answers one look every 30 seconds; the server refuses one inside that, and the buttons that make a look wait it out. */
const MIN_CALL_GAP_S = 30;

/** What the connection is, in a word, and how the card should colour it. */
export function fordState(cfg: FordLink | undefined): { word: string; tone: 'ok' | 'warn' | 'off' } {
  if (!cfg || !cfg.configured) return { word: 'Not connected', tone: 'off' };
  if (cfg.needs_link) return { word: 'Needs linking again', tone: 'warn' };
  if (!cfg.linked) return { word: 'Not linked yet', tone: 'off' };
  return { word: 'Connected', tone: 'ok' };
}

export function FordConnection({ meter, cfg, reload }: { meter: Meter; cfg: FordLink | undefined; reload: () => void }) {
  const [clientId, setClientId] = useState(cfg?.client_id ?? '');
  const [secret, setSecret] = useState('');
  const [authorizeUrl, setAuthorizeUrl] = useState(cfg?.authorize_url ?? '');
  const [tokenUrl, setTokenUrl] = useState(cfg?.token_url ?? '');
  const [unit, setUnit] = useState(cfg?.odometer_unit ?? '');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [link, setLink] = useState<{ url: string; redirect_uri: string }>();
  const [pasted, setPasted] = useState('');
  const [confirmRemove, setConfirmRemove] = useState(false);
  // When Ford may be asked again (a millisecond time) and the clock that counts down to it: the server says how long is left, the page only counts.
  const [until, setUntil] = useState(0);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const left = cfg?.read_wait_seconds ?? 0;
    if (left > 0) setUntil(Date.now() + left * 1000);
  }, [cfg]);
  useEffect(() => {
    if (until <= Date.now()) return;
    const id = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(id);
  }, [until]);
  const waitLeft = Math.max(0, Math.ceil((until - now) / 1000));
  const wait = (seconds: number) => {
    setNow(Date.now());
    setUntil(Date.now() + seconds * 1000);
  };

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

  const save = () =>
    run(
      () =>
        api.setFord({
          client_id: clientId.trim(),
          client_secret: secret.trim() || undefined,
          authorize_url: authorizeUrl.trim(),
          token_url: tokenUrl.trim(),
          odometer_unit: unit as 'km' | 'miles',
          meter_id: meter.id,
        }),
      () => {
        setSecret('');
        return 'Saved.';
      },
    );

  const start = () => {
    setBusy(true);
    setMessage('');
    api.startFordLink().then(
      (l) => {
        setBusy(false);
        setLink(l);
        setPasted('');
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
      },
    );
  };

  const finish = () => {
    wait(MIN_CALL_GAP_S);
    run(
      () => api.completeFordLink(pasted.trim()),
      () => {
        setLink(undefined);
        setPasted('');
        return 'Linked.';
      },
    );
  };

  const remove = () =>
    run(
      () => api.removeFord(),
      () => {
        setSecret('');
        setLink(undefined);
        setConfirmRemove(false);
        return 'Disconnected. The readings already saved are kept.';
      },
    );

  const state = fordState(cfg);
  const elsewhere = cfg?.configured && cfg.meter_id && cfg.meter_id !== meter.id;
  const ready = clientId.trim() && authorizeUrl.trim() && tokenUrl.trim() && unit && (secret.trim() || cfg?.configured);
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
      {elsewhere && <p class="note">The odometer of another meter is being read; saving here moves it to {meter.label}.</p>}

      {cfg?.configured && (
        <div class="chips save">
          {cfg.linked && (
            <button type="button" disabled={busy || waitLeft > 0} onClick={readNow}>
              {waitLeft > 0 ? `Read now · wait ${waitLeft} s` : 'Read now'}
            </button>
          )}
          <button type="button" disabled={busy} onClick={start}>
            {cfg.linked ? 'Link again' : 'Link Ford account'}
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
        </div>
      )}

      {link && (
        <div class="ford-link">
          <ol>
            <li>
              <a href={link.url} target="_blank" rel="noopener noreferrer">
                Open Ford's sign-in page
              </a>
              , sign in, pick the vehicle and agree.
            </li>
            <li>
              The browser then goes to <code>{link.redirect_uri}</code>, which may show an empty page or an error: that is expected. Copy the whole address
              from the address bar and paste it here within 10 minutes.
            </li>
          </ol>
          <label class="field">
            <span>Address the browser went to</span>
            <input value={pasted} autocomplete="off" placeholder={`${link.redirect_uri}?code=…&state=…`} onInput={(e) => setPasted((e.target as HTMLInputElement).value)} />
          </label>
          <div class="chips save">
            <button type="button" disabled={busy || !pasted.trim() || waitLeft > 0} onClick={finish}>
              {waitLeft > 0 ? `Finish linking · wait ${waitLeft} s` : 'Finish linking'}
            </button>
          </div>
        </div>
      )}
      {message && <p class="note" role="status">{message}</p>}

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

      <details class="ford-app" open={!cfg?.configured}>
        <summary>Developer application</summary>
        <label class="field">
          <span>Client ID</span>
          <input value={clientId} autocomplete="off" onInput={(e) => setClientId((e.target as HTMLInputElement).value)} />
        </label>
        <label class="field">
          <span>Client secret {cfg?.configured ? '(leave empty to keep the saved one)' : ''}</span>
          <input type="password" autocomplete="off" value={secret} onInput={(e) => setSecret((e.target as HTMLInputElement).value)} />
        </label>
        <label class="field">
          <span>Account-linking URL (developer dashboard: View oAuth URLs)</span>
          <input value={authorizeUrl} autocomplete="off" onInput={(e) => setAuthorizeUrl((e.target as HTMLInputElement).value)} />
        </label>
        <label class="field">
          <span>Token URL (the code exchange request of Ford's Postman collection)</span>
          <input
            value={tokenUrl}
            autocomplete="off"
            placeholder="https://api.vehicle.ford.com/dah2vb2cprod.onmicrosoft.com/oauth2/v2.0/token?p=B2C_1A_FCON_AUTHORIZE"
            onInput={(e) => setTokenUrl((e.target as HTMLInputElement).value)}
          />
        </label>
        <label class="field">
          <span>Unit of the odometer in Ford's response</span>
          <select value={unit} onChange={(e) => setUnit((e.target as HTMLSelectElement).value)}>
            <option value="">Choose (compare Ford's number with the car's display)</option>
            <option value="km">Kilometers</option>
            <option value="miles">Miles</option>
          </select>
        </label>
        <div class="chips save">
          <button type="button" disabled={busy || !ready} onClick={save}>
            Save
          </button>
        </div>
      </details>
    </div>
  );
}
