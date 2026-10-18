package store

import (
	"context"
)

func intelBaselineStep13(ctx context.Context, db execer) error { // v13 -> v14
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS mcp_sessions (
				session_id TEXT PRIMARY KEY CHECK (session_id != ''),
				created_at INTEGER NOT NULL CHECK (created_at >= 0),
				last_seen_at INTEGER NOT NULL CHECK (last_seen_at >= 0)
			) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS mcp_session_items (
				session_id TEXT NOT NULL CHECK (session_id != ''),
				item_key TEXT NOT NULL CHECK (item_key != ''),
				fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
				sent_at INTEGER NOT NULL CHECK (sent_at >= 0),
				PRIMARY KEY (session_id, item_key)
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_sessions_last_seen ON mcp_sessions(last_seen_at);`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_session_items_session ON mcp_session_items(session_id);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
