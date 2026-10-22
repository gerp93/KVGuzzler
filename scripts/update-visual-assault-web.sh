#!/usr/bin/env bash
# Re-vendors web/src/themes/themes.css from a specific VisualAssault tag (never main).
# Usage: scripts/update-visual-assault-web.sh v0.3.0
set -euo pipefail

TAG="${1:?Usage: update-visual-assault-web.sh <tag, e.g. v0.3.0>}"
OUT="$(dirname "$0")/../web/src/themes/themes.css"
URL="https://raw.githubusercontent.com/gerp93/VisualAssault/${TAG}/packages/css/themes.css"

{
  echo "/* Vendored from gerp93/VisualAssault packages/css/themes.css at tag ${TAG}."
  echo "   Do not hand-edit -- re-run scripts/update-visual-assault-web.sh <tag> to bump. */"
  echo ""
  curl -sf "$URL" | tail -n +2
} > "$OUT"

echo "Vendored themes.css from VisualAssault@${TAG} into $OUT"
