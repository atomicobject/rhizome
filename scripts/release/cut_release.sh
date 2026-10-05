#!/usr/bin/env bash
set -euo pipefail

# Keep the familiar maintainer entrypoint while Python owns all phase state.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
exec python3 "$ROOT_DIR/scripts/release/release_cli.py" cut-release "$@"
