import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { normalizeServerAddress } from './serverAddress';

export interface Settings {
  /** `http://host:port`, as made by normalizeServerAddress. */
  serverAddress?: string;
}

const FILE = 'settings.json';

/** The saved settings, or none when there is no file, it is unreadable, or the address in it is not a plain host (it is checked again on the way in). */
export function loadSettings(dir: string): Settings {
  try {
    const raw = JSON.parse(readFileSync(join(dir, FILE), 'utf8')) as { serverAddress?: unknown };
    if (typeof raw.serverAddress !== 'string') return {};
    const address = normalizeServerAddress(raw.serverAddress);
    return address ? { serverAddress: address } : {};
  } catch {
    return {};
  }
}

export function saveSettings(dir: string, settings: Settings): void {
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, FILE), JSON.stringify(settings, null, 2));
}
