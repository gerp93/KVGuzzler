// "Is that the house server?": before an address is saved, ask it for its status (a plain GET) and see that it answers like one. A wrong address
// (a mistyped number, a machine that is off) then says so in a few seconds instead of leaving the window waiting on a connection that never opens.

export const PROBE_TIMEOUT_MS = 6000;

export type ProbeResult = { ok: true; version: string } | { ok: false; reason: string };

/** Asks `address` (`http://host:port`, already checked by normalizeServerAddress) for /api/v1/status. `fetchFn` is only replaced by tests. */
export async function probeServer(address: string, fetchFn: typeof fetch = fetch, timeoutMs = PROBE_TIMEOUT_MS): Promise<ProbeResult> {
  const shown = address.replace(/^http:\/\//, '');
  try {
    const res = await fetchFn(`${address}/api/v1/status`, { signal: AbortSignal.timeout(timeoutMs) });
    if (!res.ok) return { ok: false, reason: `${shown} answered, but not like the house server (error ${res.status}). Is that the right address and port?` };
    const body: unknown = await res.json().catch(() => null);
    if (typeof body !== 'object' || body === null || typeof (body as { api_version?: unknown }).api_version !== 'number') {
      return { ok: false, reason: `${shown} answered, but it is not the house server. Is that the right address and port?` };
    }
    const version = (body as { server_version?: unknown }).server_version;
    return { ok: true, version: typeof version === 'string' ? version : '' };
  } catch (e) {
    const timedOut = e instanceof Error && (e.name === 'TimeoutError' || e.name === 'AbortError');
    return {
      ok: false,
      reason: timedOut
        ? `${shown} did not answer within ${Math.round(timeoutMs / 1000)} seconds. Check the address for a typo, and that the server is on.`
        : `Could not reach ${shown}. Check the address for a typo, and that the server is on.`,
    };
  }
}
