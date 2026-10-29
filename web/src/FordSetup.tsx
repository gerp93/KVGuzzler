// Connecting a vehicle to Ford, one step at a time in a dialog: register an application at Ford, enter its details, sign in at Ford's own page, paste
// back the address Ford sends the browser to. The client ID and secret are the owner's own; the secret is write-only and kept in a private file on the
// server. The owner's Ford password is never typed here: signing in and agreeing happen on Ford's page.
import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from './api';
import { MIN_CALL_GAP_S } from './fordWait';
import type { FordLink, Meter } from './lib';

export type SetupStep = 'register' | 'details' | 'signin' | 'paste';

/** What was typed in the dialog and not finished with, kept by the card so closing the dialog by accident (Escape, a stray click) loses nothing. */
export type FordDraft = Partial<{ clientId: string; secret: string; authorizeUrl: string; tokenUrl: string; unit: string; pasted: string }>;

/** What Ford's quick-start guide gives as the redirect address to register: nothing listens there, the owner copies the address from the browser. */
export const REDIRECT_URI = 'http://localhost:8000/callback';
/** Where the registered application's code is traded for tokens: the same for every owner (Ford's Postman collection). */
const TOKEN_URL = 'https://api.vehicle.ford.com/dah2vb2cprod.onmicrosoft.com/oauth2/v2.0/token?p=B2C_1A_FCON_AUTHORIZE';
const DEVELOPER_SITE = 'https://developer.ford.com/developer-na';

const STEPS: { id: SetupStep; label: string }[] = [
  { id: 'register', label: 'Register' },
  { id: 'details', label: 'Details' },
  { id: 'signin', label: 'Sign in' },
  { id: 'paste', label: 'Finish' },
];

export function FordSetup({
  meter,
  cfg,
  startAt,
  draft,
  waitLeft,
  wait,
  reload,
  onClose,
}: {
  meter: Meter;
  cfg: FordLink | undefined;
  startAt: SetupStep;
  draft: { current: FordDraft };
  waitLeft: number;
  wait: (seconds: number) => void;
  reload: () => void;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [step, setStep] = useState<SetupStep>(startAt);
  const [done, setDone] = useState<FordLink>();
  const kept = draft.current;
  const [clientId, setClientIdNow] = useState(kept.clientId ?? cfg?.client_id ?? '');
  const [secret, setSecretNow] = useState(kept.secret ?? '');
  const [authorizeUrl, setAuthorizeUrlNow] = useState(kept.authorizeUrl ?? cfg?.authorize_url ?? '');
  const [tokenUrl, setTokenUrlNow] = useState(kept.tokenUrl ?? (cfg?.token_url || TOKEN_URL));
  const [unit, setUnitNow] = useState<string>(kept.unit ?? cfg?.odometer_unit ?? '');
  const [link, setLink] = useState<{ url: string; redirect_uri: string }>();
  const [pasted, setPastedNow] = useState(kept.pasted ?? '');
  // Every field is written to the card's draft as it changes.
  const setClientId = (v: string) => { draft.current.clientId = v; setClientIdNow(v); };
  const setSecret = (v: string) => { draft.current.secret = v; setSecretNow(v); };
  const setAuthorizeUrl = (v: string) => { draft.current.authorizeUrl = v; setAuthorizeUrlNow(v); };
  const setTokenUrl = (v: string) => { draft.current.tokenUrl = v; setTokenUrlNow(v); };
  const setUnit = (v: string) => { draft.current.unit = v; setUnitNow(v); };
  const setPasted = (v: string) => { draft.current.pasted = v; setPastedNow(v); };
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    const d = dialog.current;
    if (d && !d.open) d.showModal();
    return () => {
      if (d?.open) d.close();
    };
  }, []);

  // The sign-in address is made once the step that shows it is reached, and kept while moving back and forth (a new one would void the old one).
  useEffect(() => {
    if (step !== 'signin' || link) return;
    setMessage('');
    api.startFordLink().then(setLink, (e: Error) => setMessage(e.message));
  }, [step, link]);

  const elsewhere = cfg?.configured && cfg.meter_id && cfg.meter_id !== meter.id;
  const ready = clientId.trim() && authorizeUrl.trim() && tokenUrl.trim() && unit && (secret.trim() || cfg?.configured);

  const go = (s: SetupStep) => {
    setMessage('');
    setStep(s);
  };

  const save = () => {
    setBusy(true);
    setMessage('');
    api
      .setFord({
        client_id: clientId.trim(),
        client_secret: secret.trim() || undefined,
        authorize_url: authorizeUrl.trim(),
        token_url: tokenUrl.trim(),
        odometer_unit: unit as 'km' | 'miles',
        meter_id: meter.id,
      })
      .then(
        () => {
          setBusy(false);
          // Saved: the secret (and the details) are the server's now, nothing is kept here.
          draft.current = { pasted: draft.current.pasted };
          setSecretNow('');
          setLink(undefined);
          reload();
          go('signin');
        },
        (e: Error) => {
          setBusy(false);
          setMessage(e.message);
        },
      );
  };

  const finish = () => {
    wait(MIN_CALL_GAP_S);
    setBusy(true);
    setMessage('');
    api.completeFordLink(pasted.trim()).then(
      (v) => {
        setBusy(false);
        setDone(v);
        setLink(undefined);
        draft.current = {};
        reload();
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
        reload();
      },
    );
  };

  const copy = () => {
    navigator.clipboard?.writeText(REDIRECT_URI).then(
      () => {
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      },
      () => undefined,
    );
  };

  const at = STEPS.findIndex((s) => s.id === step);
  return (
    <dialog
      ref={dialog}
      class="ford-setup"
      aria-labelledby="ford-setup-title"
      onClose={onClose}
    >
      <header class="setup-head">
        <h2 id="ford-setup-title">Connect Ford</h2>
        <button type="button" class="icon" aria-label="Close" onClick={() => dialog.current?.close()}>
          ✕
        </button>
      </header>

      {!done && (
        <ol class="setup-steps">
          {STEPS.map((s, i) => (
            <li key={s.id} class={i < at ? 'past' : i === at ? 'now' : ''} aria-current={i === at ? 'step' : undefined}>
              <span class="n">{i < at ? '✓' : i + 1}</span>
              <span class="label">{s.label}</span>
            </li>
          ))}
        </ol>
      )}

      <div class="setup-body">
        {done ? (
          <>
            <h3>Connected</h3>
            <p>
              The vehicle is linked.
              {done.last_raw != null && done.last_miles != null && (
                <>
                  {' '}
                  Ford sent {done.last_raw.toLocaleString()} {done.odometer_unit === 'km' ? 'km' : 'miles'} for the odometer, which is{' '}
                  {done.last_miles.toLocaleString()} mi.
                </>
              )}
            </p>
            <p class="caption">
              Next, open Check all Ford data in the settings to see whether the battery and charging fields come through. Ford allows one look every 30
              seconds, so it is ready in a moment.
            </p>
          </>
        ) : step === 'register' ? (
          <>
            <h3>Register an application at Ford</h3>
            <p>Ford shares a vehicle's data only with an application its owner registers. It takes a few minutes and is done once.</p>
            <ol class="setup-list">
              <li>
                <a href={DEVELOPER_SITE} target="_blank" rel="noopener noreferrer">
                  Open Ford's developer site
                </a>{' '}
                (US and Canada), sign in, and register an application for personal use.
              </li>
              <li>
                Set the redirect address to <code>{REDIRECT_URI}</code>{' '}
                <button type="button" class="small" onClick={copy}>
                  {copied ? 'Copied' : 'Copy'}
                </button>
              </li>
              <li>
                Tick <strong>every scope</strong>, scrolling the whole list: <code>evData</code> (battery and charging) and <code>chargingData</code> matter most.
                Scopes cannot be changed afterwards.
              </li>
              <li>Copy the client ID and the client secret when Ford shows them. The secret is shown once.</li>
            </ol>
          </>
        ) : step === 'details' ? (
          <>
            <h3>Enter the application's details</h3>
            {elsewhere && <p class="note">The odometer of another meter is being read; saving here moves it to {meter.label}.</p>}
            <label class="field">
              <span>Client ID</span>
              <input value={clientId} autocomplete="off" onInput={(e) => setClientId((e.target as HTMLInputElement).value)} />
            </label>
            <label class="field">
              <span>Client secret{cfg?.configured ? ' (leave empty to keep the saved one)' : ''}</span>
              <input type="password" autocomplete="off" value={secret} onInput={(e) => setSecret((e.target as HTMLInputElement).value)} />
            </label>
            <label class="field">
              <span>Account-linking URL</span>
              <input value={authorizeUrl} autocomplete="off" onInput={(e) => setAuthorizeUrl((e.target as HTMLInputElement).value)} />
              <small>On Ford's dashboard, open the application and choose View oAuth URLs, then copy the one for the redirect address above.</small>
            </label>
            <div class="field">
              <span>Unit of the odometer in Ford's answer</span>
              <div class="chips" role="group" aria-label="Unit of the odometer in Ford's answer">
                <button type="button" aria-pressed={unit === 'km'} onClick={() => setUnit('km')}>
                  Kilometers
                </button>
                <button type="button" aria-pressed={unit === 'miles'} onClick={() => setUnit('miles')}>
                  Miles
                </button>
              </div>
              <small>Ford sent kilometers for the Mach-E this was tested with. The first read shows what Ford sent, so a wrong choice is easy to spot and change.</small>
            </div>
            <details class="setup-advanced">
              <summary>Advanced</summary>
              <label class="field">
                <span>Token URL</span>
                <input value={tokenUrl} autocomplete="off" onInput={(e) => setTokenUrl((e.target as HTMLInputElement).value)} />
                <small>The same for everyone; it only changes if Ford moves it.</small>
              </label>
            </details>
          </>
        ) : step === 'signin' ? (
          <>
            <h3>Sign in at Ford</h3>
            <p>Ford's own page asks you to sign in, pick the vehicle and agree to share its data. Your Ford password is never typed here.</p>
            {cfg?.linked && <p class="note">This vehicle is already linked. Continue only if the application changed or Ford asks to agree again.</p>}
            {link ? (
              <a class="setup-open" href={link.url} target="_blank" rel="noopener noreferrer">
                Open Ford's sign-in page
              </a>
            ) : (
              !message && <p class="caption">Making the sign-in address…</p>
            )}
            <p class="caption">
              After you agree, the browser goes to <code>{REDIRECT_URI}</code>, which shows an empty page or an error. That is expected: leave it open and come
              back here.
            </p>
          </>
        ) : (
          <>
            <h3>Paste the address</h3>
            <p>
              Copy the whole address from the address bar of the page Ford sent the browser to. It starts with <code>{link?.redirect_uri ?? REDIRECT_URI}</code>{' '}
              and has <code>?code=…&amp;state=…</code> after it. It stays good for 10 minutes.
            </p>
            <label class="field">
              <span>Address the browser went to</span>
              <input value={pasted} autocomplete="off" placeholder={`${REDIRECT_URI}?code=…&state=…`} onInput={(e) => setPasted((e.target as HTMLInputElement).value)} />
            </label>
          </>
        )}
        {message && (
          <p class="note error" role="status">
            {message}
          </p>
        )}
      </div>

      <footer class="setup-foot">
        {done ? (
          <button type="button" class="setup-primary" onClick={() => dialog.current?.close()}>
            Close
          </button>
        ) : (
          <>
            {step !== 'register' && (
              <button type="button" disabled={busy} onClick={() => go(STEPS[Math.max(0, at - 1)]?.id ?? 'register')}>
                Back
              </button>
            )}
            <span class="spacer" />
            {step === 'register' && (
              <button type="button" class="setup-primary" onClick={() => go('details')}>
                I have the client ID and secret
              </button>
            )}
            {step === 'details' && (
              <button type="button" class="setup-primary" disabled={busy || !ready} onClick={save}>
                {busy ? 'Saving…' : 'Save and continue'}
              </button>
            )}
            {step === 'signin' && (
              <button type="button" class="setup-primary" disabled={!link} onClick={() => go('paste')}>
                Continue
              </button>
            )}
            {step === 'paste' && (
              <button type="button" class="setup-primary" disabled={busy || !pasted.trim() || waitLeft > 0} onClick={finish}>
                {busy ? 'Linking…' : waitLeft > 0 ? `Finish linking · wait ${waitLeft} s` : 'Finish linking'}
              </button>
            )}
          </>
        )}
      </footer>
    </dialog>
  );
}
