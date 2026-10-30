// The OpenEI connection, in the Connectors tab: the free key for the Utility Rate Database that "a utility's published rate" prices from. The key is write-only: it is
// saved in a private file on the server, never shown again, and sent to api.openei.org only.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { Hint } from './Hint';

export function OpenEiCard() {
  const [hasKey, setHasKey] = useState<boolean>();
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');

  useEffect(() => {
    api.openEi().then((s) => setHasKey(s.has_key), (e: Error) => setMessage(e.message));
  }, []);

  const save = (value: string) => {
    setBusy(true);
    setMessage('');
    api.setOpenEiKey(value).then(
      (s) => {
        setHasKey(s.has_key);
        setKey('');
        setBusy(false);
        setMessage(s.has_key ? 'Key saved.' : 'Key removed.');
      },
      (e: Error) => {
        setBusy(false);
        setMessage(e.message);
      },
    );
  };

  return (
    <section class="card page-narrow">
      <h2>OpenEI</h2>
      <Hint>
        Used by the electricity price when it is set to a utility's published rate. Needs a free key from openei.org (Utility Rate Database), stored on the server
        and sent to OpenEI only.
      </Hint>
      <p class="caption">{hasKey == null ? 'Checking…' : hasKey ? 'A key is saved.' : 'No key saved.'}</p>
      <label class="field">
        <span>{hasKey ? 'New API key' : 'API key'}</span>
        <input type="password" autocomplete="off" value={key} onInput={(e) => setKey((e.target as HTMLInputElement).value)} />
      </label>
      <div class="chips save">
        <button type="button" disabled={busy || key.trim().length < 8} onClick={() => save(key.trim())}>
          {hasKey ? 'Replace key' : 'Save key'}
        </button>
        {hasKey && (
          <button type="button" disabled={busy} onClick={() => save('')}>
            Remove
          </button>
        )}
      </div>
      {message && <p class="note" role="status">{message}</p>}
    </section>
  );
}
