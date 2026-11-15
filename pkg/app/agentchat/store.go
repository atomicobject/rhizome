package agentchat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

type Store struct {
	db        *sql.DB
	writeMu   *chatWriteLock
	dbPath    string
	closeOnce sync.Once
	closeErr  error
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
	dbPath := paths.ResolveSymlinks(filepath.Join(dir, "sessions.sqlite")).String()
	release, err := sqliteutil.LockSchemaInit(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer release()
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(dbPath, sqliteutil.DSNOptions{TxLockMode: sqliteutil.TxLockImmediate}), sqliteutil.Options{})
	if err != nil {
		return nil, err
	}
	store := &Store{db: db, writeMu: retainChatWriter(dbPath), dbPath: dbPath}
	store.writeMu.Lock()
	err = migration.EnsureDomain(ctx, db, chatMigrationPlan(), migration.EnsureOptions{})
	store.writeMu.Unlock()
	if err != nil {
		releaseChatWriter(dbPath, store.writeMu)
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = s.db.Close()
		releaseChatWriter(s.dbPath, s.writeMu)
	})
	return s.closeErr
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
	_, err := s.execWrite(ctx, `INSERT INTO agent_sessions (id, title, harness, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID, session.Title, kind, session.Model, formatTime(now), formatTime(now))
	return session, err
}

func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, title, harness, harness_session_id, last_turn_id, model, created_at, updated_at, archived FROM agent_sessions WHERE id = ?`, id)
	return scanSession(row)
}

func (s *Store) UpdateSessionHarness(ctx context.Context, id string, kind harness.Kind, harnessSessionID, turnID string) error {
	_, err := s.execWrite(ctx, `UPDATE agent_sessions SET harness = ?, harness_session_id = CASE WHEN ? = '' THEN harness_session_id ELSE ? END, last_turn_id = CASE WHEN ? = '' THEN last_turn_id ELSE ? END, updated_at = ? WHERE id = ?`,
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
	res, err := s.execWrite(ctx, `UPDATE agent_sessions SET archived = 1, updated_at = ? WHERE id = ?`,
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

func (s *Store) AddMessage(ctx context.Context, sessionID, role, content string, view ...*ViewContext) (Message, error) {
	var viewContext *ViewContext
	if len(view) > 0 {
		viewContext = view[0]
	}
	var encoded any
	if viewContext != nil {
		data, err := json.Marshal(viewContext)
		if err != nil {
			return Message{}, err
		}
		encoded = string(data)
	}
	now := time.Now().UTC()
	res, err := s.execWrite(ctx, `INSERT INTO agent_messages (session_id, role, content, created_at, context_json) VALUES (?, ?, ?, ?, ?)`,
		sessionID, role, content, formatTime(now), encoded)
	if err != nil {
		return Message{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Message{}, err
	}
	_, _ = s.execWrite(ctx, `UPDATE agent_sessions SET updated_at = ? WHERE id = ?`, formatTime(now), sessionID)
	return Message{ID: id, SessionID: sessionID, Role: role, Content: content, Context: viewContext, CreatedAt: now}, nil
}

func (s *Store) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, role, content, created_at, context_json FROM agent_messages WHERE session_id = ? ORDER BY id`, sessionID)
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
	res, err := s.execWrite(ctx, `INSERT INTO agent_events (session_id, type, role, content, tool_name, args_json, result_json, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
	var contextJSON sql.NullString
	var created string
	if err := row.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &created, &contextJSON); err != nil {
		return Message{}, err
	}
	if contextJSON.Valid {
		if err := json.Unmarshal([]byte(contextJSON.String), &msg.Context); err != nil {
			return Message{}, err
		}
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
