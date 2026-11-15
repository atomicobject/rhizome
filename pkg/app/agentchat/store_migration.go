package agentchat

import (
	"context"
	"database/sql"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

func chatMigrationPlan() migration.DomainPlan {
	return migration.DomainPlan{Domain: "agent_chat", Target: 2, Steps: []migration.Step{
		{To: 1, Name: "chat_base", Run: migrateChatBase},
		{To: 2, Name: "message_context", Run: func(ctx context.Context, tx *sql.Tx) error {
			exists, err := chatColumnExists(ctx, tx, "agent_messages", "context_json")
			if err != nil {
				return err
			}
			if exists {
				return nil
			}
			_, err = tx.ExecContext(ctx, `ALTER TABLE agent_messages ADD COLUMN context_json TEXT CHECK(context_json IS NULL OR json_valid(context_json))`)
			return err
		}},
	}}
}

func migrateChatBase(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS agent_sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			engine TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			harness TEXT NOT NULL DEFAULT '',
			harness_session_id TEXT NOT NULL DEFAULT '',
			last_turn_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			archived INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS agent_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(session_id) REFERENCES agent_sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS agent_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			type TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			tool_name TEXT NOT NULL DEFAULT '',
			args_json TEXT NOT NULL DEFAULT '',
			result_json TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			FOREIGN KEY(session_id) REFERENCES agent_sessions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_session ON agent_messages(session_id, id)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_events_session ON agent_events(session_id, id)`,
	}
	for _, stmt := range stmts {
		// Adopt pre-versioned chat databases without changing their rows.
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		sql  string
	}{
		{name: "harness", sql: `ALTER TABLE agent_sessions ADD COLUMN harness TEXT NOT NULL DEFAULT ''`},
		{name: "harness_session_id", sql: `ALTER TABLE agent_sessions ADD COLUMN harness_session_id TEXT NOT NULL DEFAULT ''`},
		{name: "last_turn_id", sql: `ALTER TABLE agent_sessions ADD COLUMN last_turn_id TEXT NOT NULL DEFAULT ''`},
	} {
		exists, err := chatColumnExists(ctx, tx, "agent_sessions", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, column.sql); err != nil {
				return err
			}
		}
	}
	return nil
}

func chatColumnExists(ctx context.Context, tx *sql.Tx, table, name string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if columnName == name {
			return true, nil
		}
	}
	return false, rows.Err()
}
