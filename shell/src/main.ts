// The installed viewer for the house server: one window that loads the dashboard the server serves. It holds no data of its own and
// bundles no copy of the dashboard, so updating the server updates what this shows. The only thing it keeps is the server's address.
import { app, BrowserWindow, ipcMain, Menu, shell, type IpcMainInvokeEvent } from 'electron';
import { join } from 'node:path';
import { buildContextMenuTemplate } from './contextMenu';
import { buildMenuTemplate } from './menu';
import { isLocalPage, isSafeExternalUrl, isServerUrl, normalizeServerAddress } from './serverAddress';
import { loadSettings, saveSettings, type Settings } from './settings';

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
  ipcMain.handle('viewer:set-server', (event, input: unknown) => {
    if (!fromOwnPage(event) || typeof input !== 'string') return { ok: false, error: 'Refused.' };
    const address = normalizeServerAddress(input);
    if (!address) return { ok: false, error: "That isn't an address I can use. Enter the server's host, like 192.168.68.110 or house-box:8080." };
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
