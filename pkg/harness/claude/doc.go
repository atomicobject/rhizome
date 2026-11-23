// Package claude drives Claude Code 2.1.269 and newer through its stream-json protocol.
//
// Auto permission mode was verified against 2.1.325. SessionOptions.UserMCPConfig
// (no --strict-mcp-config) and `claude mcp list` parsing were verified against
// 2.1.330.
// Permission modes map as follows: approval-required uses default;
// auto uses auto; auto-accept-edits uses acceptEdits; full-access uses bypassPermissions with
// --dangerously-skip-permissions.
package claude
