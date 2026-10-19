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
  **Updates:** the service runs `current\KVGuzzler.exe` from the per-user install, so it must be stopped before Velopack can
  replace the files (otherwise: "one or more running processes prevented it", the old version comes back). The service is created
  with an ACL letting the logged-in user start/stop it (`ServiceInstaller.ServiceSecurity`); `ApplyUpdateAsync` stops it first
  (`EnsureStoppedForUpdateAsync`, UAC only if the ACL is missing, as on a service made by an older build) and the after-update hook
  starts it again.
- **Wall meter.** A smart plug between the wall and the PC is the whole-PC figure (`ReadingScope.Wall`).
  It already contains every sensor reading, so it is shown next to them and **never added to them**
  (`PowerSampler` sums `Component` only). `ShellyPlug` only ever sends `GET`; nothing in the app may
  switch the plug, because the PC is plugged into it. The meter's address lives in
  `wall-meter.json` beside the history (not in the per-user settings: the collector service reads it too).
  Only a plain host or IP is accepted, never a URL or path.
  **Gaps are filled from the plug's own counter.** The status answer's `aenergy.total` (Wh since the plug last restarted) is kept with
  every sample (`WallMeterSource.LatestCounter`, saved in `history.db` meta by `SetLive`). After a gap (sleep, or a collector restart
  whose gap is "off"/"lost power") `PowerCollector` waits for the plug's first answer, and when the counter went up (not down: a plug
  restart; not implying more than `MaxPlugWatts`; same plug address; read within `CounterNearGap` of the gap's start) saves the
  difference as `plug_kwh` on the **gap row**, never as a sample row. It counts in `ReadingScope.Wall` totals only
  (`HistoryStore.Query`/`EnergyKwh`, spread evenly over the gap so any period gets its share), never in sensor energy, `Monitored`
  time or the chart lines. A counter that is missing or went down leaves the gap as it was. Up to a second or two at each end is
  counted both sampled and in the counter's difference.
- **Other meters (monitor, speakers).** More smart plugs, each named by the user, in `peripheral-meters.json` beside `wall-meter.json`
  (`PeripheralMetersConfig`, polled by `PeripheralMetersSource`, followed by the collector like the wall meter). Their readings are
  `ReadingScope.Peripheral` (scope 3 in `samples`) and the reading **name is the label**, which is also the history key, so renaming
  starts a new line (there is no rename; remove and add). The label may not be the wall meter's reading name (`samples` is keyed by
  minute and name). They are **never** in the sensor total, the wall figure, the per-program split or the PC's own total peak: every
  existing query filters by scope, so keep it that way. They are added to the Dashboard's energy, cost, max draw, "right now",
  chart line and "My usage" only for the meters ticked in the Dashboard's "Also count" row (`SettingKeys.PeripheralsOff` holds the
  ones left out, so a new meter counts until switched off). The combined max draw is only an upper bound (shown "≤"). Unlike the wall
  meter, nothing is filled in for time the PC slept or was off (no `aenergy.total` use for them yet). Removing a meter leaves its
  history but stops counting it.
- **Per-program power is an estimate and must read as one.** `ProgramAttributor` shares only the power above
  the learned idle baseline, by each program's share of CPU/GPU busy time; the baseline and anything no
  program claimed are their own rows, and the rows must add up to exactly the energy that went in (tested).
  Never present these numbers as measurements, and never add them to the sensor or wall figures: they
  split the CPU and GPU energy that is already counted.
  **Program names:** an executable is the program (`chrome.exe`), except interpreters (`ProgramLabeler.IsGeneric`:
  python, node, java, dotnet, pwsh...), which are labelled with what they run, `python.exe (ComfyUI)`, from the
  command line, else the parent program, else the venv folder. Looked up once per process id (`ProgramNamer`, via
  `WindowsProcessInspector`, limited-access handle, no admin); only the short label is kept, never the command
  line (arguments can hold secrets). Labels are history keys, so changing the label rules splits old and new rows.
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
- **Four cards across the top of the Dashboard**: energy, cost, right now, and **Max draw** (`HistoryStore.QueryPeak`): the highest
  once-a-second reading in the period, of the whole PC at the wall when there is a meter, else of the sensors' *total*. The total's peak is
  its own per-minute row (`total_peaks`, written by `PowerCollector`) because the sensors' own peaks never all land in the same second;
  minutes from before that table existed only give an upper bound (the sum of their peaks), shown as "≤". The big numbers sit in a
  `Viewbox` that only shrinks, so a long figure still fits the narrower cards at the minimum window width.
- **Cost estimator** (Dashboard, under Energy history) has three bases: a typed constant load, the live draw (wall figure if there is a
  wall meter, else the sensors), and "My usage" - real kWh per day over a window, counting off/asleep time, via
  `UsageSummary.AverageDay`. **Sleep power** is only used for the history detail line when there is no wall meter, so Settings hides it once a
  plug is configured (the plug's own counter fills the gaps, see the wall meter rule above).
- **No menu bar.** Help/About lives in the window (Help tab). No DevTools in release builds.
- **Logo:** none yet and none is to be fabricated. See TODO.md.
- Commit `packages.lock.json` files; package versions float within a major, the lock files pin them.
