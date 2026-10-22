# contract

The one thing the three codebases share: the shape of the house API. Nothing here is code that runs.

- `openapi.yaml` is the source of truth (OpenAPI 3.1). The Go server (`server/`) implements it, the web dashboard
  (`web/`) and the Electron viewer (`shell/`) call it, and the C# PC collector (`src/KVGuzzler`) pushes to
  `POST /api/v1/ingest/pc`.
- `examples/` are real-looking answers and ingest bodies. `check-examples.mjs` validates each against its schema, so the
  examples cannot drift from the spec.
- Checks: `npm install && npm test` in this folder (lint with Redocly, then the examples). CI runs the same.

## Versioning

`api_version` is an integer major, currently **1**, sent in the `X-KVG-Api-Version` header and in the ingest body.
Adding an optional field or an endpoint keeps the major. Removing, renaming or re-typing anything bumps it, and the server
and its clients are then updated together. A client that meets a different major must say which side needs updating
instead of guessing.

## Rules the contract encodes

- Read-only toward devices: nothing here switches or configures a Refoss monitor or a plug.
- A `Scope` is never summed with another. House power is the `mains` channels only; `circuit` and `plug` are parts of it;
  the PC's wall plug (`pc`) is a slice of a circuit and is never added to anything.
- Money is a decimal string. Carbon is an estimate and carries how much of it is estimated.
- The ingest is idempotent per (`source_id`, `minute`, `name`) so the PC can resend its outbox after a failure.

## Not settled yet

The Refoss field names (per-channel watts, volts, amps, cumulative kWh) are not confirmed against a real device, so the
Go server's Refoss client maps whatever the real `Refoss.Status.Get` returns onto these channels. If a real response shows
the model needs another field, it is added here first.
