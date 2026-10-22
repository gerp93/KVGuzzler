# server (housed)

The house server, written in Go: it polls the energy monitors and plugs on a timer, keeps per-minute history in SQLite
and serves the API in [`../contract/openapi.yaml`](../contract/openapi.yaml). One static binary, built with `go build`,
cross-compiles for the Linux box.

## Run it with no hardware

```bash
cd server
go run ./cmd/housed -sim -data ./data        # a made-up house; nothing it reports is a measurement
curl localhost:8080/api/v1/live
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
| `internal/sim` | A simulated house with a dryer, range, EV charger and so on. |
| `internal/store` | SQLite (WAL, pure Go): devices, channels, per-minute history, totals and bucketed series. |
| `internal/collector` | Polls sources, drops impossible readings (and says so), never credits time a device was silent. |
| `internal/api` | Status, devices, channels (rename, switch off), live, series, summary. |

## Not built yet (answers 501)

Adding devices, rates and bills, carbon, and the PC ingest. **Real Refoss readings are not wired in**: the shape of
`Refoss.Status.Get` (per-channel watts, volts, amps, kWh) has not been seen on a real device, so `StatusRaw` returns the body
untouched and nothing guesses its fields. The first real device call decides the mapping.

## Rules

- Devices are only read. The Refoss client refuses any method that is not on its allowlist.
- A device host is a plain host or IP, never a URL, path or port.
- The house is the sum of the `mains` channels only; circuits and plugs are parts of it and the PC slice is added to nothing.
- A reading above its channel's cap is dropped, not averaged, and reported in the notes.
