#!/usr/bin/env bash
# Download a GGUF from Hugging Face and verify its SHA-256 against the LFS
# object id published by the hub. Weights are never committed or
# redistributed by this project; review the model license on its page first.
# usage: scripts/fetch-model.sh <hf-repo> <file-in-repo> [dest-dir] [revision]
set -euo pipefail
repo="${1:?repo}"; file="${2:?file}"; dest="${3:-${XDG_DATA_HOME:-$HOME/.local/share}/boundedcode/models}"; rev="${4:-main}"
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
