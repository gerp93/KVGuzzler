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
      LibreHardwareMonitorLib, which needs admin rights and a kernel driver. That brings back the open
      design question of an elevated helper service with the UI per-user (a per-machine helper
      conflicts with Velopack's per-user, no-admin install, so it needs its own install/update step).
    - Laptop battery discharge rate (a whole-system figure while on battery): Windows WMI
      `BatteryStatus`, Linux `/sys/class/power_supply/BAT*/power_now`. Untestable on this desktop.
    - Whole system: a smart plug or wall meter over its local HTTP API (Shelly / Tasmota), the only
      accurate total; calibrate the "rest of system" offset against it. Deferred until a meter is hooked up.
- **Sampling and history**: sampling every second and integrating the session total with
  `EnergyMeter` (skipping sleep gaps) works (`PowerMonitor`, `PowerSampler`). Still to do: store samples
  in the database so history survives a restart.
- **Live dashboard**: wattage graph (ScottPlot or LiveCharts2; both MIT) and cost-so-far cards;
  per hour/day/week/month totals; per-session cost.
- **Rates**: time-of-use schedule (start, end, days, rate), then tiered rates. Store each rate with a
  date range so changing it does not reprice history. A "last bill total / kWh used" helper to get
  the all-in rate.
- **Wire the VisualAssault theme**: once VisualAssault's `v0.3.0` tag exists, run
  `scripts/update-visual-assault-avalonia.sh v0.3.0`, call `ThemeManager.Apply(id)` in
  `App.OnFrameworkInitializationCompleted`, add a theme picker to Settings, persist the choice.
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
