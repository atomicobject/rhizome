package embedindex

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/stretchr/testify/require"
)

// ctxCheckingCodeIndex fails EnsureSchema when its context is done, as a
// cancelled or busy first setup call would.
type ctxCheckingCodeIndex struct{ codeindex.Index }

func (i ctxCheckingCodeIndex) EnsureSchema(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return i.Index.EnsureSchema(ctx)
}

type ctxCheckingNoteIndex struct{ embeddings.Index }

func (i ctxCheckingNoteIndex) EnsureSchema(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return i.Index.EnsureSchema(ctx)
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestSyncerEnsureReadyRetriesAfterCancelledSetup(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := sqlite.OpenCode(context.Background(), sqlite.File(filepath.Join(t.TempDir(), "code.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	s := &Syncer{Index: ctxCheckingCodeIndex{store}, Provider: prov}

	require.ErrorIs(t, s.ensureReady(cancelledContext()), context.Canceled)
	require.NoError(t, s.ensureReady(context.Background()))
}

func TestNoteSyncerEnsureReadyRetriesAfterCancelledSetup(t *testing.T) {
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store, err := sqlite.OpenNotes(context.Background(), sqlite.File(filepath.Join(t.TempDir(), "notes.db")), sqlite.Options{Access: sqlite.WriteIndex, Dimensions: prov.Dimensions()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	s := &NoteSyncer{Index: ctxCheckingNoteIndex{store}, Provider: prov}

	require.ErrorIs(t, s.ensureReady(cancelledContext()), context.Canceled)
	require.NoError(t, s.ensureReady(context.Background()))
}
