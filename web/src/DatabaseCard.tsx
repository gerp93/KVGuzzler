// Where the server keeps its history and how big it has grown. Read-only: the file lives on the box and is backed up or moved there.
import { api } from './api';
import { Facts } from './Facts';
import { usePoll } from './hooks';
import { formatBytes } from './lib';

export function DatabaseCard() {
  const { data: status } = usePoll(api.status, 60_000, []);
  return (
    <section class="card page-narrow">
      <h2>Database</h2>
      {status?.database_path ? (
        <Facts
          items={[
            { label: 'Location', value: status.database_path },
            ...(status.database_bytes != null ? [{ label: 'Size', value: formatBytes(status.database_bytes) }] : []),
          ]}
        />
      ) : (
        <p class="note">{status ? 'Location unknown.' : 'Reading…'}</p>
      )}
    </section>
  );
}
