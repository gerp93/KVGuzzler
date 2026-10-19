# TODO

This app's own backlog of future features and fixes — not a KVG_Standards
compliance checklist (see [KVG_Standards](https://github.com/gerp93/KVG_Standards)
and its `REPO_SCOPE.md` entry for that). Just what's not built yet.

## Features

- **Power sensors** (the point of the app). One `IPowerSource`-style interface, per-OS backends:
  - NVIDIA GPU: NVML (`nvidia-smi --query-gpu=power.draw` as a fallback).
  - AMD / Intel GPU: Linux `hwmon` / sysfs; Windows via ADLX or LibreHardwareMonitorLib.
  - CPU: Windows LibreHardwareMonitorLib; Linux RAPL (`/sys/class/powercap`).
  - Whole system: a smart plug or wall meter over its local HTTP API (Shelly / Tasmota), the only
    accurate total; calibrate the "rest of system" offset against it.
  - Needs elevated rights on both OSes (CPU sensors). Decide the design: an elevated helper
    service with the UI per-user. Note a per-machine helper conflicts with Velopack's per-user,
    no-admin install and update, so the helper needs its own install/update step.
- **Sampling and history**: sample every 1-5 s, store in the database, integrate with `EnergyMeter`
  (already written and tested in `KVGuzzler.Core`), skip sleep gaps.
- **Live dashboard**: wattage graph (ScottPlot or LiveCharts2; both MIT) and cost-so-far cards;
  per hour/day/week/month totals; per-session cost.
- **Rates**: time-of-use schedule (start, end, days, rate), then tiered rates. Store each rate with a
  date range so changing it does not reprice history. A "last bill total / kWh used" helper to get
  the all-in rate.
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
