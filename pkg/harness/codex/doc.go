// Package codex drives codex-cli 0.154.0 through its app-server protocol.
// One-shot generation (generate.go) was verified against 0.163.0.
//
// The reference schemas can be regenerated with:
//
//	codex app-server generate-json-schema --out <dir>
//
// Auto permission mode was verified against 0.169.0.
// Permission modes map as follows: approval-required uses untrusted/read-only;
// auto uses on-request/workspace-write with approvalsReviewer auto_review;
// auto-accept-edits uses on-request/workspace-write, which also permits
// sandboxed commands; full-access uses never/danger-full-access.
package codex
