# shell (the installed viewer)

An Electron window that shows the house dashboard from the house server. It is a viewer and nothing else: it holds no data, and it does
not bundle a copy of the dashboard, so updating the server updates what it shows (your phone's browser shows the same page). The one thing
it keeps is the server's address.

```bash
cd shell
npm install
npm run check      # typecheck, tests, build
npm start          # build and open the window
npm run dist       # build the Windows installer into release/ (electron-builder, per-user, one click)
```

## How it behaves

- **First start:** no address is saved, so it asks for the server's host (`192.168.68.110`, or `house-box:8080`; the port defaults to 8080).
  The address is checked: only a plain host and port, never a URL with a path, a user name or a query. It is saved in the app's data folder
  (`settings.json`) and checked again every time it is read.
- **Afterwards:** the window loads `http://<host>:<port>/`. If the server does not answer it shows a "can't reach the house server" page with
  the reason, tries again by itself every 15 seconds, and offers to change the address (also under View > Server address).
- **The window only ever shows the server or the viewer's own two pages.** A link that leaves the server opens in the browser (web pages only,
  never a `file:` or a shell-handler URL). The window runs sandboxed with context isolation; the dashboard, being remote content, gets no
  access to the app, and only the viewer's own pages can ask it to change the address.
- **Menu** (KVG_Standards' electron-menu.md): View and Help only (plus the app-name menu on macOS). Reload, force reload and dev tools exist only
  in a development run. A right-click menu keeps cut, copy, paste and select all reachable without an Edit menu.

## Releasing

Actions > **Release (viewer)** > Run workflow, with a version like `0.1.0`: it typechecks, tests, builds the unsigned installer on Windows and
publishes `House-Power-Setup-0.1.0.exe` as a **pre-release** tagged `viewer-v0.1.0`. It is not KVG_Standards' `release-electron.yml` on purpose:
that publishes with electron-builder, which tags `vX.Y.Z`, and this repository's plain `vX.Y.Z` tags and releases belong to the desktop app (its
Velopack updater reads them; it asks for non-pre-releases only, so the viewer's pre-releases are invisible to it). The standard has no tag-prefix
option yet; raise it there.

## Not done yet

- **No automatic updater.** Help > Check for updates… looks at this repository's releases for a newer `viewer-vX.Y.Z` and offers its download page;
  nothing is downloaded or replaced by the app (the installer is unsigned, so a silent replace would be the wrong thing to build). The dashboard
  itself needs no update here: it is the server's.
- **No logo or icon** (none is to be fabricated), so the installer and the window use Electron's default.
- **Not themed**: its two pages use fixed colors, not the vendored VisualAssault themes the dashboard uses.
- Windows is the only target tried.
