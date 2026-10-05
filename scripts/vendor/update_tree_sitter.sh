#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VENDOR_DIR="${ROOT_DIR}/vendor/github.com/tree-sitter"

cd "${ROOT_DIR}"

if ! command -v go >/dev/null 2>&1; then
  echo "error: go not found in PATH" >&2
  exit 1
fi

go mod vendor

module_dir() {
  go list -m -f '{{.Dir}}' "$1"
}

sync_dir() {
  local src="$1"
  local dest="$2"

  if [[ ! -d "$src" ]]; then
    echo "error: missing source dir: ${src}" >&2
    exit 1
  fi

  rm -rf "$dest"
  mkdir -p "$(dirname "$dest")"
  cp -R "$src" "$dest"
}

go_tree_sitter_dir="$(module_dir github.com/tree-sitter/go-tree-sitter)"
js_dir="$(module_dir github.com/tree-sitter/tree-sitter-javascript)"
py_dir="$(module_dir github.com/tree-sitter/tree-sitter-python)"
ts_dir="$(module_dir github.com/tree-sitter/tree-sitter-typescript)"
cs_dir="$(module_dir github.com/tree-sitter/tree-sitter-c-sharp)"

sync_dir "${go_tree_sitter_dir}/include" "${VENDOR_DIR}/go-tree-sitter/include"
sync_dir "${go_tree_sitter_dir}/src" "${VENDOR_DIR}/go-tree-sitter/src"

sync_dir "${js_dir}/src" "${VENDOR_DIR}/tree-sitter-javascript/src"
sync_dir "${py_dir}/src" "${VENDOR_DIR}/tree-sitter-python/src"
sync_dir "${cs_dir}/src" "${VENDOR_DIR}/tree-sitter-c-sharp/src"

sync_dir "${ts_dir}/common" "${VENDOR_DIR}/tree-sitter-typescript/common"
sync_dir "${ts_dir}/typescript" "${VENDOR_DIR}/tree-sitter-typescript/typescript"
sync_dir "${ts_dir}/tsx" "${VENDOR_DIR}/tree-sitter-typescript/tsx"

"${ROOT_DIR}/scripts/vendor/patch_go_sqlite3.sh"

echo "updated tree-sitter vendor sources"
