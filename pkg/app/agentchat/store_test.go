package agentchat

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestStorePersistsHarnessSessionsMessagesAndEvents(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	store, err := OpenStore(ctx, vaultPath)
	require.NoError(t, err)
	session, err := store.CreateSession(ctx, "Map retrieval gaps", harness.KindCodex, "gpt-5.6")
	require.NoError(t, err)
	require.False(t, session.ReadOnly)
	_, err = store.AddMessage(ctx, session.ID, "user", "Where should agent tools live?")
	require.NoError(t, err)
	_, err = store.AddEvent(ctx, Event{SessionID: session.ID, Type: string(harness.EventCommandExecution), Result: &EventResult{Command: "rg tools", Status: "completed"}})
	require.NoError(t, err)
	require.NoError(t, store.UpdateSessionHarness(ctx, session.ID, harness.KindCodex, "thread-1", "turn-1"))
	require.NoError(t, store.UpdateSessionHarness(ctx, session.ID, harness.KindCodex, "thread-2", ""))
	require.NoError(t, store.Close())

	reopened, err := OpenStore(ctx, vaultPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	persisted, err := reopened.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, "thread-2", persisted.HarnessSessionID)
	require.Equal(t, "turn-1", persisted.LastTurnID)
	messages, err := reopened.Messages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "user", messages[0].Role)
	require.Equal(t, "Where should agent tools live?", messages[0].Content)
	events, err := reopened.EventsAfter(ctx, session.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "rg tools", events[0].Result.Command)
}

func TestStoreMigratesOldSchemaIdempotently(t *testing.T) {
	ctx := context.Background()
	vaultPath := t.TempDir()
	dir := filepath.Join(vaultPath, ".rhizome", "agent")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	db, err := sql.Open("sqlite3", filepath.Join(dir, "sessions.sqlite"))
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE agent_sessions (
		id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', engine TEXT NOT NULL DEFAULT '',
		provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL, archived INTEGER NOT NULL DEFAULT 0)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO agent_sessions (id, title, engine, provider, model, created_at, updated_at) VALUES ('old', 'Old chat', 'openai', 'openai', 'gpt', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	for range 2 {
		store, openErr := OpenStore(ctx, vaultPath)
		require.NoError(t, openErr)
		old, getErr := store.GetSession(ctx, "old")
		require.NoError(t, getErr)
		require.True(t, old.ReadOnly)
		require.Empty(t, old.Harness)
		require.NoError(t, store.Close())
	}
}

func TestStoreContextSurvivesUpgradeAndReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := OpenStore(ctx, root)
	require.NoError(t, err)
	session, err := store.CreateSession(ctx, "Context", harness.KindCodex, "")
	require.NoError(t, err)
	_, err = store.AddMessage(ctx, session.ID, "user", "old")
	require.NoError(t, err)
	// Recreate the previous messages schema and remove only the new migration witness.
	_, err = store.db.Exec(`ALTER TABLE agent_messages DROP COLUMN context_json; UPDATE rzm_migration_state SET version = 1 WHERE domain = 'agent_chat'`)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	store, err = OpenStore(ctx, root)
	require.NoError(t, err)
	view := &ViewContext{Kind: "note", Title: "A", Route: "/notes", Path: "notes/a.md"}
	_, err = store.AddMessage(ctx, session.ID, "user", "  unchanged  ", view)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	store, err = OpenStore(ctx, root)
	require.NoError(t, err)
	defer store.Close()
	messages, err := store.Messages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Nil(t, messages[0].Context)
	require.Equal(t, view, messages[1].Context)
	require.Equal(t, "  unchanged  ", messages[1].Content)
}

func TestChatStoresPreserveConcurrentMessages(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	first, err := OpenStore(ctx, root)
	require.NoError(t, err)
	defer first.Close()
	second, err := OpenStore(ctx, root)
	require.NoError(t, err)
	defer second.Close()
	session, err := first.CreateSession(ctx, "Concurrent", harness.KindCodex, "")
	require.NoError(t, err)
	done := make(chan error, 2)
	for _, store := range []*Store{first, second} {
		go func() {
			for range 20 {
				if _, err := store.AddMessage(ctx, session.ID, "user", "message"); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}()
	}
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	messages, err := first.Messages(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, messages, 40)
}
