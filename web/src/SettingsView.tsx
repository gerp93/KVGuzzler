import { useState } from 'preact/hooks';
import { api } from './api';
import { ComputersCard } from './ComputersCard';
import { DevicesCard } from './DevicesCard';
import { GroupsCard } from './GroupsCard';
import { RatesCard } from './RatesCard';
import { THEMES, type Channel, type Theme } from './lib';

function ChannelRow({ channel, onChanged }: { channel: Channel; onChanged: () => void }) {
  const [label, setLabel] = useState(channel.label);
  const [error, setError] = useState('');
  const save = (body: { label?: string; enabled?: boolean }) =>
    api.updateChannel(channel.id, body).then(
      () => {
        setError('');
        onChanged();
      },
      (e: Error) => setError(e.message),
    );
  return (
    <li>
      <label class="grow">
        <span class="visually-hidden">Label for {channel.id}</span>
        <input
          value={label}
          maxLength={60}
          onInput={(e) => setLabel((e.target as HTMLInputElement).value)}
          onBlur={() => label !== channel.label && void save({ label })}
          onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
        />
      </label>
      <span class="caption">{channel.scope}</span>
      <label class="check">
        <input
          type="checkbox"
          checked={channel.enabled}
          onChange={(e) => void save({ enabled: (e.target as HTMLInputElement).checked })}
        />
        Show
      </label>
      {error && <span class="note">{error}</span>}
    </li>
  );
}

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
      <section class="card">
        <h2>Channels</h2>
        <p class="caption">
          Rename a channel or hide it. This only changes how this dashboard shows it; nothing is sent to the monitor.
        </p>
        <ul class="settings">
          {channels.map((c) => (
            <ChannelRow key={c.id} channel={c} onChanged={onChanged} />
          ))}
        </ul>
      </section>
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
