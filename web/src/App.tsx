import { useEffect, useState } from 'preact/hooks';
import { api, VersionMismatch } from './api';
import { AppliancesView } from './AppliancesView';
import { HelpView } from './HelpView';
import { HistoryView } from './HistoryView';
import { usePoll, useStored } from './hooks';
import { LiveView } from './LiveView';
import { DEFAULT_THEME, isTheme, type Channel } from './lib';
import { routePath, useRoute } from './route';
import { SettingsView } from './SettingsView';

const TABS = [
  { id: 'live', label: 'Live' },
  { id: 'history', label: 'History' },
  { id: 'appliances', label: 'Appliances' },
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

  // Bumped after a rename or a show/hide so the channel list is read again.
  const [rev, setRev] = useState(0);
  const status = usePoll(api.status, 30_000, []);
  const chan = usePoll<Channel[]>(() => api.channels(), 0, [rev]);

  const mismatch = [status.error, chan.error].find((e): e is VersionMismatch => e instanceof VersionMismatch);
  const active: TabId = TABS.find((t) => t.id === route.tab)?.id ?? 'live';
  const channels = chan.data ?? [];

  return (
    <>
      <header class="top">
        <h1>House power</h1>
        <nav aria-label="Sections">
          {TABS.map((t) => (
            <a key={t.id} href={routePath(t.id)} aria-current={t.id === active ? 'page' : undefined}>
              {t.label}
            </a>
          ))}
        </nav>
      </header>
      <main>
        {mismatch ? (
          <section class="card warn">
            <p>{mismatch.message}</p>
          </section>
        ) : !chan.data ? (
          <p class="note">{chan.error ? `Waiting for the server: ${chan.error.message}` : 'Loading…'}</p>
        ) : active === 'live' ? (
          <LiveView channels={channels} onOpenSettings={() => (window.location.hash = routePath('settings'))} />
        ) : active === 'history' ? (
          <HistoryView channels={channels} />
        ) : active === 'appliances' ? (
          <AppliancesView channels={channels} open={route.rest} />

        ) : active === 'help' ? (
          <HelpView />
        ) : (
          <SettingsView channels={channels} onChanged={() => setRev((n) => n + 1)} theme={theme} onTheme={setThemeName} />
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
