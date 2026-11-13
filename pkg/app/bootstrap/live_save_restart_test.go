package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/stretchr/testify/require"
)

func TestLiveSaveThenRestartReembedsWithoutFileEvent(t *testing.T) {
	rt, w, provider := liveWorkloadWatcher(t)
	w.derived.close()
	path := "notes/project-0000.md"
	var oldHash string
	require.NoError(t, rt.IntelStore().DB().QueryRowContext(t.Context(), `SELECT chunk_text_hash FROM ontology_node_embedding_state WHERE note_path=?`, path).Scan(&oldHash))
	calls := provider.calls.Load()
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Saved before restart")), 0o600))
	saved, err := validationproduct.RefreshValidationProjection(t.Context(), validationproduct.ValidationProjectionRequest{
		Lane: w.lane, VaultPath: rt.VaultPath, VaultDef: w.vaultDef,
		NoteMetadata: w.noteMetadataIndexer, Target: validationproduct.ValidationProjectionLive,
		ExactPaths: &validationproduct.ValidationProjectionPaths{Changed: []paths.NotePath{paths.NotePath(path)}},
	})
	require.NoError(t, err)
	require.NoError(t, saved.Close())
	require.Equal(t, calls, provider.calls.Load(), "save only publishes structural projection")
	var tickets int
	require.NoError(t, rt.IntelStore().DB().QueryRowContext(t.Context(), `SELECT count(*) FROM derived_work`).Scan(&tickets))
	require.Zero(t, tickets, "save has not received its file event to create derived tickets")
	var savedHash string
	require.NoError(t, rt.IntelStore().DB().QueryRowContext(t.Context(), `SELECT chunk_text_hash FROM ontology_node_embedding_state WHERE note_path=?`, path).Scan(&savedHash))
	require.Equal(t, oldHash, savedHash, "save retains the previous embedding until indexing")
	root := rt.VaultPath
	w.lane.Close()
	require.NoError(t, rt.Close())

	// Restart the runtime with real leader startup and the deterministic provider.
	// WatchHub stays disabled, so only the restart pass can discover this save.
	restarted, err := NewLiveRuntime(t.Context(), LiveOptions{VaultName: root, DisableWatchHub: true, DisableSessionStore: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restarted.Close()) })
	require.NoError(t, restarted.WaitForCodeIndex(t.Context()))
	require.Eventually(t, func() bool {
		var current int
		err := restarted.IntelStore().DB().QueryRowContext(t.Context(), `SELECT count(*) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id JOIN intel_embeddings e ON e.chunk_id=s.chunk_id JOIN intel_chunks c ON c.chunk_id=e.chunk_id JOIN intel_embeddings_vec_d8 v ON v.chunk_id=c.id WHERE s.note_path=? AND s.node_structure_fingerprint=n.structural_fingerprint AND s.chunk_text_hash != ?`, path, oldHash).Scan(&current)
		return err == nil && current > 0
	}, 30*time.Second, 10*time.Millisecond, "restart must publish a vector for the saved content without a file event")
}
