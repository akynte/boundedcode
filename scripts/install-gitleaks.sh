#!/usr/bin/env bash
# CI helper: install the pinned gitleaks into $1.
exec "$(dirname "$0")/install-deps.sh" "${1:?bin dir}" gitleaks
