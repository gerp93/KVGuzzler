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
| `internal/store` | SQLite (WAL, pure Go): devices, meters, per-minute history, totals and bucketed series. |
| `internal/collector` | Polls sources, drops impossible readings (and says so), never credits time a device was silent. |
| `internal/api` | Status, devices (add, remove), meters (rename, hide, tie to a computer), computers (add, rename, remove, detail), live, series, summary, rates, the PC push. |
| `internal/runs` | Finds the times a meter was working (a car charging, a dryer drying) from its recorded minutes: pure, tested on made-up minutes. Served by `GET /api/v1/meters/{id}/runs`. |
| `internal/rates` | Pricing: one rate or per-bill prices (bills, flat fees, estimates after the latest bill), exact decimal money. A port of the desktop app's `KVGuzzler.Core.Billing`; `rates_test.go` is `BillingTests.cs` ported case for case. |
| `internal/pcrelay` | What the PC collector pushes (`POST /api/v1/ingest/pc`) and the checks it must pass: impossible readings dropped and said so, a different major version refused. |
| `internal/store` (groups) | Appliance groups: several ordinary plugs shown as one appliance; a plug is in a group or tied to a computer, not both. |
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
- An API key (EIA, OpenEI) is typed into the dashboard once and kept on the box in `secrets.json` beside the database (mode 0600, readable by the `housed` user only), **not in the
  database**, so the snapshots and backups of `house.db` do not carry it. It is write-only (never sent back) and goes only to its own service. It is plain text there: the server needs it
  to use it, so this is about who can read the file and what gets copied, not encryption. Rotate a key at the service if a copy of the old database could have leaked it.

## First call to a Refoss monitor

```bash
go run ./cmd/refoss-probe 192.168.1.50
```

Prints the monitor's device info and the raw `Refoss.Status.Get` answer (two read-only calls). The mapping was written from a real EM16P's
answer (`internal/refoss/testdata/status-em16p.json`, with the Wi-Fi name, MAC and address taken out: never commit those). Add a monitor in
the dashboard's Settings (only the EM16P is supported); its inputs are named A1-A6, B1-B6, C1-C6 as on the unit, a clamp on backwards is
fixed with "Flip sign", and the two clamps of a 240 V breaker are paired into one circuit.

## Not built yet (answers 501)

A Refoss EM06P (only the EM16P has been seen), and the main-panel setup (mains meters on a monitor) is built but has not been tried.

## Rules

- Devices are only read. The Refoss client refuses any method that is not on its allowlist.
- A device host is a plain host or IP, never a URL, path or port.
- The house is the sum of the `mains` meters only; circuits and plugs are parts of it and the PC slice is added to nothing.
- A reading above its meter's cap is dropped, not averaged, and reported in the notes.
