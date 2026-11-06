#!/usr/bin/env bash
# Public preparation uses reviewed local drafts; internal releases retain resume.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [[ -n "${RZM_INTERNAL_S3_BUCKET:-}" ]]; then
  exec python3 "$ROOT_DIR/scripts/release/release_cli.py" cut-release "$@"
fi
if [[ $# == 0 ]]; then
  printf '%s\n' 'Prepare public releases with the rhizome-release skill in Codex or Claude.' 'The skill reviews notes/version and opens a release PR; CI publishes after approval.'
  exec python3 "$ROOT_DIR/scripts/release/prepare_release.py" --help
fi
exec python3 "$ROOT_DIR/scripts/release/prepare_release.py" "$@"
