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

## Repo layout and releases

The repo also holds a whole-house energy monitor beside the PC app: `contract/` (OpenAPI between the parts, `npm test`),
`server/` (Go house server, see its README; `go vet ./... && go test ./...`), `web/` (TypeScript dashboard, embedded in the server
binary; build it first: `cd web && npm run check`) and a planned `shell/` (Electron viewer that loads the dashboard from the server). The PC app stays C#: sensors and per-program shares only
exist on the PC. The Avalonia window is **frozen** (no new features; new UI goes in the dashboard) and is retired only once the
dashboard does everything it does; the collector service stays. **Desktop releases and the .NET CI are path-filtered** (`auto-release.yml`, `ci.yml`): only
changes under `src/`, `tests/`, `KVGuzzler.sln`, `Directory.Build.props` (and `VERSION_BUMP.md` for releases) cut a desktop
release, so server/web/viewer/docs-only pushes do not. To force a desktop release with no code change, edit `VERSION_BUMP.md`.

## Rules for the house server (`server/`, `contract/`)

- Devices are only **read**. The Refoss client has a method allowlist (no `*.Set`); a device host is a plain host/IP, never a URL,
  path or port (`internal/hostcheck`). Refoss local auth is turned off on the devices (a 401 is reported, not worked around).
- Scopes (`mains | circuit | plug | pc`) are never summed across each other: the house is the `mains` channels alone, circuits
  and plugs are parts of it, and the PC's wall plug is a slice of a circuit and is added to nothing.
- A reading above its channel's cap is dropped, not averaged, and said so in the notes. Time a device did not answer is not credited.
- Pricing in the server (`server/internal/rates`) is a **port** of `KVGuzzler.Core.Billing` (bills, flat fees, `RateSchedule`); its tests are
  `BillingTests.cs` ported case for case. A change to a pricing rule goes in both places, with the test in both. Money is exact (`big.Rat`),
  never a float; bill dates are local calendar days. Only "one rate" and "my bills" exist there: the OpenEI lookup and carbon are not ported.
- Plugs: `server/internal/shelly` is a port of `ShellyPlug` (one read-only GET; a plug often has the PC or a fridge on it, so nothing may
  switch it). A device is read once before it is saved, so a wrong address is refused with 502; a saved device is restored after a restart
  even if it is offline. The PC's own wall plug is added with scope `pc`: a slice, added to nothing.
- **Deployment settings are never committed.** The box's address, the port and the server settings (`HOUSED_TZ`...) live in
  `~/.config/kvguzzler/deploy.env` on the PC and `/etc/housed/housed.env` on the box; the repo has only `scripts/deploy.env.example` with
  placeholders. Deploy with `scripts/deploy-server.sh` (see `server/README.md`); it restores the old binary if the new one does not answer.
- Channel ids never contain a slash (they are URL path segments). Money is a decimal string. The contract's integer `api_version`
  changes only for a breaking change, and the server and its clients move together.
- **Not checked against a real Refoss monitor**: `Refoss.Status.Get`'s fields are unknown, so nothing may guess them; write the
  mapping from a real answer and keep it as a test fixture.

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
- **Impossible readings are dropped, not averaged.** NVML answers with nonsense (50-300 kW) on the first read after the PC wakes.
  `PowerSampler` drops any reading above `MaxComponentWatts` (sensors) / `MaxMeterWatts` (plugs) like a missing one and says so in the
  notes; `HistoryStore.RepairImplausibleReadings` (run by the collector at start) fixes rows saved before that. Keep both.
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
  `ReadingScope.Peripheral` (scope 3 in `samples`) and the reading **name is the label**, which is also the history key. Renaming
  (Settings) moves the history with `HistoryStore.RenamePeripheral` (run again ~25 s later, because the collector may still save a few
  seconds under the old name); a label may not be the wall meter's reading name (`samples` is keyed by minute and name). They are
  **never** in the sensor total, the wall figure, the per-program split or the PC's own total peak: every existing query filters by
  scope, so keep it that way. The Dashboard's "Count" row ticks which of PC and meters make up energy, cost, max draw, "right now",
  the chart (one dashed line per meter) and "My usage": `SettingKeys.PeripheralsOff` holds the meters left out (a new meter counts until
  switched off) and `SettingKeys.PcOff` the PC being unticked ("peripherals only", only while meters exist; `includePc` in
  `UsageSummary`/`PowerChart`). The combined max draw is only an upper bound (shown "≤"). **Gaps** are filled per meter from each
  plug's own `aenergy.total` like the wall meter (`gap_peripherals` table, `IPeripheralEnergyCounters`, counters saved in `meta` so a
  restart can fill too); that energy is in `PeripheralKwh`/`QueryPeripherals` (as `GapKwh`), never sampled and never in the chart. A
  meter that stops answering is named in the live notes (`IPowerSourceNotes`). Export (Dashboard > Energy history) is
  `HistoryExport.DailyCsv`, one row per local day; each plug row has an "Open in browser" button (`PlugPageUrl`: only `http://` + a host that
  passed `IsValidHost`, never a path; `MainWindow.UriLauncher` is the test seam); labels that could run as a spreadsheet formula are written as text. Removing a meter
  leaves its history but stops counting it.
- **Rate source is a choice of three** (Settings > Electricity rate > "Price from"; `SettingKeys.RateMode`: `one`, `bills`, `lookup`;
  `RateMode`): one fixed rate, per-bill prices, or a **looked-up tariff** from the OpenEI Utility Rate Database (`OpenEiClient`, window only,
  free key typed once, saved in the settings database and never shown again; the key is a query parameter because that is how OpenEI
  documents it, so it only goes to `api.openei.org`). A tariff is reduced to a 12 months x weekday/weekend x 24 hours grid of $/kWh
  (`Tariff`: the first tier's rate + adj + the month's fuel adjustment; fixed and demand charges are left out, tiers beyond the first are
  ignored and the Settings text says so; rates that cannot be priced per kWh are left out, never guessed). Everything else prices **per
  day** (`RateSchedule.RateAt`), so in lookup mode a day's price is the grid weighted by `UsageProfile` (the share of usage in each hour,
  weekday/weekend, learned from the last 60 days of history on the background read, at most hourly, only for a tariff that varies by
  time). Today's tariff is applied to the past as well; bills are the exact figure. A saved tariff over a week old is fetched again by its
  label when the window opens (the old one is kept if that fails). With a looked-up tariff the Cost card carries a footnote
  (`StatRateNote`) naming the utility, the rate, what the period averaged to per kWh, and that it is the energy charge only. **Not checked against the real service**: written from OpenEI's
  documented field list (the docs give no full sample answer), parsing leaves out what it cannot make sense of.
- **Carbon footprint (two sources, chosen in Settings: Electricity Maps or EIA).** `CarbonConfig.Source` (`CarbonSources`; files without one are
  Electricity Maps). **EIA** (`EiaClient`, US only, free registration key as a query parameter to `api.eia.gov` only) reads hourly generation by
  fuel for a balancing authority (`MISO`, `PJM`, `ERCO`...) and works the intensity out itself: generation-weighted IPCC life-cycle factors
  (`EiaClient.GramsPerKwh`: coal 820, gas 490, oil 650, nuclear 12, solar 45, wind 11, hydro 24, geothermal 38, biomass/other 230), leaving out
  storage (BAT, PS), unknown fuels and imports, and skipping partly reported hours. A request returns about 2.5 days, and the feed saves every
  hour, so hours missed while the collector was off are backfilled (Electricity Maps' free key only gives the latest). A saved setup belongs to
  its source: the other source's key is never sent to the wrong service. The original Electricity Maps notes follow. `carbon.json` in the machine-wide data folder holds a free personal Electricity Maps key and a
  place (zone code or "lat, lon"), set in Settings > Carbon footprint; the key is plain text there and is shown nowhere after saving.
  The **collector service** fetches (`CarbonFeed`, every 30 minutes, own timer, follows the file) `GET /v3/carbon-intensity/latest` with the
  key in the `auth-token` header and saves it per hour in `carbon_intensity`, plus its last outcome in `meta` (`CarbonStatus`, shown in
  Settings). The key goes only to `CarbonConfig.ApiBase` (a constant, `https://api.electricitymaps.com/v4`, never read from the file) and the place is validated and encoded,
  so nothing typed can change the request. The free key has no history, so hours before it was set up or while the collector was off
  have no figure: `CarbonCalculator` values them at the average of the hours that do (and says how much, `EstimatedShare`), and values
  the plugs' counter energy across gaps (no hour of its own) at the period average. Carbon follows the same Count choice as energy
  (`UsageStat.Carbon`), is a grid *average* (location based), never "what one more kWh would cause", and is always shown as "≈".
  Errors carry Electricity Maps' own `message` (never the key) so a refused key can be diagnosed from Settings. The host, `/v4/...` path
  and `auth-token` header were checked to answer (401 without a key) but **a real key has not been tried here**; parsing is tolerant. If
  the API moves again, change `CarbonConfig.ApiBase` and `ElectricityMapsClient.ParseLatest`. The free key is one zone chosen at signup:
  Minnesota (Xcel/NSP) is `US-MIDW-MISO` there and `MISO` at EIA. A free key can be valid yet refused with "You do not have access to this
  specific endpoint for this specific zone"; `scripts/test-carbon-key.ps1` (hidden key prompt) shows which endpoints a key can read.
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
