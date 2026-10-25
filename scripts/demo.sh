#!/usr/bin/env bash
# A demo of the whole dashboard with a made-up house and three weeks of made-up history, on this computer only. Nothing here is a measurement,
# and it never touches your real server or its data: it runs on its own address with its own temporary database.
#
#   bash scripts/demo.sh [port]        # then open http://127.0.0.1:<port> (default 8097); Ctrl-C stops it
set -euo pipefail
PORT="${1:-8097}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATA="${DEMO_DATA:-$(mktemp -d)}"
PY="$(command -v python3 || command -v python)"

(cd "$ROOT/web" && npm run build >/dev/null)
(cd "$ROOT/server" && go build -o "$DATA/housed-demo" ./cmd/housed)
"$DATA/housed-demo" -sim -sim-days 21 -data "$DATA" -listen "127.0.0.1:$PORT" &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT
"$PY" "$ROOT/scripts/demo-seed.py" "http://127.0.0.1:$PORT"
echo "The demo is at http://127.0.0.1:$PORT (Ctrl-C to stop; its data is in $DATA)"
wait $PID
