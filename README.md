# KVGuzzler

See what your PC costs to run. KVGuzzler is a cross-platform (Windows and Linux) desktop app that
measures the power your GPU, CPU and other components draw and turns it into an electricity cost per
hour, day, month and year at your own rate.

> **Status: early.** The app shell, settings (electricity rate, database location), self-update, a
> cost estimator for a constant load, and **live power readings** work today: NVIDIA GPUs (NVML), and
> the CPU package, cores and memory (Windows energy counters; Linux RAPL, not yet tried on real Linux
> hardware). A background collector service (Windows, admin install) records history even when nobody is
> logged in; it is built but has not yet been tried as an installed service. Not built yet: AMD and Intel
> GPUs on Windows, AMD CPUs on Windows, graphs; see [TODO.md](TODO.md). On Windows the Programs tab
> estimates how much of the CPU and GPU energy each program caused (an estimate, not a measurement). A Shelly smart plug can be set
> up as a wall meter for the whole-PC figure (written from Shelly's documentation, not yet tried on a real plug).

## Install

Download from the [latest release](https://github.com/gerp93/KVGuzzler/releases/latest):

- **Windows**: the `Setup.exe`. It installs for your user (no admin prompt) and updates itself.
- **Linux**: the `.AppImage`. Make it executable (`chmod +x *.AppImage`) and run it. It updates itself.

Builds are unsigned, so Windows SmartScreen will warn about an unrecognized publisher; click through
to launch it.

## Build and test

Needs the current .NET LTS SDK (10.x).

```bash
dotnet build KVGuzzler.sln -warnaserror
dotnet test KVGuzzler.sln
dotnet run --project src/KVGuzzler
```

A `dotnet run` build is a development run: it keeps its data in a separate `KVGuzzler-dev` folder, so
it never touches an installed copy's database, and it skips update checks.

Layout: `src/KVGuzzler.Core` (energy and cost logic, no UI), `src/KVGuzzler` (the Avalonia app),
`tests/KVGuzzler.Tests` (unit tests plus headless UI tests).

## Standards

This repo follows [gerp93/KVG_Standards](https://github.com/gerp93/KVG_Standards): the
[Avalonia/.NET desktop stack](https://github.com/gerp93/KVG_Standards/blob/main/dotnet-avalonia.md)
(Velopack release and self-update, a vendored database-location helper, a vendored VisualAssault theme),
plus AGPL-3.0 licensing, `TODO.md` and `VERSION_BUMP.md`. Releases are cut automatically on every push to
`main` and manually through the "Cut Release" workflow; both call KVG_Standards'
`release-avalonia.yml`.

## License

[AGPL-3.0](LICENSE).
