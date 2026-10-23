// "Is there a newer viewer?": a look at this repository's releases for one tagged viewer-vX.Y.Z that is newer than the running version. It only
// tells the person and offers the download page; nothing is downloaded or replaced by the app. The viewer's releases are marked pre-release and
// use their own tag prefix so they can never be mistaken for the desktop app's Velopack releases (which read this same repository).

export const RELEASES_API = 'https://api.github.com/repos/gerp93/KVGuzzler/releases?per_page=50';
export const TAG_PREFIX = 'viewer-v';
const PAGE_PREFIX = 'https://github.com/gerp93/KVGuzzler/releases/';

export type UpdateResult =
  | { kind: 'newer'; version: string; url: string }
  | { kind: 'current'; version: string }
  | { kind: 'failed'; reason: string };

/** "1.2.3" as numbers, or null when it is not exactly three dot-separated whole numbers. */
export function parseVersion(text: string): [number, number, number] | null {
  const m = /^(\d{1,6})\.(\d{1,6})\.(\d{1,6})$/.exec(text.trim());
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
}

/** True when `a` is a higher version than `b`. */
export function isNewer(a: [number, number, number], b: [number, number, number]): boolean {
  for (let i = 0; i < 3; i++) if (a[i]! !== b[i]!) return a[i]! > b[i]!;
  return false;
}

/** The newest published viewer release in a releases answer, whatever it is; drafts, other tags and anything malformed are ignored. */
export function newestViewerRelease(releases: unknown): { version: string; url: string } | null {
  if (!Array.isArray(releases)) return null;
  let best: { numbers: [number, number, number]; version: string; url: string } | null = null;
  for (const r of releases) {
    if (typeof r !== 'object' || r === null) continue;
    const { tag_name: tag, html_url: url, draft } = r as { tag_name?: unknown; html_url?: unknown; draft?: unknown };
    if (draft === true || typeof tag !== 'string' || !tag.startsWith(TAG_PREFIX) || typeof url !== 'string') continue;
    const version = tag.slice(TAG_PREFIX.length);
    const numbers = parseVersion(version);
    // Only a page on this repository's releases is ever offered for opening.
    if (!numbers || !url.startsWith(PAGE_PREFIX)) continue;
    if (!best || isNewer(numbers, best.numbers)) best = { numbers, version, url };
  }
  return best ? { version: best.version, url: best.url } : null;
}

/** Looks for a newer viewer than `current`. `fetchFn` is only replaced by tests. Nothing about the person or the server is sent. */
export async function checkForUpdate(current: string, fetchFn: typeof fetch = fetch): Promise<UpdateResult> {
  const have = parseVersion(current);
  if (!have) return { kind: 'failed', reason: `This viewer's version (${current}) is not one I can compare.` };
  try {
    const res = await fetchFn(RELEASES_API, { headers: { Accept: 'application/vnd.github+json' }, signal: AbortSignal.timeout(15_000) });
    if (!res.ok) return { kind: 'failed', reason: `GitHub answered with an error (${res.status}).` };
    const newest = newestViewerRelease(await res.json());
    if (newest && isNewer(parseVersion(newest.version)!, have)) return { kind: 'newer', ...newest };
    return { kind: 'current', version: current };
  } catch (e) {
    return { kind: 'failed', reason: `Could not reach GitHub: ${e instanceof Error ? e.message : String(e)}` };
  }
}
