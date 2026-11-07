import { AlertRulesCard } from './AlertRulesCard';
import { DatabaseCard } from './DatabaseCard';
import { HistoryStartCard } from './HistoryStartCard';
import { RatesCard } from './RatesCard';
import { THEMES, type Meter, type Theme } from './lib';

export function SettingsView({ theme, onTheme, meters }: { theme: Theme; onTheme: (t: Theme) => void; meters: Meter[] }) {
  return (
    <>
      <RatesCard />
      <AlertRulesCard meters={meters} />
      <HistoryStartCard />
      <DatabaseCard />
      <section class="card page-narrow">
        <h2>Theme</h2>
        <label>
          <span class="visually-hidden">Theme</span>
          <select value={theme} onChange={(e) => onTheme((e.target as HTMLSelectElement).value as Theme)}>
            {THEMES.map((t) => (
              <option key={t} value={t}>
                {t.replace(/-/g, ' ')}
              </option>
            ))}
          </select>
        </label>
      </section>
    </>
  );
}
