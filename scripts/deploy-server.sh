#!/usr/bin/env bash
# Builds the house server (dashboard included) and installs it on the Linux box over SSH.
#
#   scripts/deploy-server.sh kvg@192.168.68.110
#   scripts/deploy-server.sh kvg@192.168.68.110 --skip-web     # reuse the dashboard already built
#   scripts/deploy-server.sh kvg@192.168.68.110 --unit         # also install server/deploy/housed.service if it differs
#
# What it does: build web/ (unless --skip-web), cross-compile linux/amd64 with the commit id as the version, copy the
# binary over, keep the old one as /usr/local/bin/housed.previous, restart the service, and check that the new version
# answers on the LAN. If it does not, the old binary is put back and the script fails. The database is never touched.
#
# Needs on the box: an SSH user with passwordless sudo, and the `housed` service from server/deploy/housed.service.
set -euo pipefail

TARGET="${1:?Usage: deploy-server.sh <user@host> [--skip-web] [--unit] [--port 8080]}"
shift
SKIP_WEB=0
UNIT=0
PORT=8080
while [ $# -gt 0 ]; do
  case "$1" in
    --skip-web) SKIP_WEB=1 ;;
    --unit) UNIT=1 ;;
    --port) PORT="${2:?--port needs a number}"; shift ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

HOST="${TARGET#*@}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="$(git -C "$ROOT" rev-parse --short HEAD)"
if [ -n "$(git -C "$ROOT" status --porcelain -- server web contract)" ]; then VERSION="$VERSION-dirty"; fi
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

if [ "$SKIP_WEB" -eq 0 ]; then
  echo "== building the dashboard"
  (cd "$ROOT/web" && npm ci --no-audit --no-fund >/dev/null && npm run build >/dev/null)
fi

echo "== building housed $VERSION for linux/amd64"
(cd "$ROOT/server" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/housed" ./cmd/housed)

echo "== copying to $TARGET"
scp -q "$OUT/housed" "$TARGET:/tmp/housed.new"
if [ "$UNIT" -eq 1 ]; then scp -q "$ROOT/server/deploy/housed.service" "$TARGET:/tmp/housed.service.new"; fi

echo "== installing and restarting"
ssh "$TARGET" "UNIT=$UNIT bash -s" <<'REMOTE'
set -euo pipefail
if [ -f /usr/local/bin/housed ]; then sudo cp -p /usr/local/bin/housed /usr/local/bin/housed.previous; fi
sudo install -m 755 /tmp/housed.new /usr/local/bin/housed
rm -f /tmp/housed.new
if [ "$UNIT" = 1 ]; then
  if ! cmp -s /tmp/housed.service.new /etc/systemd/system/housed.service; then
    sudo install -m 644 /tmp/housed.service.new /etc/systemd/system/housed.service
    sudo systemctl daemon-reload
    echo "unit file updated"
  fi
  rm -f /tmp/housed.service.new
fi
sudo systemctl restart housed
REMOTE

echo "== checking that $VERSION answers at http://$HOST:$PORT"
ok=0
for _ in $(seq 1 15); do
  sleep 1
  body="$(curl -s --max-time 3 "http://$HOST:$PORT/api/v1/status" || true)"
  if echo "$body" | grep -q "\"server_version\":\"$VERSION\""; then ok=1; break; fi
done

if [ "$ok" -eq 1 ]; then
  echo "deployed $VERSION: $body"
  exit 0
fi

echo "the new version did not answer; putting the previous one back" >&2
ssh "$TARGET" 'if [ -f /usr/local/bin/housed.previous ]; then sudo install -m 755 /usr/local/bin/housed.previous /usr/local/bin/housed && sudo systemctl restart housed; fi'
exit 1
