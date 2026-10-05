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
