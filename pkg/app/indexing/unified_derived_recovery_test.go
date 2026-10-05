package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBatchRecoversDelayedGraphDebtWithoutSourceChanges(t *testing.T) {
	ctx := context.Background()
	root, definition := indexedTouchParityVault(t)
	store := openTouchParityStore(t, root)
	initial, err := store.GraphDocScores(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, initial)
	// This is the durable state after structural acknowledgement succeeded but
	// graph publication failed. Its retry deadline must not suppress a batch repair.
	require.NoError(t, store.ReplaceGraphDocScores(ctx, nil))
	work, err := store.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedGraph}})
	require.NoError(t, err)
	require.NoError(t, store.ActivateDerivedWork(ctx, work))
	require.NoError(t, store.RetryDerivedWork(ctx, work[0], time.Now().Add(time.Hour)))
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testNoteMetadataIndexer(t)}))
	repaired, err := store.GraphDocScores(ctx)
	require.NoError(t, err)
	require.Len(t, repaired, len(initial))
	pending, err := store.HasDerivedWork(ctx, anchors.DerivedGraph)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestBatchReplaysGlobalCodeDebtWithFreshRelatedDocumentContext(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "foo.go"), []byte("package fixture\n\n// Foo computes a result.\nfunc Foo() int { return 1 }\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "guide.md"), []byte("# Guide\n"), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"notes/*.md\"]\ncode:\n  enabled: true\n  goRoots: [src]\nnoteEmbeddings:\n  enabled: false\ncodeEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\n"), 0600))
	definition, err := obsidian.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	opts := UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testNoteMetadataIndexer(t), SkipConfigPersistence: true}
	require.NoError(t, RunUnifiedCore(ctx, opts))
	store := openTouchParityStore(t, root)
	var anchorID string
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT anchor_id FROM intel_code_anchors WHERE path='src/foo.go' AND symbol='Foo'`).Scan(&anchorID))
	initialChunks, err := store.IntelChunksByOwners(ctx, []string{anchorID})
	require.NoError(t, err)
	require.NotEmpty(t, initialChunks)
	// Final structural context may change independently of source ingestion. An
	// unchanged code owner still needs the exact global obligation replayed.
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "notes/guide.md", []anchors.DocLink{{SrcType: "note", SrcPath: "notes/guide.md", DstKind: "anchor", DstID: anchorID, Label: "Updated guidance"}}))
	work, err := store.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: anchors.DerivedCode}})
	require.NoError(t, err)
	require.NoError(t, store.ActivateDerivedWork(ctx, work))
	require.NoError(t, store.RetryDerivedWork(ctx, work[0], time.Now().Add(time.Hour)))
	require.NoError(t, RunUnifiedCore(ctx, opts))
	chunks, err := store.IntelChunksByOwners(ctx, []string{anchorID})
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	require.NotEqual(t, initialChunks[0].ContentHash, chunks[0].ContentHash, "unchanged owner must reflect fresh related-document synthesis")
	pending, err := store.HasDerivedWork(ctx, anchors.DerivedCode)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestBatchRecoversDelayedNoteDebtWithoutSourceChanges(t *testing.T) {
	for _, typed := range []bool{false, true} {
		name := "raw"
		if typed {
			name = "ontology"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0755))
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "guide.md"), []byte("# Guide\n\nStable body evidence for recovery.\n"), 0600))
			if typed {
				require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte("type Guide @node(paths: [\"notes/*.md\"]) { title: String }\n"), 0600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"notes/*.md\"]\nnoteEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\ncodeEmbeddings:\n  enabled: false\n"), 0600))
			definition, err := obsidian.LoadDefinitionFromPath(root)
			require.NoError(t, err)
			opts := UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testNoteMetadataIndexer(t), SkipConfigPersistence: true}
			require.NoError(t, RunUnifiedCore(ctx, opts))
			store := openTouchParityStore(t, root)
			var count int
			require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM intel_embeddings`).Scan(&count))
			require.Positive(t, count)
			// Failed derived publication leaves structurally current inputs and ready
			// retry debt. Source discovery itself has nothing new to ingest.
			_, err = store.DB().ExecContext(ctx, `DELETE FROM intel_embeddings`)
			require.NoError(t, err)
			if typed {
				_, err = store.DB().ExecContext(ctx, `DELETE FROM ontology_node_embedding_state`)
			} else {
				_, err = store.DB().ExecContext(ctx, `DELETE FROM emb_chunk_embeddings`)
			}
			require.NoError(t, err)
			kind := anchors.DerivedNotes
			if typed {
				kind = anchors.DerivedOntology
			}
			work, err := store.MarkDerivedDirty(ctx, []anchors.DerivedScope{{Kind: kind}})
			require.NoError(t, err)
			require.NoError(t, store.ActivateDerivedWork(ctx, work))
			require.NoError(t, store.RetryDerivedWork(ctx, work[0], time.Now().Add(time.Hour)))
			require.NoError(t, RunUnifiedCore(ctx, opts))
			require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM intel_embeddings`).Scan(&count))
			require.Positive(t, count, "batch must repair vectors before acknowledging ready debt")
			pending, err := store.HasDerivedWork(ctx, kind)
			require.NoError(t, err)
			require.False(t, pending)
		})
	}
}
