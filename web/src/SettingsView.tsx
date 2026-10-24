import { ChannelsCard } from './ChannelsCard';
import { ComputersCard } from './ComputersCard';
import { CarbonCard } from './CarbonCard';
import { DatabaseCard } from './DatabaseCard';
import { DevicesCard } from './DevicesCard';
import { GroupsCard } from './GroupsCard';
import { RatesCard } from './RatesCard';
import { THEMES, type Channel, type Theme } from './lib';

export function SettingsView({
  channels,
  onChanged,
  theme,
  onTheme,
}: {
  channels: Channel[];
  onChanged: () => void;
  theme: Theme;
  onTheme: (t: Theme) => void;
}) {
  return (
    <>
      <DevicesCard onChanged={onChanged} />
      <ComputersCard channels={channels} onChanged={onChanged} />
      <GroupsCard channels={channels} onChanged={onChanged} />
      <RatesCard />
      <ChannelsCard channels={channels} onChanged={onChanged} />
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
