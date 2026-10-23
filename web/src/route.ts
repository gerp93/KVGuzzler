// A tiny hash router: the address says which tab is open and, in Appliances, which thing is open, so the browser's back
// button, a reload and a copied link all land in the same place.
//
//   #/live                                  a tab
//   #/appliances                            the list
//   #/appliances/channel/<channel id>       one appliance
//   #/appliances/computer/<computer id>     one computer
import { useEffect, useState } from 'preact/hooks';

export interface Route {
  tab: string;
  /** What is open inside the tab (empty for the tab itself): `["computer", "comp-1a2b"]`. */
  rest: string[];
}

export function parseRoute(hash: string, fallbackTab: string): Route {
  const parts = hash
    .replace(/^#\/?/, '')
    .split('/')
    .filter(Boolean)
    .map((p) => {
      try {
        return decodeURIComponent(p);
      } catch {
        return p;
      }
    });
  return { tab: parts[0] ?? fallbackTab, rest: parts.slice(1) };
}

export function routePath(tab: string, ...rest: string[]): string {
  return `#/${[tab, ...rest].map(encodeURIComponent).join('/')}`;
}

/** The current route, and a way to go somewhere (a path made with routePath). */
export function useRoute(fallbackTab: string): [Route, (path: string) => void] {
  const [hash, setHash] = useState(() => window.location.hash);
  useEffect(() => {
    const on = () => setHash(window.location.hash);
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  return [parseRoute(hash, fallbackTab), (path: string) => (window.location.hash = path)];
}
