#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VENDOR_DIR="${ROOT_DIR}/vendor/github.com/mattn/go-sqlite3"
TARGET_FILE="${VENDOR_DIR}/sqlite3_opt_fts5_rhizome.go"

if [[ ! -d "$VENDOR_DIR" ]]; then
  echo "error: vendor path not found: ${VENDOR_DIR}" >&2
  echo "hint: run 'go mod vendor' first" >&2
  exit 1
fi

# If upstream ever enables FTS5 unconditionally, avoid adding a redundant shim.
if rg -q --no-messages "SQLITE_ENABLE_FTS5" "${VENDOR_DIR}/sqlite3_opt_fts5.go"; then
  if ! rg -q --no-messages "//go:build" "${VENDOR_DIR}/sqlite3_opt_fts5.go"; then
    echo "info: upstream sqlite3_opt_fts5.go already enables FTS5 without build tags; skipping shim"
    exit 0
  fi
fi

cat >"$TARGET_FILE" <<'EOF'
//go:build cgo && !fts5 && !sqlite_fts5
// +build cgo,!fts5,!sqlite_fts5

package sqlite3

/*
#cgo CFLAGS: -DSQLITE_ENABLE_FTS5
#cgo LDFLAGS: -lm
*/
import "C"
EOF

echo "patched: ${TARGET_FILE}"
