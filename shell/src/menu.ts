// The application menu, kept to what this app uses (KVG_Standards' electron-menu.md): View and Help, and on macOS the app-name menu.
// No File, Edit or Window: there is nothing to open or save, text fields keep their own shortcuts and a right-click menu, and there is
// one window. Reload, force reload and dev tools exist only in a development run.
import type { MenuItemConstructorOptions } from 'electron';

export interface MenuOptions {
  isPackaged: boolean;
  version: string;
  platform: NodeJS.Platform;
  appName: string;
  changeServer: () => void;
  zoom: (direction: 'in' | 'out' | 'reset') => void;
  openExternal: (url: string) => void;
}

export const REPO_URL = 'https://github.com/gerp93/KVGuzzler';

export function buildMenuTemplate(o: MenuOptions): MenuItemConstructorOptions[] {
  // Electron's own zoomIn role binds only the first accelerator, so Ctrl+Shift+= (the + key) and the numpad + get items of their own.
  const zoomInItems: MenuItemConstructorOptions[] = [
    { label: 'Zoom In', accelerator: 'CmdOrCtrl+=', click: () => o.zoom('in') },
    { label: 'Zoom In', accelerator: 'CmdOrCtrl+Shift+=', visible: false, click: () => o.zoom('in') },
    { label: 'Zoom In', accelerator: 'CmdOrCtrl+numadd', visible: false, click: () => o.zoom('in') },
  ];

  const view: MenuItemConstructorOptions = {
    label: 'View',
    submenu: [
      ...(o.isPackaged ? [] : ([{ role: 'reload' }, { role: 'forceReload' }, { role: 'toggleDevTools' }, { type: 'separator' }] as MenuItemConstructorOptions[])),
      ...zoomInItems,
      { label: 'Zoom Out', accelerator: 'CmdOrCtrl+-', click: () => o.zoom('out') },
      { label: 'Actual Size', accelerator: 'CmdOrCtrl+0', click: () => o.zoom('reset') },
      { type: 'separator' },
      { role: 'togglefullscreen' },
      { type: 'separator' },
      // The one thing this app is for configuring: where the house server is.
      { label: 'Server address…', click: o.changeServer },
    ],
  };

  const help: MenuItemConstructorOptions = {
    label: 'Help',
    submenu: [
      { label: 'KVGuzzler on GitHub', click: () => o.openExternal(REPO_URL) },
      { label: 'Report an issue', click: () => o.openExternal(`${REPO_URL}/issues`) },
      { type: 'separator' },
      { label: `Version ${o.version}`, enabled: false },
    ],
  };

  const template: MenuItemConstructorOptions[] = [view, help];
  if (o.platform === 'darwin') {
    template.unshift({
      label: o.appName,
      submenu: [{ role: 'about' }, { type: 'separator' }, { role: 'services' }, { type: 'separator' }, { role: 'hide' }, { role: 'hideOthers' }, { role: 'unhide' }, { type: 'separator' }, { role: 'quit' }],
    });
  }
  return template;
}
