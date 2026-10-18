# CLAUDE.md - KVGuzzler

A PC power-consumption monitor and electricity-cost estimator (Avalonia / .NET, Windows + Linux).

**This repo follows [gerp93/KVG_Standards](https://github.com/gerp93/KVG_Standards)** - read its
[`dotnet-avalonia.md`](https://github.com/gerp93/KVG_Standards/blob/main/dotnet-avalonia.md) before
changing release, update, theming or database-location code, and use its `app-standards` skill for
audits. Gaps in the standard are fixed there, not worked around here.

## Commands

```bash
dotnet build KVGuzzler.sln -warnaserror     # warnings are errors, as in CI
dotnet test KVGuzzler.sln                    # unit tests + headless Avalonia UI tests
dotnet run --project src/KVGuzzler           # a development run (separate -dev data folder)
```

## Rules that are easy to break

- **Never hand-edit vendored files.** `src/KVGuzzler/KvgDbLocation/` and (when wired)
  `src/KVGuzzler/Themes/VisualAssault/` start with a `// Vendored-from:` line. Fix bugs upstream
  (KVG_Standards / VisualAssault), then re-run `scripts/update-kvg-dblocation.sh <ref>` /
  `scripts/update-visual-assault-avalonia.sh <tag>`. KVG_Standards has no tags yet, so `main` is the
  documented interim pin; VisualAssault must be a release tag, never `main`.
- **Velopack:** `VelopackApp.Build().Run()` is the first statement in `Main`; the running version comes
  only from Velopack (`UpdateService.VersionText`); `CheckForUpdatesAsync` throws outside an installed
  build, so `UpdateService` guards on `IsInstalled`. The `Velopack` NuGet package must stay a direct
  reference of `src/KVGuzzler` (the release workflow installs the matching `vpk` from it). Never change
  the package id (`KVGuzzler`) after releasing: installed copies find their updates by it. The repo
  must stay public so the updater can read releases without a token.
- **Database:** WAL mode through `Microsoft.Data.Sqlite`. Before moving the file, close every connection
  and call `ClearAllPools()` (`AppServices.CloseDatabase`) or recent writes sit in the `-wal` file and the
  copy is unreadable. Dev runs use the `KVGuzzler-dev` folder; one running copy per data folder
  (`InstanceLock`); a configured-but-missing database path asks the user, it never creates an empty file.
- **No menu bar.** Help/About lives in the window (Help tab). No DevTools in release builds.
- **Logo:** none yet and none is to be fabricated. See TODO.md.
- Commit `packages.lock.json` files; package versions float within a major, the lock files pin them.
