package agentchat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

// OpenStore opens the per-vault agent chat database.
//
// The database lives under .rhizome/agent rather than the shared intel store so
// chat transcripts and streamed UI events can be pruned/backed up independently
// from rebuildable retrieval indexes.
func OpenStore(ctx context.Context, vaultPath string) (*Store, error) {
	if strings.TrimSpace(vaultPath) == "" {
		return nil, fmt.Errorf("vault path is required")
	}
	dir := filepath.Join(vaultPath, ".rhizome", "agent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", sqliteutil.DSN(filepath.Join(dir, "sessions.sqlite")))
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
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
		// Migrations stay idempotent because the service opens on demand from web
		// and tests. Keep schema changes append-only here unless a separate
		// migration version table is introduced.
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
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
		exists, err := s.sessionColumnExists(ctx, column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, column.sql); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) sessionColumnExists(ctx context.Context, name string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(agent_sessions)`)
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

func (s *Store) CreateSession(ctx context.Context, title string, kind harness.Kind, model string) (Session, error) {
	now := time.Now().UTC()
	session := Session{
		ID:        newID("agt"),
		Title:     strings.TrimSpace(title),
		Harness:   kind,
		Model:     strings.TrimSpace(model),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if session.Title == "" {
		session.Title = "New chat"
	}
	session.ReadOnly = kind == ""
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_sessions (id, title, harness, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID, session.Title, kind, session.Model, formatTime(now), formatTime(now))
	return session, err
}

func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, title, harness, harness_session_id, last_turn_id, model, created_at, updated_at, archived FROM agent_sessions WHERE id = ?`, id)
	return scanSession(row)
}

func (s *Store) UpdateSessionHarness(ctx context.Context, id string, kind harness.Kind, harnessSessionID, turnID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET harness = ?, harness_session_id = CASE WHEN ? = '' THEN harness_session_id ELSE ? END, last_turn_id = CASE WHEN ? = '' THEN last_turn_id ELSE ? END, updated_at = ? WHERE id = ?`,
		kind, harnessSessionID, harnessSessionID, turnID, turnID, formatTime(time.Now().UTC()), id)
	return err
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, harness, harness_session_id, last_turn_id, model, created_at, updated_at, archived FROM agent_sessions WHERE archived = 0 ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) ArchiveSession(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET archived = 1, updated_at = ? WHERE id = ?`,
		formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) AddMessage(ctx context.Context, sessionID, role, content string) (Message, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO agent_messages (session_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, role, content, formatTime(now))
	if err != nil {
		return Message{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Message{}, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE agent_sessions SET updated_at = ? WHERE id = ?`, formatTime(now), sessionID)
	return Message{ID: id, SessionID: sessionID, Role: role, Content: content, CreatedAt: now}, nil
}

func (s *Store) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, role, content, created_at FROM agent_messages WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func (s *Store) AddEvent(ctx context.Context, event Event) (Event, error) {
	now := time.Now().UTC()
	resultJSON, err := marshalAny(event.Result)
	if err != nil {
		return Event{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO agent_events (session_id, type, role, content, tool_name, args_json, result_json, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.SessionID, event.Type, event.Role, event.Content, event.ToolName, "", resultJSON, event.Error, formatTime(now))
	if err != nil {
		return Event{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Event{}, err
	}
	event.ID = id
	event.CreatedAt = now
	return event, nil
}

func (s *Store) EventsAfter(ctx context.Context, sessionID string, afterID int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, type, role, content, tool_name, args_json, result_json, error, created_at FROM agent_events WHERE session_id = ? AND id > ? ORDER BY id`, sessionID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (Session, error) {
	var s Session
	var created, updated string
	var archived int
	if err := row.Scan(&s.ID, &s.Title, &s.Harness, &s.HarnessSessionID, &s.LastTurnID, &s.Model, &created, &updated, &archived); err != nil {
		return Session{}, err
	}
	s.CreatedAt = parseTime(created)
	s.UpdatedAt = parseTime(updated)
	s.Archived = archived != 0
	s.ReadOnly = s.Harness == ""
	return s, nil
}

func scanMessage(row scanner) (Message, error) {
	var msg Message
	var created string
	if err := row.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &created); err != nil {
		return Message{}, err
	}
	msg.CreatedAt = parseTime(created)
	return msg, nil
}

func scanEvent(row scanner) (Event, error) {
	var event Event
	var argsJSON, resultJSON, created string
	if err := row.Scan(&event.ID, &event.SessionID, &event.Type, &event.Role, &event.Content, &event.ToolName, &argsJSON, &resultJSON, &event.Error, &created); err != nil {
		return Event{}, err
	}
	_ = argsJSON
	event.Result = unmarshalEventResult(resultJSON)
	event.CreatedAt = parseTime(created)
	return event, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return t
}

func marshalAny(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := json.Marshal(v)
	return string(data), err
}

func unmarshalEventResult(raw string) *EventResult {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out EventResult
	_ = json.Unmarshal([]byte(raw), &out)
	return &out
}
