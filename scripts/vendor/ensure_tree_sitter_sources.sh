#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VENDOR_DIR="${ROOT_DIR}/vendor/github.com/tree-sitter"

cd "${ROOT_DIR}"

if [[ ! -d "${ROOT_DIR}/vendor" ]]; then
  echo "error: vendor directory not found; run 'go mod vendor' first" >&2
  exit 1
fi

missing=()

require_dir() {
  local path="$1"
  if [[ ! -d "$path" ]]; then
    missing+=("$path")
  fi
}

require_dir "${VENDOR_DIR}/go-tree-sitter/include"
require_dir "${VENDOR_DIR}/go-tree-sitter/src"
require_dir "${VENDOR_DIR}/tree-sitter-javascript/src"
require_dir "${VENDOR_DIR}/tree-sitter-python/src"
require_dir "${VENDOR_DIR}/tree-sitter-c-sharp/src"
require_dir "${VENDOR_DIR}/tree-sitter-typescript/common"
require_dir "${VENDOR_DIR}/tree-sitter-typescript/typescript"
require_dir "${VENDOR_DIR}/tree-sitter-typescript/tsx"

if (( ${#missing[@]} > 0 )); then
  echo "error: missing tree-sitter vendor sources:" >&2
  for path in "${missing[@]}"; do
    echo "  - ${path}" >&2
  done
  echo "hint: run ./scripts/vendor/update_tree_sitter.sh to populate vendor sources" >&2
  exit 1
fi

"${ROOT_DIR}/scripts/vendor/patch_go_sqlite3.sh"

echo "verified tree-sitter vendor sources"
