#!/usr/bin/env bash
# Copy a multi-repo fixture into DEST, making each top-level directory its own
# git repository with one commit on main.
# usage: scripts/materialize-fixture.sh benchmarks/fixtures/payment-platform DEST
set -euo pipefail
src="${1:?fixture dir}"; dest="${2:?destination}"
mkdir -p "$dest"
for d in "$src"/*/; do
  name="$(basename "$d")"
  target="$dest/$name"
  rm -rf "$target"
  cp -r "$d" "$target"
  git -C "$target" init -q -b main
  git -C "$target" add -A
  git -C "$target" -c user.name=fixture -c user.email=fixture@example.invalid commit -q -m "initial $name"
  echo "$target"
done
