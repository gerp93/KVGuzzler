// About this server, how to read the numbers, and where to ask about problems. The dashboard is the server's own page, so it is always
// the version the box is running; the box is updated by deploying from the repository, not from here.
import { api } from './api';
import { usePoll } from './hooks';
import { ISSUES_URL, PROJECT_URL, VIEWER_RELEASES_URL } from './lib';

export function HelpView() {
  const { data: status } = usePoll(api.status, 30_000, []);
  return (
    <>
      <section class="card">
        <h2>KVGuzzler house server</h2>
        <p>{status ? `Version ${status.server_version}` : 'Reading the version…'}</p>
        {status && (
          <p class="caption">
            API version {status.api_version}
            {status.time_zone ? `, days and bill dates in ${status.time_zone}` : ''}.
          </p>
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
        <h2>Updates</h2>
        <p class="note">
          This page is served by the house server, so it is always the version the server is running (above). The server is updated by
          running the deploy script from the repository on a PC that can reach the box; the Windows collector and the old window update
          themselves from their own Help tab.
        </p>
      </section>

      <section class="card">
        <h2>Desktop viewer</h2>
        <p class="note">
          An installed window for this dashboard (Windows). It loads this page from the server and keeps only the server's address. The installer is
          unsigned, so Windows will warn about the publisher the first time.
        </p>
        <p>
          <a class="button" href={VIEWER_RELEASES_URL} target="_blank" rel="noreferrer noopener">
            Download the viewer
          </a>
        </p>
      </section>

      <section class="card">
        <h2>What do these numbers mean?</h2>
        <details open>
          <summary>The PC</summary>
          <p class="note">
            With a smart plug between the wall and the PC, the whole-PC figure is what the plug measures: it is the one that matches your
            bill. The CPU, GPU and memory sensors only cover what those parts draw while the PC is on; they leave out fans, drives, the
            motherboard, power-supply losses and any time it sleeps. The two are shown side by side and never added together.
          </p>
        </details>
        <details>
          <summary>The hierarchy and the house</summary>
          <p class="note">
            The house figure is the mains clamps. Circuits and plugs are parts of it, and the PC's plug is a slice of one of the circuits. None
            of them is added to another, so the house minus the circuits is what no clamp accounts for.
          </p>
        </details>
        <details>
          <summary>Programs, costs and estimates</summary>
          <p class="note">
            Which program used the power is an estimate: the power above the PC's idle level is shared out by how busy each program kept the
            CPU and GPU. A cost marked ≈ rests on an estimate or on a stand-in price (a day outside any bill). A stretch where nothing was being
            recorded is shown as a gap, not as zero.
          </p>
        </details>
      </section>
    </>
  );
}
