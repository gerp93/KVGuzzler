// The house server's address, and what the viewer will and will not open.
//
// The viewer builds the address itself from a host (and optional port), so only the server's own origin can ever be loaded into the
// window: a pasted URL with a path, a user name or a query is refused, as the other KVGuzzler apps refuse it for a device address.

/** The port the house server listens on unless told otherwise. */
export const DEFAULT_PORT = 8080;

const HOST = /^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$/;

/** `192.168.68.110` or `house-box:8080` (a leading `http://` and a trailing `/` are forgiven) as `http://host:port`; null if it is not a plain host. */
export function normalizeServerAddress(input: string): string | null {
  let text = input.trim();
  if (/^http:\/\//i.test(text)) text = text.slice('http://'.length);
  text = text.replace(/\/$/, '');
  if (text === '' || text.length > 260 || /[\s/?#@\\]/.test(text)) return null;

  const [host, port, ...extra] = text.split(':');
  if (extra.length > 0 || !host || host.length > 253 || !HOST.test(host)) return null;
  if (host.split('.').some((label) => label === '' || label.length > 63)) return null;
  let number = DEFAULT_PORT;
  if (port !== undefined) {
    if (!/^\d{1,5}$/.test(port)) return null;
    number = Number(port);
    if (number < 1 || number > 65535) return null;
  }
  // What the URL parser really makes of it must be the host that was typed (it rejects things like 192.168.68.300 as an IP).
  try {
    const url = new URL(`http://${host}:${number}/`);
    if (url.hostname.toLowerCase() !== host.toLowerCase() || url.pathname !== '/') return null;
  } catch {
    return null;
  }
  return `http://${host}:${number}`;
}

/** Only web pages are ever opened outside the app: a `file:` or a shell-handler URL would be opened by the OS with whatever claims it. */
export function isSafeExternalUrl(url: string): boolean {
  try {
    const { protocol } = new URL(url);
    return protocol === 'https:' || protocol === 'http:';
  } catch {
    return false;
  }
}

/** Whether `url` is on the same origin as the server (the only place the window may navigate to). */
export function isServerUrl(url: string, server: string | undefined): boolean {
  if (!server) return false;
  try {
    return new URL(url).origin === new URL(server).origin;
  } catch {
    return false;
  }
}

/** A page of the viewer itself (setup, "can't reach the server"), loaded from its own files. */
export function isLocalPage(url: string): boolean {
  return url.startsWith('file:');
}
