# TODO

This app's own backlog of future features and fixes — not a KVG_Standards
compliance checklist (see [KVG_Standards](https://github.com/gerp93/KVG_Standards)
and its `REPO_SCOPE.md` entry for that). Just what's not built yet.

## Features

- **Power sensors**: done so far (`IPowerSource` in `KVGuzzler.Core/Power`, shown live on the Dashboard):
  - NVIDIA GPU via NVML (Windows and Linux). Checked on an RTX 5070 Ti against `nvidia-smi`.
  - CPU package / cores / graphics / DRAM on Windows via the built-in "Energy Meter" performance
    counters: no driver, no admin rights, so no elevated helper is needed for these. Checked on an
    Intel Core Ultra 9 285K (about 50 W idle, 170 W under load). DRAM reads zero there and is hidden.
  - Linux: CPU/DRAM via RAPL (`/sys/class/powercap`, needs root since 2020) and GPU power via hwmon
    (amdgpu, i915, xe). Written and unit-tested against a fake sysfs tree; **not yet tried on real
    Linux hardware**.
  - Still to do:
    - AMD / Intel GPU on Windows (ADLX, or LibreHardwareMonitorLib).
    - AMD CPU on Windows: the Energy Meter counters probably do not exist there; needs
      LibreHardwareMonitorLib, which needs admin rights and a kernel driver. The collector service already
      runs with system rights, so it is the natural place to add it.
    - Laptop battery discharge rate (a whole-system figure while on battery): Windows WMI
      `BatteryStatus`, Linux `/sys/class/power_supply/BAT*/power_now`. Untestable on this desktop.
    - Whole system (wall meter): **built for a Shelly smart plug (second generation or newer, e.g. the
      Plug US Gen4), not yet tried on a real plug.** `ShellyPlug` / `WallMeterSource` read
      `GET /rpc/Switch.GetStatus?id=0` (`apower`, watts) once a second; the address is set in
      Settings > Wall meter and saved in `%ProgramData%\KVGuzzler[-dev]\wall-meter.json`, which the
      collector service follows. Tested with a fake plug, a stand-in Shelly server, and the real answer of a Plug US Gen4 (field names confirmed).
      To confirm on the real plug: the field names, whether authentication is off by default, and the
      reading's accuracy against the Eve plug. Still to do:
      - **Gaps filled from the plug's own energy counter** (`aenergy.total`): built, **not tried on a real plug or a real
        sleep.** The difference of the counter across a sleep / stop / power cut is saved as the gap's whole-PC energy
        (`plug_kwh` on the gap row) and shown under the whole-PC figure, marked "from the plug's own energy counter". Left
        unfilled when the counter is missing, went down (plug restart) or implies more than a plug can carry. To confirm on a
        real plug: that `aenergy.total` really keeps counting while the PC sleeps, and whether it survives a plug reboot or
        power cut (the code assumes it does not and leaves such a gap alone). Not built: filling a short stretch where only
        the plug was unreachable while the PC stayed awake, and splitting a long gap whose counter reset in the middle.
      - **Rates looked up from OpenEI** (a third "Price from" choice beside one rate and My bills): built, **not tried against the real
        service or with a real key.** To confirm: that `address=<ZIP>` + `sector=Residential` + `effective_on_date` returns the utility's
        current rates with `detail=full` (and is not too slow), the real field shapes (`energyratestructure`, the schedules, `unit`), and
        that `getpage=<label>` returns one rate. Not built: usage tiers (the first tier is used), holidays (the weekend schedule applies to
        them), demand and fixed charges, the tariff's history (today's is applied to past days), and Xcel-specific bill add-ons.
      - **Carbon footprint**: built with two sources, Electricity Maps and EIA (see CLAUDE.md), **not tried with a real key for either.** EIA's
        answer shape was checked against a real answer for MISO (DEMO_KEY); a real Electricity Maps free key was refused for MISO by Electricity
        Maps itself, so EIA is the likelier one to work.
        To confirm: the v3 `latest` path and `auth-token` header, the field names, the free-tier limits, and that the zone code or
        "lat, lon" the free key was set up for is accepted. Not built: carbon per program and per run, a grid-intensity line on the
        chart ("when is it clean"), a "run it later" suggestion, and other sources (UK Carbon Intensity needs no key).
      - **Other meters for peripherals (monitor, speakers)**: built, **not tried on real plugs.** Settings > Other meters
        (add, rename keeping history, remove), `peripheral-meters.json`, Dashboard "Peripherals" card and "Count" tick boxes
        (PC and each meter, so "peripherals only" works too), one chart line per meter, gaps filled from each plug's own counter,
        a note when a plug stops answering, and Export to CSV (see CLAUDE.md). Not built: an exact (not upper-bound) combined max
        draw, and a time-of-use view of the meters.
      - Other meters behind the same `IPowerSource`: Tasmota plugs (local JSON), and Eve / any Matter plug
        through a Home Assistant reader (REST `/api/states/<entity>`).
      - Calibrate a "rest of the system" offset against the wall figure (the Dashboard already shows wall
        minus sensors as "fans, drives, motherboard and power-supply losses").
- **Background recording** (built, **not yet tried as an installed service**): `KVGuzzler.exe --collector`
  runs headless as a Windows service and saves one-minute energy per component, the live readings, and
  gaps with their reason (asleep / off / lost power) to `%ProgramData%\KVGuzzler\history.db`. The window
  shows live readings, rolling hour / 24 h / 7 d / 30 d totals with measured and estimated energy apart, and
  has a "Turn on background recording" button (asks Windows for administrator permission, a UAC prompt).
  Verified here: the collector logic and history in unit tests, and the collector run headless in a console
  against the real sensors with the window reading its history. **Not verified (needs an admin install):**
  - `sc.exe` actually creating and starting the service, and that the service starts at boot.
  - The sensors as the system account (NVML and the Windows energy counters in a service session).
  - A normal user's window reading a history folder the system account created (the collector grants
    Users "modify" on it so SQLite can open the WAL file; check it works).
  - Sleep: detected as a long silence between samples. Not seen on real sleep yet.
  - Clean stop: whether Windows' shutdown reaches the host as a clean stop (else shutdown shows as "lost
    power", still counted as 0).
  - Velopack per-machine install and update: the install hooks (`Program.BuildVelopack`) register, stop,
    restart and remove the service; the per-machine `--instLocation` value and that an update replacing
    `current\` works with the service stopped first. See KVG_Standards `dotnet-avalonia.md`.
  - Linux: no service yet (an AppImage cannot install one); run `KVGuzzler --collector` yourself for now.
  Still to do: charts; per-day/week/month calendar totals (the windows are rolling); a Windows shutdown
  hook; an option to estimate time the PC was on but not recorded.
- **Per-program power** (built, Windows only): the collector estimates which program caused the CPU's and
  GPU's power and the Programs tab lists energy, cost and active time per executable, plus a live "right
  now" list. Method: only the power above a learned idle baseline is shared, in proportion to each program's
  share of CPU time and GPU engine time (Windows "Process" and "GPU Engine" counters; no admin needed);
  `ProgramAttributor` keeps the rows summing to exactly the energy measured. Checked on this PC with a real
  CPU load (a 16-process PowerShell burn was the top CPU user; the rows summed to 99.7% of the measured
  CPU+GPU energy). Known limits and what is not built:
  - **The idle baseline starts too high** if the PC was never quiet during a run (it is the lowest 30 s
    average seen so far, kept between runs). It only comes down, so the first day understates heavy
    users until a truly idle moment is seen. Consider a "reset" button or a manual override.
  - GPU engines of different adapters (this PC has an NVIDIA GPU and an Intel iGPU) are not separated by
    adapter, so iGPU use shares the NVIDIA card's power. Needs the adapter LUID mapped to the GPU.
  - Programs are named by executable name only: two different `app.exe` files merge. Memory and fan power
    are not attributed. CPU attribution counts busy time, not clock speed.
  - **Runs** (built): the Programs tab's "Recent runs" prices each session, from when a program is first
    seen running until none of its processes are left (`RunTracker`; runs under half a watt-hour are
    dropped). A program that is already open when recording starts begins its run then, and a program name
    that never fully closes (a shell, a browser with a background process) is one long run.
  - Not built: pinning a specific program (everything is tracked automatically, so not needed yet), an
    option to name a run (e.g. one game launch inside a launcher), Linux (`/proc` and the GPU's own
    per-process data).
- **Live dashboard**: the Dashboard's "Power over time" chart is built (whole PC from the wall meter, CPU and
  GPU, as average watts per bucket for the last hour / 24 hours / 7 days / 30 days, with the stretches nothing
  was recorded shaded by reason, and a hover readout). It is drawn directly, so there is no charting library.
  Checked on this PC's real history: axes, lines, shaded gaps and the hover guide line show correctly; the
  hover readout box itself was not seen on screen (the scripted pointer test lost the window), only unit-tested
  for its maths. Still to do: per-day/week/month totals as bars, and a per-program stacked view.
- **Totals cards** (built): the top of the Dashboard shows two big numbers, the energy used and what it cost, for a
  period chosen from a row of block-style buttons (last hour, 24 hours, 7 days, 30 days, year, all time; the choice
  is remembered, the first time it is "All time"). It is the whole PC from the wall meter if that recorded in the
  period (noting when it began part-way through), otherwise the CPU/GPU sensors; the cost is priced day by day.
  Not built: a comparison with the previous period, or a per-day bar view.
- **Rates from bills** (built): Settings > Electricity rate can switch from "One rate" to "My bills". Each bill is
  its dates, total and kWh, plus zero or more flat fees to leave out (a basic charge, a credit); the price per kWh is
  (total - fees) / kWh and applies to everything recorded on those dates, so an old month keeps its own price.
  Days after the latest bill (or in a gap) use the latest bill's price as an estimate, and are re-priced when the next
  bill is entered; days before the first bill use the saved single rate. Two bills cannot claim the same day; past
  bills can be entered in any order. Each bill shows how much of its kWh KVGuzzler measured (wall meter, else the
  sensors' active power) and what that cost. Dashboard history, the Programs list and runs are priced by date.
  Not built: time-of-use or tiered rates (this bill has neither), a "work out my rate" helper for plain totals,
  per-program prices that follow the bill by day (a program's period uses one time-weighted average price).
- **Whole-house energy monitor** (Refoss clamp monitors, always-on server; plan agreed, first parts built): `contract/` is the
  OpenAPI between the parts and `server/` (Go, see its README) is the house server. Built: the contract with checked examples, a
  read-only Refoss client (`DeviceInfo` parsed), a simulated house, SQLite history, a collector that drops impossible readings, and
  the API for status, devices, channels, live, series and summary, and the `web/` dashboard (Live, History, Appliances, Settings; themed,
  phone-width checked, embedded in the server binary). **Verified only against the simulator and fake HTTP servers**;
  nothing has been tried on a real Refoss monitor. Still to do, in order: real Refoss readings (call `Refoss.Status.Get` on a real
  device first and write the mapping from its answer; confirm auth is off), OpenEI/carbon in Go (port with the C# test
  cases; **rates and bills are done** (`server/internal/rates`, the Settings price editor, cost in the Appliances view), still to port: the
  OpenEI lookup and the carbon footprint; **Shelly plugs can be added in Settings** (read-only, restored after a restart, the PC's plug as a
  `pc` slice) but only tried against a stand-in plug, and `cmd/refoss-probe` is ready for the first real Refoss call), the PC relay (C# collector pushes per-minute summaries to `POST /api/v1/ingest/pc`
  with an outbox), the Electron viewer that loads the dashboard from the server, then retiring the Avalonia window (only once the
  dashboard does everything it does; the collector service stays). The server box itself (Debian) is set up separately.
- **House monitor: next** (see the whole-house item above for what exists):
  - **Appliance groups** are built (an entertainment center on two plugs shown as one; Settings > Appliance groups). Only Shelly plugs can be
    added so far, so a group of other brands waits for their sources (below).
  - **The C# relay client** is built (`RelayFeed`, `relay.json`; see CLAUDE.md) and tested against a fake server and the contract's field
    names. It was also run for real once: the development collector (its `KVGuzzler-dev` history, a week of backfill) pushed 26 minutes to a
    **throwaway local server**, which accepted everything (sensors, the CPU breakdown, the wall meter, programs including the system baseline, and
    gaps with their reasons). It has **not** pushed to the real box: the installed collector service is still the version before the relay, so it
    needs the update first. Then put `{"host": "<the box's address>"}` in `%ProgramData%\KVGuzzler\relay.json` and look for "House server: Sent N
    minute(s)" in its log and the PC's sensors and programs on its page under Appliances (the box's one computer adopts the first collector).
    No window setting for it yet (the window is frozen).
  - **Appliance groups** are built (an entertainment center on two plugs shown as one; Settings > Appliance groups). Only Shelly plugs can be
    added so far, so a group of other brands waits for their sources (below).
  - **The C# relay client** is built (`RelayFeed`, `relay.json`; see CLAUDE.md) and tested against a fake server and the contract's field
    names, **not yet run against the real box**. To try it: put `{"host": "<the box's address>"}` in `%ProgramData%\KVGuzzler\relay.json` (or the
    `-dev` folder for a development run) while the collector is running, and look for "House server: Sent N minute(s)" in its log and the
    PC under Appliances. No window setting for it yet (the window is frozen).
  - **Parity with the window, in progress** (the window stays until the dashboard does everything it does). Done: per-minute sensors, the wall
    breakdown and programs (relay), gaps with reasons, and now the **live look** (Right now with each part's share of the PC and "everything else",
    live per-program watts with the idle level, notes, Max draw, Recent runs; the per-component chart with shaded gaps). Programs now carry a cost (estimated; at the period's average price, runs at the price on the day they ended), the split bar and legend, and "how this is worked out". The energy history list (hour to all time, gaps by reason), the daily CSV export (formula-safe labels) and the year and all-time periods are done. The cost estimator (constant load, right now, my usage over a window; today's price and the real average per day come from the server) and the Count ticks (PC and each plug, in the figures, max draw and the estimator) are done. Still to do, in order:
    the OpenEI tariff lookup and the carbon footprint (both ports, with the currency symbol and the carbon columns of the CSV). The Help tab (version,
    links, how to read the numbers) and the read-only database card are done.
  - **The Electron viewer** (`shell/`) is built and was run against the real box (loads the dashboard, shows the unreachable page with the
    reason, and the setup form refuses a bad address and saves a good one). Still to do: a release (the tag namespace is shared with the
    desktop app's Velopack releases, which read this repo's releases to find updates, so check that a second app's release cannot confuse them,
    then KVG_Standards' `release-electron.yml` with `app_dir: shell` plus `electron-updater`), an icon (no logo exists and none is to be
    made up), VisualAssault theming for its two pages, and trying it off Windows. `npm run dist` was run once here: the packaged app starts and
    opens its setup page, and the one-click per-user installer (`shell/release/House Power Setup 0.1.0.exe`, about 107 MB, **unsigned**, so
    SmartScreen will warn) was built, but not installed or run.
  - Carbon footprint and the OpenEI tariff lookup in Go; the Refoss mapping (run `refoss-probe` first).
  - Other plug brands (the entertainment center's are not Shelly): a source per brand behind the same `Source` interface (Tasmota, and Matter plugs through Home Assistant).
- **Logo and icons**: real artwork in `assets/logo.png` (do not fabricate one), then
  `python scripts/generate-icons.py`, set `Window.Icon`, `<ApplicationIcon>` and uncomment `icon_ico` /
  `icon_png` in both release workflows; add the logo to the README header and the app header.
- Linux: add a `.desktop`/category via the release workflow if the AppImage menu entry needs it.

## Fixes

- Move the vendored `KvgDbLocation` pin from KVG_Standards `main` to a tag once KVG_Standards cuts one
  (`scripts/update-kvg-dblocation.sh <tag>`).
- First release: confirm the Windows `Setup.exe`, the Linux AppImage, and a self-update from N to
  N+1 on both (see KVG_Standards' `dotnet-avalonia.md`, "Not yet verified"). Nothing here has been
  run on Windows.
