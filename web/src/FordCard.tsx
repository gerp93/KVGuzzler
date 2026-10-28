// Reads the car's odometer from Ford so the readings on the charging meter keep themselves up to date. The Ford developer application (client ID and
// secret) is the owner's own and is typed here once; the secret is write-only and kept in a private file on the server. The owner's Ford password is
// never typed here: signing in and agreeing happen on Ford's own page, and only the address Ford sends the browser to afterwards is pasted back.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { Hint } from './Hint';
import type { FordLink, Meter } from './lib';

export function FordCard({ meter, onRead }: { meter: Meter; onRead: () => void }) {
  const [cfg, setCfg] = useState<FordLink>();
  const [clientId, setClientId] = useState('');
  const [secret, setSecret] = useState('');
  const [authorizeUrl, setAuthorizeUrl] = useState('');
  const [tokenUrl, setTokenUrl] = useState('');
  const [unit, setUnit] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [link, setLink] = useState<{ url: string; redirect_uri: string }>();
  const [pasted, setPasted] = useState('');

  const load = (c: FordLink) => {
    setCfg(c);
    if (c.configured) {
      setClientId(c.client_id ?? '');
      setAuthorizeUrl(c.authorize_url ?? '');
      setTokenUrl(c.token_url ?? '');
      setUnit(c.odometer_unit ?? '');
    }
  };
  useEffect(() => {
    api.ford().then(load, (e: Error) => setMessage(e.message));
  }, []);

  const run = (work: () => Promise<FordLink | void>, done?: (c: FordLink | void) => string) => {
    setBusy(true);
    setMessage('');
    work().then(
      (c) => {
        setBusy(false);
        if (c) load(c);
        if (done) setMessage(done(c));
        onRead();
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
      },
    );
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

  const finish = () =>
    run(
      () => api.completeFordLink(pasted.trim()),
      (c) => {
        setLink(undefined);
        setPasted('');
        return c && c.last_outcome ? c.last_outcome : 'Linked.';
      },
    );

  const readNow = () => run(() => api.readFord());
  const remove = () =>
    run(
      () => api.removeFord().then(() => api.ford()),
      () => {
        setSecret('');
        setLink(undefined);
        return 'Removed. The readings already saved are kept.';
      },
    );

  const elsewhere = cfg?.configured && cfg.meter_id && cfg.meter_id !== meter.id;
  const ready = clientId.trim() && authorizeUrl.trim() && tokenUrl.trim() && unit && (secret.trim() || cfg?.configured);
  return (
    <details class="ford" open={!cfg?.configured || !cfg.linked}>
      <summary>Read the odometer from Ford{cfg?.linked ? ' (linked)' : ''}</summary>
      <Hint>
        Once a day or more, the server asks Ford for the vehicle's odometer and saves it as a reading. It needs a personal application registered at
        developer.ford.com (its client ID and secret are typed here once). Signing in and agreeing to share the vehicle happen on Ford's own page: no
        Ford password is typed here or kept.
      </Hint>
      {elsewhere && <p class="note">The odometer of another meter is being read; saving here moves it to {meter.label}.</p>}
      <label class="field">
        <span>Client ID</span>
        <input value={clientId} autocomplete="off" onInput={(e) => setClientId((e.target as HTMLInputElement).value)} />
      </label>
      <label class="field">
        <span>Client secret {cfg?.configured ? '(leave empty to keep the saved one)' : ''}</span>
        <input type="password" autocomplete="off" value={secret} onInput={(e) => setSecret((e.target as HTMLInputElement).value)} />
      </label>
      <label class="field">
        <span>Account-linking URL (from the developer dashboard: View oAuth URLs)</span>
        <input value={authorizeUrl} autocomplete="off" onInput={(e) => setAuthorizeUrl((e.target as HTMLInputElement).value)} />
      </label>
      <label class="field">
        <span>Token URL (the code exchange request of Ford's Postman collection)</span>
        <input value={tokenUrl} autocomplete="off" placeholder="https://api.vehicle.ford.com/dah2vb2cprod.onmicrosoft.com/oauth2/v2.0/token?p=B2C_1A_FCON_AUTHORIZE" onInput={(e) => setTokenUrl((e.target as HTMLInputElement).value)} />
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
        {cfg?.configured && (
          <button type="button" disabled={busy} onClick={start}>
            {cfg.linked ? 'Link again' : 'Link Ford account'}
          </button>
        )}
        {cfg?.linked && (
          <button type="button" disabled={busy} onClick={readNow}>
            Read now
          </button>
        )}
        {cfg?.configured && (
          <button type="button" disabled={busy} onClick={remove}>
            Remove
          </button>
        )}
      </div>
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
            <button type="button" disabled={busy || !pasted.trim()} onClick={finish}>
              Finish linking
            </button>
          </div>
        </div>
      )}
      {message && <p class="note" role="status">{message}</p>}
      {cfg?.needs_link && <p class="note error">Ford no longer shares this vehicle: link the account again.</p>}
      {cfg?.last_outcome && (
        <p class="caption">
          Last read{cfg.last_check ? ` ${new Date(cfg.last_check).toLocaleString()}` : ''}: {cfg.last_outcome}
          {cfg.last_raw != null && cfg.last_miles != null
            ? ` Ford said ${cfg.last_raw.toLocaleString()} ${cfg.odometer_unit === 'km' ? 'km' : 'miles'} (${cfg.last_found ?? 'odometer'}), ${cfg.last_miles.toLocaleString()} mi.`
            : ''}
        </p>
      )}
    </details>
  );
}
