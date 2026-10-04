#!/usr/bin/env bash
# Download a GGUF from Hugging Face at a pinned commit and verify its SHA-256
# against the LFS object id published by the hub for that commit. Weights are
# never committed or redistributed by this project; review the model license
# on its page first.
#
# usage: scripts/fetch-model.sh <hf-repo> <file-in-repo> <revision> [dest-dir]
#   revision  full 40-character commit sha (the profile's source.revision in
#             configs/models/*.yaml). Branch names such as `main` are refused
#             because they move.
set -euo pipefail
repo="${1:?usage: fetch-model.sh <hf-repo> <file-in-repo> <revision> [dest-dir]}"
file="${2:?file-in-repo}"
rev="${3:?revision (full commit sha, see source.revision in configs/models/*.yaml)}"
dest="${4:-${XDG_DATA_HOME:-$HOME/.local/share}/boundedcode/models}"
if ! [[ "$rev" =~ ^[0-9a-f]{40}$ ]]; then
  echo "revision must be a full 40-character commit sha, got: $rev" >&2
  exit 2
fi
mkdir -p "$dest"
out="$dest/$(basename "$file")"
sha=$(curl -fsSL "https://huggingface.co/api/models/$repo/tree/$rev?recursive=true" | python3 -c "
import json,sys
for f in json.load(sys.stdin):
    if f['path']==sys.argv[1]: print(f['lfs']['oid'])" "$file")
[ -n "$sha" ] || { echo "file not found in $repo@$rev: $file" >&2; exit 1; }
if [ -f "$out" ] && echo "$sha  $out" | sha256sum -c --quiet - 2>/dev/null; then echo "already present: $out"; exit 0; fi
curl -fL --retry 5 -C - -o "$out.part" "https://huggingface.co/$repo/resolve/$rev/$file"
echo "$sha  $out.part" | sha256sum -c --quiet - || { echo "checksum mismatch" >&2; exit 1; }
mv "$out.part" "$out"
echo "$out"
