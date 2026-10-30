// The OpenEI connection, in the Connectors tab: the free key for the Utility Rate Database that "a utility's published rate" prices from. The key is write-only: it is
// saved in a private file on the server, never shown again, and sent to api.openei.org only.
import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { ApiKeyField } from './ApiKeyField';

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
      <h2>Open Energy Information (OpenEI)</h2>
      <p class="caption">Utility electricity rates, for the electricity price. Free key from openei.org.</p>
      <ApiKeyField saved={hasKey} value={key} onInput={setKey} />
      <div class="chips save">
        <button type="button" disabled={busy || key.trim().length < 8} onClick={() => save(key.trim())}>
          Save
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
