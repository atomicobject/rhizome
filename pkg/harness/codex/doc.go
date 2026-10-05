// Package codex drives codex-cli 0.154.0 through its app-server protocol.
//
// The reference schemas can be regenerated with:
//
//	codex app-server generate-json-schema --out <dir>
//
// Permission modes map as follows: approval-required uses untrusted/read-only;
// auto-accept-edits uses on-request/workspace-write, which also permits
// sandboxed commands; full-access uses never/danger-full-access.
package codex
