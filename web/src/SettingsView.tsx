import { CarbonCard } from './CarbonCard';
import { DatabaseCard } from './DatabaseCard';
import { RatesCard } from './RatesCard';
import { THEMES, type Theme } from './lib';
import { routePath } from './route';

export function SettingsView({ theme, onTheme }: { theme: Theme; onTheme: (t: Theme) => void }) {
  return (
    <>
      <section class="card page-narrow">
        <h2>Setting up the system</h2>
        <p class="caption">
          Devices, computers and appliance groups, and how the meters are laid out, are set up in the <a href={routePath('hierarchy')}>Hierarchy</a> tab (the + Add
          button). Settings is for what gets the app working: the price, the carbon footprint, the database and the theme.
        </p>
      </section>
      <RatesCard />
      <CarbonCard />
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
