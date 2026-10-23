# server (housed)

The house server, written in Go: it polls the energy monitors and plugs on a timer, keeps per-minute history in SQLite
and serves the API in [`../contract/openapi.yaml`](../contract/openapi.yaml). One static binary, built with `go build`,
cross-compiles for the Linux box.

## Run it with no hardware

```bash
(cd web && npm install && npm run build)    # the dashboard, embedded into the server
cd server
go run ./cmd/housed -sim -data ./data        # a made-up house; nothing it reports is a measurement
# open http://localhost:8080/ , or: curl localhost:8080/api/v1/live
```

Flags: `-listen` (default `:8080`), `-data` (folder for `house.db`), `-sim`, `-tz` (IANA zone for day buckets, e.g. `America/Chicago`).

## Build and test

```bash
go vet ./... && go test ./...
GOOS=linux GOARCH=amd64 go build -o housed ./cmd/housed
```

## What exists

| Package | |
|---|---|
| `internal/hostcheck` | Accepts only a plain host or IP for a device (no URL, path, port). |
| `internal/refoss` | Read-only client for a Refoss monitor (`GET /rpc/<Method>`); an allowlist means a `*.Set` call cannot be made. Only `DeviceInfo` is parsed. |
| `internal/shelly` | Shelly plug (Gen2+) over one read-only GET (`/rpc/Switch.GetStatus?id=0`). A port of the desktop app's `ShellyPlug`, tested with the answer captured from a real Plug US Gen4. |
| `internal/devices` | Adds, removes and restores devices: a plug is read once before it is kept, saved, and polled again after a restart. |
| `internal/sim` | A simulated house with a dryer, range, EV charger and so on. |
| `internal/store` | SQLite (WAL, pure Go): devices, channels, per-minute history, totals and bucketed series. |
| `internal/collector` | Polls sources, drops impossible readings (and says so), never credits time a device was silent. |
| `internal/api` | Status, devices, channels (rename, switch off), live, series, summary. |
| `internal/rates` | Pricing: one rate or per-bill prices (bills, flat fees, estimates after the latest bill), exact decimal money. A port of the desktop app's `KVGuzzler.Core.Billing`; `rates_test.go` is `BillingTests.cs` ported case for case. |
| `internal/webui` | Serves the dashboard that `web/` builds into `internal/webui/dist` (embedded in the binary; not committed, so build `web/` first). |

## Deploying to the Linux box

```bash
scripts/deploy-server.sh            # target and settings come from ~/.config/kvguzzler/deploy.env
scripts/deploy-server.sh --unit     # also install server/deploy/housed.service if it differs
scripts/deploy-server.sh --env      # also write the box's /etc/housed/housed.env from your HOUSED_* settings
```

It builds the dashboard and a `linux/amd64` binary (the commit id is its version), copies it over SSH, keeps the old binary as
`/usr/local/bin/housed.previous`, restarts the `housed` service, and checks that the new version answers on the LAN, putting the
old binary back if it does not. The database is never touched. Rollback restores the binary only, not a changed unit or settings file.

**Where settings live.** Nothing about one site is in the repository:

- On your PC, `~/.config/kvguzzler/deploy.env` (outside the repo; `scripts/deploy.env.example` shows the fields): the box's `user@host`, the port, and the server settings.
- On the box, `/etc/housed/housed.env`: `HOUSED_TZ` (which calendar day a bill or "day" bucket belongs to), and optionally `HOUSED_LISTEN` and `HOUSED_DATA`. The unit file in `server/deploy/` carries only plain defaults and reads that file. A command-line flag still wins over the environment.
- Secrets are not kept in files at all: an API key (Electricity Maps, EIA, OpenEI) is typed into the dashboard once and stored in the database on the box.

## First call to a Refoss monitor

```bash
go run ./cmd/refoss-probe 192.168.1.50
```

Prints the monitor's device info and the raw `Refoss.Status.Get` answer (two read-only calls). Save that answer as a fixture; the
server's Refoss mapping is written from it. Until then adding a Refoss monitor answers 501.

## Not built yet (answers 501)

Carbon, and the PC ingest. Adding a Refoss monitor (only Shelly plugs can be added). Pricing from a looked-up utility tariff (OpenEI) is not ported either: saving that mode is refused. **Real Refoss readings are not wired in**: the shape of
`Refoss.Status.Get` (per-channel watts, volts, amps, kWh) has not been seen on a real device, so `StatusRaw` returns the body
untouched and nothing guesses its fields. The first real device call decides the mapping.

## Rules

- Devices are only read. The Refoss client refuses any method that is not on its allowlist.
- A device host is a plain host or IP, never a URL, path or port.
- The house is the sum of the `mains` channels only; circuits and plugs are parts of it and the PC slice is added to nothing.
- A reading above its channel's cap is dropped, not averaged, and reported in the notes.
