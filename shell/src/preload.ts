// Gives the viewer's own pages (setup, "can't reach the server") a few calls into the app. The house server's dashboard, which is
// remote content, gets nothing: the bridge exists only on a file: page.
import { contextBridge, ipcRenderer } from 'electron';

if (location.protocol === 'file:') {
  contextBridge.exposeInMainWorld('viewer', {
    state: (): Promise<{ address: string; version: string } | null> => ipcRenderer.invoke('viewer:state'),
    setServer: (address: string): Promise<{ ok: boolean; error?: string }> => ipcRenderer.invoke('viewer:set-server', address),
    retry: (): Promise<void> => ipcRenderer.invoke('viewer:retry'),
    changeServer: (): Promise<void> => ipcRenderer.invoke('viewer:change-server'),
  });
}
