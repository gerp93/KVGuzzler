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
- **Background collector (approved exception to the per-user install, see KVG_Standards'
  `dotnet-avalonia.md`, "Exception: per-machine install").** `KVGuzzler.exe --collector` is the same
  program running headless, as a Windows service (`KVGuzzlerCollector`; `-dev` for a development run).
  It writes `history.db` in `%ProgramData%\KVGuzzler[-dev]`; the window only reads it. The service
  logic is `PowerCollector` in `KVGuzzler.Core` (clock-driven, tested); `Collector/` is the thin host.
  The window never creates `history.db`, only the collector does. A gap is saved with its reason
  (asleep / off / lost power) and shown apart from measured energy; sleep is an estimate shown on its own
  line and never added to the "active" figures (CPU/GPU/memory sensors = active power; the whole-PC
  total is the wall meter's job, not built yet). A running service locks `KVGuzzler.exe`, so turn background recording off
  before rebuilding a development copy.
- **Wall meter.** A smart plug between the wall and the PC is the whole-PC figure (`ReadingScope.Wall`).
  It already contains every sensor reading, so it is shown next to them and **never added to them**
  (`PowerSampler` sums `Component` only). `ShellyPlug` only ever sends `GET`; nothing in the app may
  switch the plug, because the PC is plugged into it. The meter's address lives in
  `wall-meter.json` beside the history (not in the per-user settings: the collector service reads it too).
  Only a plain host or IP is accepted, never a URL or path.
- **Per-program power is an estimate and must read as one.** `ProgramAttributor` shares only the power above
  the learned idle baseline, by each program's share of CPU/GPU busy time; the baseline and anything no
  program claimed are their own rows, and the rows must add up to exactly the energy that went in (tested).
  Never present these numbers as measurements, and never add them to the sensor or wall figures: they
  split the CPU and GPU energy that is already counted.
- **Prices come from `RateSchedule`, never from reading the single rate setting directly.** In "My bills" mode each
  bill's price (total minus its flat fees, over its kWh) applies to the dates it covers; cost of a period is its
  energy priced stretch by stretch (`RateSchedule.Cost`), so a period spanning two bills is right. Bill dates are
  local calendar days. Money is `decimal`, kept as text in the database. Two bills may not share a day.
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
- **Window layout.** Default 1240x860, minimum 960x640; page content is capped at 1200 wide (820 for Settings/Help/Estimator).
  Shared look (cards, text classes `h1/h2/caption/note/hero/mono`, the `seg` period picker, `bar`) is `Views/UiStyles.axaml`;
  list rows share the `BarRow` template. One period picker (`StatsPeriodPanel`) drives the Dashboard stats, chart and program lists.
  A component's power is shown as a **share of the whole PC at the wall** when a wall meter exists (else of the sensor total, and
  the header says so); the chart's whole-PC line uses the theme's `Text` colour because `PrimaryAction` is invisible on dark themes.
- **No menu bar.** Help/About lives in the window (Help tab). No DevTools in release builds.
- **Logo:** none yet and none is to be fabricated. See TODO.md.
- Commit `packages.lock.json` files; package versions float within a major, the lock files pin them.
