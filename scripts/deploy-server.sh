#!/usr/bin/env bash
# Builds the house server (dashboard included) and installs it on the Linux box over SSH.
#
#   scripts/deploy-server.sh                  # the target comes from your deploy config
#   scripts/deploy-server.sh user@host        # or name it here
#   scripts/deploy-server.sh --skip-web       # reuse the dashboard already built
#   scripts/deploy-server.sh --unit           # also install server/deploy/housed.service if it differs
#   scripts/deploy-server.sh --env            # also write the box's /etc/housed/housed.env from your HOUSED_* settings
#
# Settings are kept OUT of the repository, in ~/.config/kvguzzler/deploy.env (or the file named by $KVG_DEPLOY_CONFIG).
# See scripts/deploy.env.example for what goes in it.
#
# What it does: build web/ (unless --skip-web), cross-compile linux/amd64 with the commit id as the version, copy the
# binary over, keep the old one as /usr/local/bin/housed.previous, restart the service, and check that the new version
# answers on the LAN. If it does not, the old binary is put back and the script fails. The database is never touched.
#
# Needs on the box: an SSH user with passwordless sudo, and the `housed` service from server/deploy/housed.service.
set -euo pipefail

CONFIG="${KVG_DEPLOY_CONFIG:-$HOME/.config/kvguzzler/deploy.env}"
DEPLOY_TARGET="${DEPLOY_TARGET:-}"
DEPLOY_PORT="${DEPLOY_PORT:-8080}"
if [ -f "$CONFIG" ]; then
  # shellcheck disable=SC1090
  . "$CONFIG"
fi

TARGET=""
SKIP_WEB=0
UNIT=0
WRITE_ENV=0
while [ $# -gt 0 ]; do
  case "$1" in
    --skip-web) SKIP_WEB=1 ;;
    --unit) UNIT=1 ;;
    --env) WRITE_ENV=1 ;;
    --port) DEPLOY_PORT="${2:?--port needs a number}"; shift ;;
    -*) echo "unknown option: $1" >&2; exit 2 ;;
    *) TARGET="$1" ;;
  esac
  shift
done
TARGET="${TARGET:-$DEPLOY_TARGET}"
if [ -z "$TARGET" ]; then
  echo "No target. Give user@host, or set DEPLOY_TARGET in $CONFIG (see scripts/deploy.env.example)." >&2
  exit 2
fi

HOST="${TARGET#*@}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="$(git -C "$ROOT" rev-parse --short HEAD)"
if [ -n "$(git -C "$ROOT" status --porcelain -- server web contract)" ]; then VERSION="$VERSION-dirty"; fi
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

# The server settings to put on the box: only the HOUSED_* names, only plain values, so nothing odd reaches a root-owned file.
ENV_LINES=""
if [ "$WRITE_ENV" -eq 1 ]; then
  for name in HOUSED_LISTEN HOUSED_DATA HOUSED_TZ; do
    value="${!name:-}"
    [ -z "$value" ] && continue
    if ! printf '%s' "$value" | grep -Eq '^[A-Za-z0-9_/:.+-]+$'; then
      echo "$name has characters that are not allowed ($value)." >&2
      exit 2
    fi
    ENV_LINES="$ENV_LINES$name=$value"$'\n'
  done
  if [ -z "$ENV_LINES" ]; then
    echo "--env: none of HOUSED_LISTEN, HOUSED_DATA, HOUSED_TZ is set in $CONFIG." >&2
    exit 2
  fi
fi

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
if [ "$WRITE_ENV" -eq 1 ]; then printf '%s' "$ENV_LINES" | ssh "$TARGET" 'cat > /tmp/housed.env.new'; fi

echo "== installing and restarting"
ssh "$TARGET" "UNIT=$UNIT WRITE_ENV=$WRITE_ENV bash -s" <<'REMOTE'
set -euo pipefail
if [ -f /usr/local/bin/housed ]; then sudo cp -p /usr/local/bin/housed /usr/local/bin/housed.previous; fi
sudo install -m 755 /tmp/housed.new /usr/local/bin/housed
rm -f /tmp/housed.new
if [ "$WRITE_ENV" = 1 ]; then
  sudo install -d -m 755 /etc/housed
  sudo install -m 644 /tmp/housed.env.new /etc/housed/housed.env
  rm -f /tmp/housed.env.new
  echo "settings written to /etc/housed/housed.env"
fi
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

echo "== checking that $VERSION answers at http://$HOST:$DEPLOY_PORT"
ok=0
for _ in $(seq 1 15); do
  sleep 1
  body="$(curl -s --max-time 3 "http://$HOST:$DEPLOY_PORT/api/v1/status" || true)"
  if echo "$body" | grep -q "\"server_version\":\"$VERSION\""; then ok=1; break; fi
done

if [ "$ok" -eq 1 ]; then
  echo "deployed $VERSION: $body"
  exit 0
fi

echo "the new version did not answer; putting the previous one back" >&2
ssh "$TARGET" 'if [ -f /usr/local/bin/housed.previous ]; then sudo install -m 755 /usr/local/bin/housed.previous /usr/local/bin/housed && sudo systemctl restart housed; fi'
exit 1
