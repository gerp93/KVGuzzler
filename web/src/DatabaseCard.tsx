// Where the server keeps its history and how big it has grown. Read-only: the file lives on the box and is backed up or moved there.
import { api } from './api';
import { usePoll } from './hooks';
import { formatBytes } from './lib';

export function DatabaseCard() {
  const { data: status } = usePoll(api.status, 60_000, []);
  return (
    <section class="card">
      <h2>Database</h2>
      {status?.database_path ? (
        <>
          <p>
            Current file: <code>{status.database_path}</code>
          </p>
          {status.database_bytes != null && <p>Size: {formatBytes(status.database_bytes)}</p>}
        </>
      ) : (
        <p class="note">{status ? 'The server did not say where its database is.' : 'Reading…'}</p>
      )}
      <p class="caption">
        The history lives on the box. To back it up, stop the service and copy the folder; it is not moved from this page.
      </p>
    </section>
  );
}
