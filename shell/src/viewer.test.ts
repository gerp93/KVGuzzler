import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, it, vi } from 'vitest';
import { buildContextMenuTemplate } from './contextMenu';
import { buildMenuTemplate, REPO_URL, type MenuOptions } from './menu';
import { isLocalPage, isSafeExternalUrl, isServerUrl, normalizeServerAddress } from './serverAddress';
import { loadSettings, saveSettings } from './settings';

describe('the server address', () => {
  it('accepts a plain host, with or without a port, and says where it will go', () => {
    expect(normalizeServerAddress('192.168.68.110')).toBe('http://192.168.68.110:8080');
    expect(normalizeServerAddress('  house-box  ')).toBe('http://house-box:8080');
    expect(normalizeServerAddress('house-box.lan:9000')).toBe('http://house-box.lan:9000');
    expect(normalizeServerAddress('http://192.168.68.110/')).toBe('http://192.168.68.110:8080');
    expect(normalizeServerAddress('HTTP://House-Box:81')).toBe('http://House-Box:81');
  });

  it('refuses anything that is not a plain host: a path, a user name, a query, another scheme, a bad port', () => {
    for (const bad of [
      '', '   ', 'http://', 'https://house-box', 'ftp://house-box', 'house-box/api', 'house-box/?x=1', 'user@house-box', 'house-box#frag',
      'house box', 'house-box:', 'house-box:0', 'house-box:65536', 'house-box:80:90', 'house-box:abc', '-house', 'house-', 'a..b', '.box',
      '192.168.68.300', 'house_box', '[::1]', 'http://evil.example/steal', 'javascript:alert(1)', 'house-box\\share', 'x'.repeat(300),
    ]) {
      expect(normalizeServerAddress(bad), JSON.stringify(bad)).toBeNull();
    }
  });

  it('only opens web pages outside the app', () => {
    expect(isSafeExternalUrl('https://github.com/gerp93/KVGuzzler')).toBe(true);
    expect(isSafeExternalUrl('http://192.168.1.5/')).toBe(true);
    for (const bad of ['file:///C:/Windows/System32/calc.exe', 'javascript:alert(1)', 'ms-msdt:/id', 'notaurl', '']) {
      expect(isSafeExternalUrl(bad), bad).toBe(false);
    }
  });

  it('recognises the server and the viewer\'s own pages, and nothing else', () => {
    const server = 'http://192.168.68.110:8080';
    expect(isServerUrl('http://192.168.68.110:8080/#/appliances', server)).toBe(true);
    expect(isServerUrl('http://192.168.68.110:8081/', server)).toBe(false);
    expect(isServerUrl('http://192.168.68.111:8080/', server)).toBe(false);
    expect(isServerUrl('https://192.168.68.110:8080/', server)).toBe(false);
    expect(isServerUrl('http://192.168.68.110:8080/', undefined)).toBe(false);
    expect(isServerUrl('not a url', server)).toBe(false);
    expect(isLocalPage('file:///C:/app/dist/pages/setup.html')).toBe(true);
    expect(isLocalPage('http://192.168.68.110:8080/')).toBe(false);
  });
});

describe('the saved settings', () => {
  const withDir = (run: (dir: string) => void) => {
    const dir = mkdtempSync(join(tmpdir(), 'kvg-viewer-'));
    try {
      run(dir);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  };

  it('round-trips the address', () =>
    withDir((dir) => {
      expect(loadSettings(dir)).toEqual({});
      saveSettings(dir, { serverAddress: 'http://house-box:8080' });
      expect(loadSettings(dir)).toEqual({ serverAddress: 'http://house-box:8080' });
    }));

  it('treats a missing, broken or hand-edited file as no setting, and re-checks the address on the way in', () =>
    withDir((dir) => {
      writeFileSync(join(dir, 'settings.json'), 'not json');
      expect(loadSettings(dir)).toEqual({});
      writeFileSync(join(dir, 'settings.json'), JSON.stringify({ serverAddress: 'http://evil.example/steal' }));
      expect(loadSettings(dir)).toEqual({});
      writeFileSync(join(dir, 'settings.json'), JSON.stringify({ serverAddress: 5 }));
      expect(loadSettings(dir)).toEqual({});
      writeFileSync(join(dir, 'settings.json'), JSON.stringify({ serverAddress: 'house-box' }));
      expect(loadSettings(dir)).toEqual({ serverAddress: 'http://house-box:8080' });
    }));

  it('writes only the address', () =>
    withDir((dir) => {
      saveSettings(dir, { serverAddress: 'http://house-box:8080' });
      expect(Object.keys(JSON.parse(readFileSync(join(dir, 'settings.json'), 'utf8')) as object)).toEqual(['serverAddress']);
    }));
});

describe('the application menu', () => {
  const options = (over: Partial<MenuOptions> = {}): MenuOptions => ({
    isPackaged: true, version: '1.2.3', platform: 'win32', appName: 'House Power',
    changeServer: vi.fn(), zoom: vi.fn(), openExternal: vi.fn(), ...over,
  });
  const labels = (t: ReturnType<typeof buildMenuTemplate>) => t.map((m) => m.label);
  const submenu = (t: ReturnType<typeof buildMenuTemplate>, label: string) => (t.find((m) => m.label === label)?.submenu ?? []) as { label?: string; role?: string; click?: () => void; enabled?: boolean; visible?: boolean }[];

  it('has View and Help and nothing else on Windows and Linux (no File, Edit or Window)', () => {
    expect(labels(buildMenuTemplate(options()))).toEqual(['View', 'Help']);
    expect(labels(buildMenuTemplate(options({ platform: 'linux' })))).toEqual(['View', 'Help']);
  });

  it('adds the app-name menu on macOS only', () => {
    expect(labels(buildMenuTemplate(options({ platform: 'darwin' })))).toEqual(['House Power', 'View', 'Help']);
  });

  it('keeps reload, force reload and dev tools out of a packaged app and in a development run', () => {
    const roles = (packaged: boolean) => submenu(buildMenuTemplate(options({ isPackaged: packaged })), 'View').map((i) => i.role);
    for (const dev of ['reload', 'forceReload', 'toggleDevTools']) {
      expect(roles(true)).not.toContain(dev);
      expect(roles(false)).toContain(dev);
    }
  });

  it('zooms with every plus key binding and offers the server address', () => {
    const zoom = vi.fn();
    const changeServer = vi.fn();
    const view = submenu(buildMenuTemplate(options({ zoom, changeServer })), 'View');
    const zoomIns = view.filter((i) => i.label === 'Zoom In');
    expect(zoomIns).toHaveLength(3); // =, shift+= and numpad +: the zoomIn role binds only the first
    expect(zoomIns.filter((i) => i.visible === false)).toHaveLength(2);
    zoomIns.forEach((i) => i.click?.());
    view.find((i) => i.label === 'Zoom Out')?.click?.();
    view.find((i) => i.label === 'Actual Size')?.click?.();
    expect(zoom.mock.calls.map((c) => c[0])).toEqual(['in', 'in', 'in', 'out', 'reset']);
    view.find((i) => i.label === 'Server address…')?.click?.();
    expect(changeServer).toHaveBeenCalledOnce();
  });

  it('has a Help menu with the repository, issues and a disabled version line', () => {
    const openExternal = vi.fn();
    const help = submenu(buildMenuTemplate(options({ openExternal })), 'Help');
    help.find((i) => i.label === 'KVGuzzler on GitHub')?.click?.();
    help.find((i) => i.label === 'Report an issue')?.click?.();
    expect(openExternal.mock.calls.map((c) => c[0])).toEqual([REPO_URL, `${REPO_URL}/issues`]);
    expect(help.find((i) => i.label === 'Version 1.2.3')?.enabled).toBe(false);
  });
});

describe('the right-click menu', () => {
  const flags = (over: Partial<{ canCut: boolean; canCopy: boolean; canPaste: boolean; canSelectAll: boolean }> = {}) => ({
    canCut: false, canCopy: false, canPaste: false, canSelectAll: false, ...over,
  });
  const roles = (p: Parameters<typeof buildContextMenuTemplate>[0]) => buildContextMenuTemplate(p).map((i) => i.role);

  it('offers only what is available in a text field', () => {
    expect(roles({ isEditable: true, selectionText: '', editFlags: flags({ canPaste: true, canSelectAll: true }) })).toEqual(['paste', 'selectAll']);
    expect(roles({ isEditable: true, selectionText: 'x', editFlags: flags({ canCut: true, canCopy: true, canPaste: true, canSelectAll: true }) })).toEqual(['cut', 'copy', 'paste', 'selectAll']);
  });

  it('offers copy on selected text elsewhere and nothing otherwise', () => {
    expect(roles({ isEditable: false, selectionText: 'a number', editFlags: flags({ canCopy: true }) })).toEqual(['copy']);
    expect(roles({ isEditable: false, selectionText: '  ', editFlags: flags({ canCopy: true }) })).toEqual([]);
    expect(roles({ isEditable: false, selectionText: '', editFlags: flags() })).toEqual([]);
  });
});
