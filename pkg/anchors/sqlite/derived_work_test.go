package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestDerivedWorkSurvivesRestartAndRequiresStructuralActivation(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	s, err := Open(dbPath)
	require.NoError(t, err)
	defer s.Close()
	scope := anchors.DerivedScope{Kind: anchors.DerivedNotes, Path: "notes/a.md"}
	work, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{scope})
	require.NoError(t, err)
	pending, err := s.PendingDerivedWork(ctx, time.Now(), 10)
	require.NoError(t, err)
	require.Empty(t, pending)
	require.NoError(t, s.ActivateDerivedWork(ctx, work))
	pending, err = s.PendingDerivedWork(ctx, time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	// Reopen the same database with an independent handle, as startup recovery does.
	reopened, err := Open(dbPath)
	require.NoError(t, err)
	defer reopened.Close()
	pending, err = reopened.PendingDerivedWork(ctx, time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
}

func TestDerivedWorkOldCompletionAndFailureCannotTouchNewerGeneration(t *testing.T) {
	ctx := context.Background()
	s := newOwnershipTransitionStore(t)
	scope := anchors.DerivedScope{Kind: anchors.DerivedCode, Path: "src/a.go"}
	old, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{scope})
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(ctx, old))
	next, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{scope})
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(ctx, next))
	require.NoError(t, s.AckDerivedWork(ctx, old))
	require.NoError(t, s.RetryDerivedWork(ctx, old[0], time.Now().Add(time.Hour)))
	pending, err := s.PendingDerivedWork(ctx, time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, next[0].Generation, pending[0].Generation)
	require.Zero(t, pending[0].Attempt)
	require.NoError(t, s.AckDerivedWork(ctx, next))
	third, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{scope})
	require.NoError(t, err)
	require.Greater(t, third[0].Generation, next[0].Generation)
}

func TestDerivedGlobalInvalidationFencesInflightPathWork(t *testing.T) {
	ctx := context.Background()
	s := newOwnershipTransitionStore(t)
	old, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedOntology, Path: "notes/a.md"}})
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(ctx, old))
	current, err := s.CurrentDerivedWork(ctx, old[0])
	require.NoError(t, err)
	require.True(t, current)
	global, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedOntology}})
	require.NoError(t, err)
	current, err = s.CurrentDerivedWork(ctx, old[0])
	require.NoError(t, err)
	require.False(t, current)
	require.NoError(t, s.AckDerivedWork(ctx, old))
	require.NoError(t, s.RetryDerivedWork(ctx, old[0], time.Now().Add(time.Hour)))
	pending, err := s.PendingDerivedWork(ctx, time.Now(), 10)
	require.NoError(t, err)
	require.Empty(t, pending, "partial schema work must block preparation")
	require.NoError(t, s.ActivateDerivedWork(ctx, global))
	pending, err = s.PendingDerivedWork(ctx, time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	for _, w := range pending {
		require.Equal(t, global[0].Epoch, w.Epoch)
	}
}

func TestDerivedPathEditFencesGlobalAndUnreadyWork(t *testing.T) {
	ctx := context.Background()
	s := newOwnershipTransitionStore(t)
	global, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedOntology}})
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(ctx, global))
	next, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedOntology, Path: "notes/a.md"}})
	require.NoError(t, err)
	current, err := s.CurrentDerivedWork(ctx, global[0])
	require.NoError(t, err)
	require.False(t, current)
	require.NoError(t, s.ActivateDerivedWork(ctx, next))
	current, err = s.CurrentDerivedWork(ctx, global[0])
	require.NoError(t, err)
	require.False(t, current)
}

func TestDerivedUnrelatedPathKeepsInflightResultCurrent(t *testing.T) {
	ctx := context.Background()
	s := newOwnershipTransitionStore(t)
	first, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedOntology, Path: "notes/a.md"}})
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(ctx, first))
	next, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedOntology, Path: "notes/b.md"}})
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(ctx, next))
	current, err := s.CurrentDerivedWork(ctx, first[0])
	require.NoError(t, err)
	require.True(t, current)
}

func TestHasDerivedWorkIncludesUnreadyAndDelayedRecovery(t *testing.T) {
	s := newOwnershipTransitionStore(t)
	ctx := t.Context()
	pending, err := s.HasDerivedWork(ctx, anchors.DerivedGraph)
	require.NoError(t, err)
	require.False(t, pending)
	work, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedGraph}})
	require.NoError(t, err)
	pending, err = s.HasDerivedWork(ctx, anchors.DerivedGraph)
	require.NoError(t, err)
	require.True(t, pending)
	require.NoError(t, s.ActivateDerivedWork(ctx, work))
	require.NoError(t, s.RetryDerivedWork(ctx, work[0], time.Now().Add(time.Hour)))
	due, err := s.PendingDerivedWork(ctx, time.Now(), 10)
	require.NoError(t, err)
	require.Empty(t, due)
	pending, err = s.HasDerivedWork(ctx, anchors.DerivedGraph)
	require.NoError(t, err)
	require.True(t, pending)
	require.NoError(t, s.AckDerivedWork(ctx, work))
	pending, err = s.HasDerivedWork(ctx, anchors.DerivedGraph)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestDerivedCodeWitnessIncludesDocLinkLabelsWithoutSourceChanges(t *testing.T) {
	s := newOwnershipTransitionStore(t)
	ctx := t.Context()
	scope := anchors.DerivedScope{Kind: anchors.DerivedCode, Path: "pkg/code.go"}
	link := anchors.DocLink{SrcType: "note", SrcPath: "notes/context.md", DstKind: "anchor", DstID: "anchor", Label: "Before"}
	require.NoError(t, s.ReplaceDocLinksForPath(ctx, link.SrcPath, []anchors.DocLink{link}))
	before, err := s.DerivedSourceFingerprint(ctx, scope)
	require.NoError(t, err)
	link.Label = "After"
	require.NoError(t, s.ReplaceDocLinksForPath(ctx, link.SrcPath, []anchors.DocLink{link}))
	after, err := s.DerivedSourceFingerprint(ctx, scope)
	require.NoError(t, err)
	require.NotEqual(t, before, after, "related document labels are semantic plan inputs even without a source edit")
}

func TestDerivedV70UpgradePreservesNotesAndVectorsAndRecoversDebt(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	s, err := openWithOptionsAtSchemaVersion(path, OpenOptions{}, 70)
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `INSERT INTO notes(path,title,content_hash,mtime,size,indexed_at) VALUES ('notes/existing.md','Existing','unchanged',1,1,1)`)
	require.NoError(t, err)
	require.NoError(t, s.ReplaceIntelChunks(ctx, []string{"owner"}, []anchors.IntelChunk{{ChunkID: "preserved", OwnerID: "owner", OwnerType: "anchor", ContentHash: "retained", Granularity: "symbol"}}))
	require.NoError(t, s.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"preserved": {1, 0, 0, 0}}))
	require.NoError(t, s.Close())
	s, err = Open(path)
	require.NoError(t, err)
	var title string
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT title FROM notes WHERE path='notes/existing.md'`).Scan(&title))
	require.Equal(t, "Existing", title)
	vectors, err := s.EmbeddingsByChunkIDs(ctx, []string{"preserved"})
	require.NoError(t, err)
	require.Equal(t, embeddings.Embedding{1, 0, 0, 0}, vectors["preserved"])
	work, err := s.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedCode}})
	require.NoError(t, err)
	require.NoError(t, s.ActivateDerivedWork(ctx, work))
	require.NoError(t, s.RetryDerivedWork(ctx, work[0], time.Now().Add(time.Hour)))
	require.NoError(t, s.Close())
	s, err = Open(path)
	require.NoError(t, err)
	defer s.Close()
	recovered, err := s.PendingDerivedWork(ctx, time.Now().Add(2*time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, recovered, 1)
	require.Equal(t, work[0].Generation, recovered[0].Generation)
	require.Equal(t, 1, recovered[0].Attempt)
}

func TestDerivedGraphWitnessIncludesPersistedCodeToNoteEdges(t *testing.T) {
	s := newOwnershipTransitionStore(t)
	ctx := t.Context()
	scope := anchors.DerivedScope{Kind: anchors.DerivedGraph}
	link := anchors.DocLink{SrcType: "code", SrcPath: "pkg/source.go", DstKind: "note", DstPath: "notes/before.md"}
	require.NoError(t, s.ReplaceDocLinksForPath(ctx, link.SrcPath, []anchors.DocLink{link}))
	before, err := s.DerivedSourceFingerprint(ctx, scope)
	require.NoError(t, err)
	link.DstPath = "notes/after.md"
	require.NoError(t, s.ReplaceDocLinksForPath(ctx, link.SrcPath, []anchors.DocLink{link}))
	after, err := s.DerivedSourceFingerprint(ctx, scope)
	require.NoError(t, err)
	require.NotEqual(t, before, after, "persisted coderef edges feed document graph scores")
}
