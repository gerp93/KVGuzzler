// The PC collector, in the Connectors tab: the program on each PC that reports its CPU, GPU, memory and programs to this server. It pushes rather than being
// polled, so it is not in the Devices list; here is each computer that has one and whether it is being heard from. Read-only: where it sends to is set on the PC.
import { api } from './api';
import { collectorState } from './collector';
import { usePoll } from './hooks';
import { formatAge } from './lib';
import { routePath } from './route';

export function PcCollectorCard() {
  const { data, error } = usePoll(api.computers, 5000, []);
  const withCollector = (data ?? []).filter((c) => c.source_id);
  const now = Date.now();

  return (
    <section class="card page-narrow">
      <h2>PC collector</h2>
      <p class="caption">Reports each PC's CPU, GPU, memory and programs.</p>
      {error && <p class="note">{error.message}</p>}
      {data && withCollector.length === 0 && <p class="note">No PC collector has reported yet.</p>}
      {withCollector.length > 0 && (
        <ul class="settings devices">
          {withCollector.map((c) => {
            const s = collectorState(c.last_seen, now);
            return (
              <li key={c.id} class="device-row">
                <div class="device-main">
                  <span class="grow">
                    <span class={s.state === 'reporting' ? 'dot on' : 'dot'} role="img" aria-label={s.state === 'reporting' ? 'reporting' : 'not answering'} />
                    <a href={routePath('hierarchy', 'computer', c.id)}>{c.name}</a>{' '}
                    <span class="caption">
                      {s.state === 'never' ? 'never reported' : s.state === 'reporting' ? `reporting, ${formatAge(s.ageSeconds)}` : `not answering, last heard ${formatAge(s.ageSeconds)}`}
                    </span>
                  </span>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
