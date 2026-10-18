#!/usr/bin/env bash
# Template: copy to scripts/update-kvg-dblocation.sh in an Avalonia app repo and set DEST.
# Re-vendors KvgDbLocation.cs from a pinned KVG_Standards ref into the app, stamping the
# source ref and commit into the file. This is the deliberate, reviewable equivalent of
# bumping a pinned dependency version: never hand-edit the vendored copy, fix bugs in
# KVG_Standards and re-run this.
#
# Usage: scripts/update-kvg-dblocation.sh <ref>
#   <ref> is a KVG_Standards tag (e.g. v0.3.0). KVG_Standards has no tags yet, so "main"
#   is the documented interim exception (see update-check-versioning.md).
set -euo pipefail

REPO="gerp93/KVG_Standards"
SRC_PATH="packages/dotnet/KvgDbLocation/KvgDbLocation.cs"
DEST="src/KVGuzzler/KvgDbLocation/KvgDbLocation.cs"   # TODO: this app's path

REF="${1:?usage: $0 <KVG_Standards tag, or main until the first tag exists>}"

# Resolve the ref to an immutable commit (prefer the peeled commit of an annotated tag).
SHA=$(git ls-remote "https://github.com/$REPO" "$REF" "refs/tags/$REF^{}" | awk '{print $1}' | tail -n 1)
[ -n "$SHA" ] || { echo "Could not resolve '$REF' in $REPO" >&2; exit 1; }

mkdir -p "$(dirname "$DEST")"
curl -fsSL "https://raw.githubusercontent.com/$REPO/$SHA/$SRC_PATH" -o "$DEST.tmp"
sed "s|^// Vendored-from: .*|// Vendored-from: $REPO $REF ($SHA) - do not hand-edit|" "$DEST.tmp" > "$DEST"
rm "$DEST.tmp"
grep -q "^// Vendored-from: $REPO" "$DEST" || { echo "Stamp line missing from $SRC_PATH" >&2; exit 1; }
echo "Updated $DEST from $REPO $REF ($SHA)"
