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
      collector service follows. Tested with a fake plug and against a stand-in Shelly server on this PC.
      To confirm on the real plug: the field names, whether authentication is off by default, and the
      reading's accuracy against the Eve plug. Still to do:
      - **Fill the gaps from the plug's own energy counter** (`aenergy.total`): the plug keeps counting
        while the PC is asleep or off, which is exactly what the wall meter is for. Needs a real plug to
        learn whether the counter survives a plug reboot or power cut. Until then, time the collector was
        not recording has no wall figure.
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
