package indexing

import (
	"fmt"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func BenchmarkUnifiedDeterministicWorkload(b *testing.B) {
	for _, notes := range []int{128, 512} {
		b.Run(fmt.Sprintf("notes=%d_code=32", notes), func(b *testing.B) {
			var elapsed time.Duration
			for range b.N {
				b.StopTimer()
				root := b.TempDir()
				indexingworkload.Write(b, root, notes, 32)
				definition, err := obsidian.LoadDefinitionFromPath(root)
				require.NoError(b, err)
				opts := UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testNoteMetadataIndexerForHelper(), SkipConfigPersistence: true}
				b.StartTimer()
				start := time.Now()
				err = RunUnifiedCore(b.Context(), opts)
				elapsed += time.Since(start)
				b.StopTimer()
				require.NoError(b, err)
				store, closeStore, err := obsidian.OpenIntelStore(root, false)
				require.NoError(b, err)
				paths, err := store.CurrentNoteMetadataPaths(b.Context())
				require.NoError(b, err)
				require.Len(b, paths, notes)
				nodes, err := store.AllOntologyNodes(b.Context())
				require.NoError(b, err)
				require.Len(b, nodes, notes)
				codePaths, err := store.IndexedFilePaths(b.Context())
				require.NoError(b, err)
				require.Len(b, codePaths, 32)
				_, pending, err := store.PendingOwnershipReconciliation(b.Context())
				require.NoError(b, err)
				require.False(b, pending)
				var derivedDebt int
				require.NoError(b, store.DB().QueryRowContext(b.Context(), `SELECT count(*) FROM derived_work`).Scan(&derivedDebt))
				require.Zero(b, derivedDebt, "completed batch must leave no derived obligations")
				var ontologyVectors, codeVectors int
				require.NoError(b, store.DB().QueryRowContext(b.Context(), `SELECT count(*) FROM intel_embeddings e JOIN intel_chunks c ON c.chunk_id=e.chunk_id JOIN intel_embeddings_vec_d8 v ON v.chunk_id=c.id WHERE c.owner_type='ontology_node'`).Scan(&ontologyVectors))
				require.GreaterOrEqual(b, ontologyVectors, notes)
				require.NoError(b, store.DB().QueryRowContext(b.Context(), `SELECT count(*) FROM intel_embeddings e JOIN intel_chunks c ON c.chunk_id=e.chunk_id JOIN intel_embeddings_vec_d8 v ON v.chunk_id=c.id WHERE c.owner_type='anchor'`).Scan(&codeVectors))
				require.GreaterOrEqual(b, codeVectors, 32)
				var mismatchedFingerprints int
				require.NoError(b, store.DB().QueryRowContext(b.Context(), `SELECT count(*) FROM ontology_node_embedding_state WHERE provider != 'test' OR model != 'test' OR source_content_hash='' OR node_structure_fingerprint=''`).Scan(&mismatchedFingerprints))
				require.Zero(b, mismatchedFingerprints)
				b.ReportMetric(float64(ontologyVectors+codeVectors), "vectors")
				closeStore()
			}
			b.ReportMetric(float64((notes+32)*b.N)/elapsed.Seconds(), "files/s")
		})
	}
}
