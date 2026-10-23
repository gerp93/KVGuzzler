// The installed viewer for the house server: one window that loads the dashboard the server serves. It holds no data of its own and
// bundles no copy of the dashboard, so updating the server updates what this shows. The only thing it keeps is the server's address.
import { app, BrowserWindow, dialog, ipcMain, Menu, shell, type IpcMainInvokeEvent } from 'electron';
import { join } from 'node:path';
import { buildContextMenuTemplate } from './contextMenu';
import { buildMenuTemplate } from './menu';
import { probeServer } from './probe';
import { isLocalPage, isSafeExternalUrl, isServerUrl, normalizeServerAddress } from './serverAddress';
import { loadSettings, saveSettings, type Settings } from './settings';
import { checkForUpdate } from './updateCheck';

const PAGES = join(__dirname, 'pages');
const ABORTED = -3; // a load that was cancelled by a newer one is not a failure

let settings: Settings = {};
let win: BrowserWindow | undefined;
let zoom = 0;

function loadPage(name: 'setup' | 'error', query: Record<string, string> = {}): void {
  void win?.loadFile(join(PAGES, `${name}.html`), { query });
}

/** Shows the dashboard from the server, or asks for the server's address when there is none yet. */
function showServer(): void {
  if (!win) return;
  if (settings.serverAddress) void win.loadURL(settings.serverAddress);
  else loadPage('setup');
}

function openExternal(url: string): void {
  if (isSafeExternalUrl(url)) void shell.openExternal(url);
}

/** Help > Check for updates: says whether a newer viewer is published and offers its download page. Nothing is downloaded or replaced here. */
async function checkUpdates(): Promise<void> {
  const result = await checkForUpdate(app.getVersion());
  const options = { type: 'info' as const, title: 'House Power', buttons: ['OK'], defaultId: 0, cancelId: 0 };
  const message =
    result.kind === 'newer'
      ? { ...options, message: `Version ${result.version} is available.`, detail: `You have ${app.getVersion()}. The download page has the installer.`, buttons: ['Open the download page', 'Not now'], cancelId: 1 }
      : result.kind === 'current'
        ? { ...options, message: 'You have the newest viewer.', detail: `Version ${result.version}. The dashboard itself is always the server's, so it needs no update here.` }
        : { ...options, type: 'warning' as const, message: "Couldn't check for updates.", detail: result.reason };
  const answer = win ? await dialog.showMessageBox(win, message) : await dialog.showMessageBox(message);
  if (result.kind === 'newer' && answer.response === 0) openExternal(result.url);
}

function setZoom(direction: 'in' | 'out' | 'reset'): void {
  zoom = direction === 'reset' ? 0 : Math.max(-3, Math.min(5, zoom + (direction === 'in' ? 0.5 : -0.5)));
  win?.webContents.setZoomLevel(zoom);
}

function createWindow(): void {
  win = new BrowserWindow({
    width: 1240,
    height: 860,
    minWidth: 380,
    minHeight: 600,
    title: 'House power',
    backgroundColor: '#0d47a1',
    webPreferences: { preload: join(__dirname, 'preload.js'), contextIsolation: true, nodeIntegration: false, sandbox: true },
  });
  const contents = win.webContents;

  // Links that leave the server open in the browser; the window itself only ever shows the server or the viewer's own pages.
  contents.setWindowOpenHandler(({ url }) => {
    openExternal(url);
    return { action: 'deny' };
  });
  contents.on('will-navigate', (event, url) => {
    if (isLocalPage(url) || isServerUrl(url, settings.serverAddress)) return;
    event.preventDefault();
    openExternal(url);
  });
  contents.on('did-fail-load', (_event, code, description, url, isMainFrame) => {
    if (!isMainFrame || code === ABORTED || isLocalPage(url)) return;
    loadPage('error', { address: settings.serverAddress ?? '', reason: description });
  });
  contents.on('context-menu', (_event, params) => {
    const items = buildContextMenuTemplate(params);
    if (items.length > 0) Menu.buildFromTemplate(items).popup({ window: win });
  });
  win.on('closed', () => {
    win = undefined;
  });

  showServer();
}

/** Only the viewer's own pages may ask the app to do things; the dashboard (remote content) may not. */
function fromOwnPage(event: IpcMainInvokeEvent): boolean {
  return isLocalPage(event.senderFrame?.url ?? '');
}

function registerIpc(): void {
  ipcMain.handle('viewer:state', (event) => (fromOwnPage(event) ? { address: settings.serverAddress ?? '', version: app.getVersion() } : null));
  ipcMain.handle('viewer:set-server', async (event, input: unknown, force: unknown) => {
    if (!fromOwnPage(event) || typeof input !== 'string') return { ok: false, error: 'Refused.' };
    const address = normalizeServerAddress(input);
    if (!address) return { ok: false, error: "That isn't an address I can use. Enter the server's host, like 192.168.68.110 or house-box:8080." };
    // Look before saving: a mistyped address says so in a few seconds. Saving it anyway is one more press (the server may simply be off).
    if (force !== true) {
      const probe = await probeServer(address);
      if (!probe.ok) return { ok: false, error: probe.reason, canForce: true };
    }
    settings = { serverAddress: address };
    saveSettings(app.getPath('userData'), settings);
    showServer();
    return { ok: true };
  });
  ipcMain.handle('viewer:retry', (event) => {
    if (fromOwnPage(event)) showServer();
  });
  ipcMain.handle('viewer:change-server', (event) => {
    if (fromOwnPage(event)) loadPage('setup', { address: settings.serverAddress ?? '' });
  });
}

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', () => {
    if (win?.isMinimized()) win.restore();
    win?.focus();
  });

  void app.whenReady().then(() => {
    settings = loadSettings(app.getPath('userData'));
    registerIpc();
    Menu.setApplicationMenu(
      Menu.buildFromTemplate(
        buildMenuTemplate({
          isPackaged: app.isPackaged,
          version: app.getVersion(),
          platform: process.platform,
          appName: app.getName(),
          changeServer: () => loadPage('setup', { address: settings.serverAddress ?? '' }),
          checkForUpdates: () => void checkUpdates(),
          zoom: setZoom,
          openExternal,
        }),
      ),
    );
    createWindow();
    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow();
    });
  });

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit();
  });
}
