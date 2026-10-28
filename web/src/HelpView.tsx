// About this server, how to read the numbers, and where to ask about problems. The dashboard is the server's own page, so it is always
// the version the box is running; the box is updated by deploying from the repository, not from here.
import { api } from './api';
import { Facts } from './Facts';
import { usePoll } from './hooks';
import { ISSUES_URL, PROJECT_URL, VIEWER_RELEASES_URL } from './lib';

export function HelpView() {
  const { data: status } = usePoll(api.status, 30_000, []);
  return (
    <>
      <section class="card page-narrow">
        <h2>KVGuzzler house server</h2>
        {status ? (
          <Facts
            items={[
              { label: 'Version', value: status.server_version },
              { label: 'API', value: String(status.api_version) },
              ...(status.time_zone ? [{ label: 'Time zone', value: status.time_zone }] : []),
            ]}
          />
        ) : (
          <p class="note">Reading…</p>
        )}
        <p>
          <a class="button" href={PROJECT_URL} target="_blank" rel="noreferrer noopener">
            Project page
          </a>{' '}
          <a class="button" href={ISSUES_URL} target="_blank" rel="noreferrer noopener">
            Report an issue
          </a>
        </p>
      </section>

      <section class="card">
        <h2>Desktop viewer</h2>
        <p class="caption">Windows app for this dashboard. The installer is unsigned.</p>
        <p>
          <a class="button" href={VIEWER_RELEASES_URL} target="_blank" rel="noreferrer noopener">
            Download the viewer
          </a>
        </p>
      </section>

      <section class="card">
        <h2>Reading the numbers</h2>
        <details open>
          <summary>The PC</summary>
          <p class="caption">
            The wall plug measures the whole PC and matches your bill. CPU, GPU and memory sensors cover those parts only, while the PC is on. Shown
            side by side, never added.
          </p>
        </details>
        <details>
          <summary>The house</summary>
          <p class="caption">
            The house is the mains clamps. Circuits and plugs are parts of it and are never added to each other; house minus circuits is what no clamp
            accounts for.
          </p>
        </details>
        <details>
          <summary>Estimates</summary>
          <p class="caption">
            Per-program power is an estimate. ≈ marks a cost based on an estimate or a stand-in price. Time with no recording is a gap, not zero.
          </p>
        </details>
      </section>
    </>
  );
}
