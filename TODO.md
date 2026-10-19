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
- **Rates from bills** (built): Settings > Electricity rate can switch from "One rate" to "My bills". Each bill is
  its dates, total and kWh, plus zero or more flat fees to leave out (a basic charge, a credit); the price per kWh is
  (total - fees) / kWh and applies to everything recorded on those dates, so an old month keeps its own price.
  Days after the latest bill (or in a gap) use the latest bill's price as an estimate, and are re-priced when the next
  bill is entered; days before the first bill use the saved single rate. Two bills cannot claim the same day; past
  bills can be entered in any order. Each bill shows how much of its kWh KVGuzzler measured (wall meter, else the
  sensors' active power) and what that cost. Dashboard history, the Programs list and runs are priced by date.
  Not built: time-of-use or tiered rates (this bill has neither), a "work out my rate" helper for plain totals,
  per-program prices that follow the bill by day (a program's period uses one time-weighted average price).
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
