package indexing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestDocLinkIndexerUpgradeRepairsWithoutSendingProviderText(t *testing.T) {
	var texts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		texts.Add(int64(len(request.Input)))
		data := make([]map[string]any, len(request.Input))
		for i := range data {
			data[i] = map[string]any{"index": i, "embedding": []float32{1, 0, 0, 0, 0, 0, 0, 0}}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	ctx := vaultconfig.WithEnv(t.Context(), vaultconfig.Env{HomeDir: t.TempDir()})
	root := t.TempDir()
	writeIndexingParityWorkload(t, root, 2, 2)
	require.NoError(t, os.WriteFile(filepath.Join(root, "src/unit_0000.go"), []byte("package workload\n// Unit0 follows [[project-0001]].\nfunc Unit0(value int) int { return Unit1(value) }\n"), 0600))
	configuration, err := repoconfig.Load(root)
	require.NoError(t, err)
	configuration.Local.NoteEmbeddings.Provider, configuration.Local.CodeEmbeddings.Provider = "openai", "openai"
	configuration.Local.NoteEmbeddings.Endpoint, configuration.Local.CodeEmbeddings.Endpoint = server.URL, server.URL
	require.NoError(t, repoconfig.SaveLocalConfig(root, *configuration.Local))
	configuration, err = repoconfig.Load(root)
	require.NoError(t, err)
	definition := repoconfig.LocalConfigToDefinition(root, configuration.Local)
	opts := UnifiedOptions{VaultPath: root, VaultDef: definition, Config: &configuration, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true, APIKey: "synthetic-only"}
	run := func() {
		t.Helper()
		require.NoError(t, runUnifiedCoreForTest(t, ctx, opts))
	}
	run()
	require.Positive(t, texts.Load(), "the synthetic provider must embed the initial fixture")
	store, closeStore, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	defer closeStore()
	before, err := canonicalWorkloadSnapshot(t, ctx, store, root)
	require.NoError(t, err)
	require.NotEmpty(t, before["doc-links"])
	require.NotEmpty(t, before["vectors"])
	require.NotEmpty(t, before["chunk-vectors"])
	_, err = store.DB().ExecContext(ctx, `DELETE FROM doc_links; UPDATE files SET indexer_version='v1.17.0'; UPDATE index_metadata SET value='v1.17.0' WHERE key='indexer_version'`)
	require.NoError(t, err)
	require.True(t, reverseIndexNeedsRebuild(ctx, store, nil))
	wantReady, inspected := false, false
	opts.afterPreparedOwnershipCommit = func(ctx context.Context) error {
		ready, present, err := store.ReverseIndexBackfillComplete(ctx)
		require.NoError(t, err)
		require.True(t, present)
		require.Equal(t, wantReady, ready, "only the upgrade run may reset the reverse index before parsing")
		inspected = true
		return nil
	}
	texts.Store(0)
	run()
	require.True(t, inspected)
	require.Zero(t, texts.Load(), "repair must not send any note, ontology, or code text")
	after, err := canonicalWorkloadSnapshot(t, ctx, store, root)
	require.NoError(t, err)
	require.Equal(t, before["doc-links"], after["doc-links"])
	for _, table := range []string{"vectors", "chunk-vectors", "code-embeddings", "note-embeddings"} {
		require.Equal(t, before[table], after[table], "preserve %s", table)
	}
	version, _, err := store.IndexerVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, codeanchor.IndexerVersion, version)
	require.False(t, reverseIndexNeedsRebuild(ctx, store, nil), "repair resets the reverse index only once")
	wantReady, inspected = true, false
	run()
	require.True(t, inspected)
	require.Zero(t, texts.Load())
	require.False(t, reverseIndexNeedsRebuild(ctx, store, nil))
}
