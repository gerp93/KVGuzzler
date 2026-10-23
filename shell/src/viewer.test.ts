import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, it, vi } from 'vitest';
import { buildContextMenuTemplate } from './contextMenu';
import { buildMenuTemplate, REPO_URL, type MenuOptions } from './menu';
import { isLocalPage, isSafeExternalUrl, isServerUrl, normalizeServerAddress } from './serverAddress';
import { loadSettings, saveSettings } from './settings';
import { probeServer } from './probe';
import { checkForUpdate, isNewer, newestViewerRelease, parseVersion } from './updateCheck';

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
    changeServer: vi.fn(), checkForUpdates: vi.fn(), zoom: vi.fn(), openExternal: vi.fn(), ...over,
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

  it('has a Help menu with the repository, issues, a way to check for updates and a disabled version line', () => {
    const openExternal = vi.fn();
    const checkForUpdates = vi.fn();
    const help = submenu(buildMenuTemplate(options({ openExternal, checkForUpdates })), 'Help');
    help.find((i) => i.label === 'Check for updates…')?.click?.();
    expect(checkForUpdates).toHaveBeenCalledTimes(1);
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

describe('the update check', () => {
  const release = (tag: string, over: Record<string, unknown> = {}) => ({
    tag_name: tag, html_url: `https://github.com/gerp93/KVGuzzler/releases/tag/${tag}`, draft: false, prerelease: true, ...over,
  });
  const answer = (body: unknown, status = 200) => vi.fn(async () => new Response(JSON.stringify(body), { status })) as unknown as typeof fetch;

  it('reads and compares plain three-number versions', () => {
    expect(parseVersion('1.2.3')).toEqual([1, 2, 3]);
    for (const bad of ['1.2', '1.2.3.4', 'v1.2.3', '1.2.x', '1.2.3-beta', '', '-1.0.0']) expect(parseVersion(bad), bad).toBeNull();
    expect(isNewer([0, 2, 0], [0, 1, 9])).toBe(true);
    expect(isNewer([1, 0, 0], [0, 99, 99])).toBe(true);
    expect(isNewer([0, 1, 0], [0, 1, 0])).toBe(false);
    expect(isNewer([0, 0, 9], [0, 1, 0])).toBe(false);
  });

  it('picks the highest viewer release and ignores the desktop app, drafts, junk and pages elsewhere', () => {
    const list = [
      release('v0.0.99'), // the desktop app's releases share the repository and are never the viewer
      release('viewer-v0.1.0'),
      release('viewer-v0.10.0'),
      release('viewer-v0.9.0'),
      release('viewer-v9.9.9', { draft: true }),
      release('viewer-vnext'),
      release('viewer-v8.0.0', { html_url: 'https://evil.example/releases/tag/viewer-v8.0.0' }),
      release('viewer-v7.0.0', { html_url: 5 }),
      null, 'text', 5,
    ];
    expect(newestViewerRelease(list)).toEqual({ version: '0.10.0', url: 'https://github.com/gerp93/KVGuzzler/releases/tag/viewer-v0.10.0' });
    expect(newestViewerRelease([release('v1.0.0')])).toBeNull();
    expect(newestViewerRelease({ not: 'a list' })).toBeNull();
  });

  it('says newer, current or failed, and asks nothing about the person', async () => {
    const releases = [release('viewer-v0.2.0'), release('viewer-v0.1.0')];
    const fetchFn = answer(releases);
    expect(await checkForUpdate('0.1.0', fetchFn)).toEqual({ kind: 'newer', version: '0.2.0', url: 'https://github.com/gerp93/KVGuzzler/releases/tag/viewer-v0.2.0' });
    const [url, init] = (fetchFn as unknown as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://api.github.com/repos/gerp93/KVGuzzler/releases?per_page=50');
    expect(init.method).toBeUndefined(); // a plain GET
    expect(Object.keys(init.headers as object)).toEqual(['Accept']);

    expect(await checkForUpdate('0.2.0', answer(releases))).toEqual({ kind: 'current', version: '0.2.0' });
    expect(await checkForUpdate('0.3.0', answer(releases))).toEqual({ kind: 'current', version: '0.3.0' });
    expect(await checkForUpdate('0.1.0', answer([]))).toEqual({ kind: 'current', version: '0.1.0' });
    expect(await checkForUpdate('0.1.0', answer({ message: 'rate limited' }, 403))).toEqual({ kind: 'failed', reason: 'GitHub answered with an error (403).' });
    const down = vi.fn(async () => { throw new Error('getaddrinfo ENOTFOUND api.github.com'); }) as unknown as typeof fetch;
    expect(await checkForUpdate('0.1.0', down)).toMatchObject({ kind: 'failed', reason: expect.stringContaining('Could not reach GitHub') });
    expect(await checkForUpdate('dev', answer(releases))).toMatchObject({ kind: 'failed' });
  });
});

describe('checking the address before saving it', () => {
  const answer = (body: unknown, status = 200) => vi.fn(async () => new Response(typeof body === 'string' ? body : JSON.stringify(body), { status })) as unknown as typeof fetch;
  const server = 'http://192.168.68.110:8080';

  it('accepts a house server and asks it only for its status', async () => {
    const fetchFn = answer({ api_version: 1, server_version: 'db4aff0', notes: [] });
    expect(await probeServer(server, fetchFn)).toEqual({ ok: true, version: 'db4aff0' });
    expect((fetchFn as unknown as ReturnType<typeof vi.fn>).mock.calls[0]![0]).toBe('http://192.168.68.110:8080/api/v1/status');
  });

  it('says it is not the house server when something else answers', async () => {
    for (const fetchFn of [answer('<html>router login</html>'), answer({ hello: 1 }), answer([1, 2]), answer({ api_version: 'one' })]) {
      const r = await probeServer(server, fetchFn);
      expect(r.ok).toBe(false);
      if (!r.ok) expect(r.reason).toContain('not the house server');
    }
    const r = await probeServer(server, answer({}, 404));
    expect(r).toEqual({ ok: false, reason: '192.168.68.110:8080 answered, but not like the house server (error 404). Is that the right address and port?' });
  });

  it('says a mistyped or switched-off address does not answer, in words, within the time given', async () => {
    const down = vi.fn(async () => { throw new TypeError('fetch failed'); }) as unknown as typeof fetch;
    const r = await probeServer('http://162.168.68.110:8080', down);
    expect(r).toEqual({ ok: false, reason: 'Could not reach 162.168.68.110:8080. Check the address for a typo, and that the server is on.' });

    const hang = ((_url: string, init: RequestInit) => new Promise((_resolve, reject) => init.signal!.addEventListener('abort', () => reject(init.signal!.reason)))) as unknown as typeof fetch;
    const slow = await probeServer('http://162.168.68.110:8080', hang, 50);
    expect(slow.ok).toBe(false);
    if (!slow.ok) expect(slow.reason).toContain('did not answer');
  });
});
