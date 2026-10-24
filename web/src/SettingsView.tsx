import { useState } from 'preact/hooks';
import { api } from './api';
import { ComputersCard } from './ComputersCard';
import { CarbonCard } from './CarbonCard';
import { DatabaseCard } from './DatabaseCard';
import { DevicesCard } from './DevicesCard';
import { GroupsCard } from './GroupsCard';
import { RatesCard } from './RatesCard';
import { THEMES, type Channel, type Theme } from './lib';

const isRefoss = (c: Channel) => c.device_id.startsWith('refoss-');

function ChannelRow({ channel, all, onChanged }: { channel: Channel; all: Channel[]; onChanged: () => void }) {
  const [label, setLabel] = useState(channel.label);
  const [error, setError] = useState('');
  // The other legs this one could be paired with: inputs of the same monitor that are not already part of a pair.
  const taken = new Set(all.flatMap((c) => (c.pair_with ? [c.pair_with] : [])));
  const options = all.filter((c) => c.device_id === channel.device_id && c.id !== channel.id && !c.pair_with && (!taken.has(c.id) || c.id === channel.pair_with));
  const partnerOf = all.find((c) => c.pair_with === channel.id);
  const save = (body: { label?: string; enabled?: boolean; invert?: boolean; pair_with?: string | null; scope?: 'circuit' | 'mains' }) =>
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
      {isRefoss(channel) && !partnerOf && (
        <>
          <label class="check">
            <input type="checkbox" checked={channel.invert === true} onChange={(e) => void save({ invert: (e.target as HTMLInputElement).checked })} />
            Flip sign
          </label>
          <label class="check">
            <span class="visually-hidden">Other leg of the breaker for {channel.label}</span>
            <select value={channel.pair_with ?? ''} onChange={(e) => void save({ pair_with: (e.target as HTMLSelectElement).value || null })}>
              <option value="">One wire</option>
              {options.map((c) => (
                <option key={c.id} value={c.id}>
                  plus {c.label}
                </option>
              ))}
            </select>
          </label>
          <label class="check">
            <span class="visually-hidden">What {channel.label} measures</span>
            <select value={channel.scope} onChange={(e) => void save({ scope: (e.target as HTMLSelectElement).value as 'circuit' | 'mains' })}>
              <option value="circuit">circuit</option>
              <option value="mains">the mains</option>
            </select>
          </label>
        </>
      )}
      {partnerOf && <span class="caption">second leg of {partnerOf.label}</span>}
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
          Rename a channel or hide it. This only changes how this dashboard shows it; nothing is sent to the monitor. On a Refoss monitor a load should read
          positive: if a channel reads backwards, switch on "Flip sign" for it (or turn the clamp round). A 240 V breaker has a clamp on each of its two legs
          (one on an A input, one on a B input): choose "plus" the other one so the two show as one circuit.
        </p>
        <ul class="settings">
          {channels.map((c) => (
            <ChannelRow key={c.id} channel={c} all={channels} onChanged={onChanged} />
          ))}
        </ul>
      </section>
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
