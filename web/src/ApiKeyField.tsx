// The write-only API key field every connector with a key uses (EIA, OpenEI), so they are entered the same way: whether a key is saved, then the field. The saved
// key is never shown; typing replaces it.
export function ApiKeyField({ saved, value, onInput }: { saved: boolean | undefined; value: string; onInput: (value: string) => void }) {
  return (
    <>
      <p class="caption">{saved == null ? 'Checking…' : saved ? 'A key is saved.' : 'No key saved.'}</p>
      <label class="field">
        <span>API key</span>
        <input
          type="password"
          autocomplete="off"
          value={value}
          placeholder={saved ? 'Type to replace the saved key' : ''}
          onInput={(e) => onInput((e.target as HTMLInputElement).value)}
        />
      </label>
    </>
  );
}
