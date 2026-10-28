import { useEffect, useRef, useState } from 'preact/hooks';
import { api, VersionMismatch } from './api';
import { HelpView } from './HelpView';
import { HierarchyView } from './HierarchyView';
import { CarbonView } from './CarbonView';
import { EstimatorView } from './EstimatorView';
import { HistoryView } from './HistoryView';
import { HouseNow } from './HouseNow';
import { TabIcon } from './Icons';
import { usePoll, useStored } from './hooks';
import { LiveView } from './LiveView';
import { DEFAULT_THEME, isTheme, setCurrencyCode, type Meter } from './lib';
import { routePath, useRoute } from './route';
import { Skeleton } from './Skeleton';
import { SettingsView } from './SettingsView';

const TABS = [
  { id: 'live', label: 'Live' },
  { id: 'history', label: 'History' },
  { id: 'carbon', label: 'Carbon' },
  { id: 'estimator', label: 'Estimator' },
  { id: 'hierarchy', label: 'Hierarchy' },
  { id: 'settings', label: 'Settings' },
  { id: 'help', label: 'Help' },
] as const;
type TabId = (typeof TABS)[number]['id'];

export function App() {
  const [route] = useRoute('live');
  const [themeName, setThemeName] = useStored('theme', DEFAULT_THEME);
  const theme = isTheme(themeName) ? themeName : DEFAULT_THEME;
  useEffect(() => {
    document.body.className = `${theme}-theme`;
  }, [theme]);

  // Bumped after a rename or a show/hide so the meter list is read again.
  const [rev, setRev] = useState(0);
  const status = usePoll(api.status, 10_000, []);
  const loaded = usePoll<Meter[]>(() => api.meters(), 0, [rev]);
  // Every amount is shown in the currency of the saved price settings.
  const rateSettings = usePoll(api.rates, 60_000, [rev]);
  setCurrencyCode(rateSettings.data?.currency);

  // The server is updated without the page knowing: when its version changes, this tab is showing the old dashboard. Reload at once if nobody is looking, else offer it.
  const firstVersion = useRef<string | undefined>(undefined);
  const [stale, setStale] = useState(false);
  const version = status.data?.server_version;
  useEffect(() => {
    if (!version) return;
    if (firstVersion.current === undefined) firstVersion.current = version;
    else if (version !== firstVersion.current) {
      if (document.hidden) window.location.reload();
      else setStale(true);
    }
  }, [version]);
  useEffect(() => {
    if (!stale) return;
    const onVisible = () => {
      if (document.hidden) window.location.reload();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => document.removeEventListener('visibilitychange', onVisible);
  }, [stale]);

  const mismatch = [status.error, loaded.error].find((e): e is VersionMismatch => e instanceof VersionMismatch);
  // The old Appliances tab became the hierarchy: its links open the same things there.
  const active: TabId = TABS.find((t) => t.id === (route.tab === 'appliances' ? 'hierarchy' : route.tab))?.id ?? 'live';
  const meters = loaded.data ?? [];
  // A new tab starts at its top, and the tab's name is in the browser tab so several open pages can be told apart.
  useEffect(() => {
    window.scrollTo(0, 0);
    document.title = `${TABS.find((t) => t.id === active)?.label ?? 'Live'} · House power`;
  }, [active]);
  const offline = !mismatch && status.error !== undefined;

  return (
    <>
      <header class="top">
        <h1>House power</h1>
        <span class={`conn${offline ? ' off' : ''}`} role="status">
          <i aria-hidden="true" />
          {offline ? 'Server not answering' : status.data ? 'Connected' : 'Connecting…'}
        </span>
        <nav aria-label="Sections">
          {TABS.map((t) => (
            <a key={t.id} href={routePath(t.id)} aria-current={t.id === active ? 'page' : undefined}>
              <TabIcon name={t.id} />
              <span>{t.label}</span>
            </a>
          ))}
        </nav>
        <HouseNow meters={meters} />
      </header>
      {offline && (
        <p class="banner" role="alert">
          Server not answering{status.error ? ` (${status.error.message})` : ''}. Showing the last data; retrying.
        </p>
      )}
      {stale && (
        <p class="update-banner" role="status">
          A new version of this page is ready.
          <button type="button" onClick={() => window.location.reload()}>
            Reload
          </button>
        </p>
      )}
      <main class={`page page-${active}${route.rest[0] ? ` page-${active}-${route.rest[0]}` : ''}`}>
        {mismatch ? (
          <section class="card warn">
            <p>{mismatch.message}</p>
          </section>
        ) : !loaded.data ? (
          loaded.error ? <p class="note">{`Waiting for the server: ${loaded.error.message}`}</p> : <Skeleton />
        ) : active === 'live' ? (
          <LiveView meters={meters} onAddDevice={() => (window.location.hash = routePath('hierarchy'))} />
        ) : active === 'history' ? (
          <HistoryView meters={meters} />
        ) : active === 'carbon' ? (
          <CarbonView meters={meters} />
        ) : active === 'estimator' ? (
          <EstimatorView meters={meters} />
        ) : active === 'hierarchy' ? (
          <HierarchyView meters={meters} open={route.rest} onChanged={() => setRev((n) => n + 1)} />
        ) : active === 'help' ? (
          <HelpView />
        ) : (
          <SettingsView theme={theme} onTheme={setThemeName} />
        )}
        {status.data && status.data.notes.length > 0 && active !== 'live' && (
          <section class="card warn">
            {status.data.notes.map((n) => (
              <p key={n}>{n}</p>
            ))}
          </section>
        )}
      </main>
    </>
  );
}
