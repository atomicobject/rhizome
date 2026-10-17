package store

import (
	"context"
)

func createSessionReservationSchema(ctx context.Context, db execer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS mcp_session_item_reservations (
			session_id TEXT NOT NULL CHECK (session_id != ''),
			item_key TEXT NOT NULL CHECK (item_key != ''),
			fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
			reservation_id TEXT NOT NULL CHECK (reservation_id != ''),
			reserved_at INTEGER NOT NULL CHECK (reserved_at >= 0),
			PRIMARY KEY (session_id, item_key)
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_session_item_reservations_owner
			ON mcp_session_item_reservations(session_id, reservation_id);`,
		`CREATE TABLE IF NOT EXISTS mcp_session_maintenance (
			maintenance_key TEXT PRIMARY KEY CHECK (maintenance_key != ''),
			next_due_at INTEGER NOT NULL CHECK (next_due_at >= 0)
		) WITHOUT ROWID, STRICT;`,
		`INSERT INTO mcp_session_maintenance (maintenance_key, next_due_at)
			VALUES ('cleanup', 0) ON CONFLICT(maintenance_key) DO NOTHING;`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
