import { DatabaseCard } from './DatabaseCard';
import { RatesCard } from './RatesCard';
import { THEMES, type Theme } from './lib';

export function SettingsView({ theme, onTheme }: { theme: Theme; onTheme: (t: Theme) => void }) {
  return (
    <>
      <RatesCard />
      <DatabaseCard />
      <section class="card">
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
