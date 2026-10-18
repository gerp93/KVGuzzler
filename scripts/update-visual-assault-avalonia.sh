#!/usr/bin/env bash
# Template: copy to scripts/update-visual-assault-avalonia.sh in an Avalonia app repo and
# set DEST. Re-vendors the VisualAssault Avalonia theme files from a pinned release tag,
# stamping the source tag and commit into each file. Same idea as the Go/CSS consumers'
# re-vendor scripts in themes-versioning.md: a deliberate, reviewable bump, never `main`,
# never a hand edit.
#
# Usage: scripts/update-visual-assault-avalonia.sh <tag>      e.g. v0.3.0
set -euo pipefail

REPO="gerp93/VisualAssault"
SRC_DIR="packages/avalonia/VisualAssault"
FILES=(Themes.cs FluentMapping.cs ThemeManager.cs)
DEST="src/KVGuzzler/Themes/VisualAssault"   # TODO: this app's path

REF="${1:?usage: $0 <VisualAssault release tag, e.g. v0.3.0>}"
case "$REF" in main|master) echo "Pin to a release tag, not '$REF' (themes-versioning.md)." >&2; exit 1;; esac

SHA=$(git ls-remote "https://github.com/$REPO" "refs/tags/$REF" "refs/tags/$REF^{}" | awk '{print $1}' | tail -n 1)
[ -n "$SHA" ] || { echo "No tag '$REF' in $REPO" >&2; exit 1; }

mkdir -p "$DEST"
for f in "${FILES[@]}"; do
  curl -fsSL "https://raw.githubusercontent.com/$REPO/$SHA/$SRC_DIR/$f" -o "$DEST/$f.tmp"
  { echo "// Vendored-from: $REPO $REF ($SHA) - do not hand-edit; re-run scripts/update-visual-assault-avalonia.sh"; cat "$DEST/$f.tmp"; } > "$DEST/$f"
  rm "$DEST/$f.tmp"
done
echo "Updated $DEST from $REPO $REF ($SHA)"
