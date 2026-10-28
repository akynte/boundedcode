#!/usr/bin/env bash
set -euo pipefail

build_all() {
  echo building
}

function release {
  build_all
}

release
