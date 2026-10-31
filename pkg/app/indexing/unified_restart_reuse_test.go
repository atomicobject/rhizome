package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// TestSameBuildCatchUpReusesUnchangedEmbeddings models a runtime restart: boot
// catch-up is a fresh RunUnifiedCore over the existing index. Every indexing
// EmbedTexts call site counts provider.texts, so a zero count proves the
// provider saw no text for unchanged ontology bodies, notes, or code.
func TestSameBuildCatchUpReusesUnchangedEmbeddings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	write(".rhizome/config.yml", "notes:\n  includes: [\"notes/*.md\"]\ncode:\n  enabled: true\n  goRoots: [src]\nnoteEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\ncodeEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\n")
	write(".rhizome/ontology/schema.graphql", "type Decision @node(paths: [\"notes/decision-*.md\"]) { summary: String }\n")
	write("notes/decision-cache.md", "---\nsummary: Cache embeddings\n---\n# Cache embeddings\n\nReuse vectors when content is unchanged.\n\n## Consequences\n\nRestarts stay cheap.\n")
	write("notes/decision-lane.md", "# Serialize indexing\n\nOne lane owns the index lock.\n")
	write("notes/guide.md", "# Guide\n\nUntyped guidance that uses fallback ontology coverage.\n")
	write("src/foo.go", "package fixture\n\n// Foo computes a result.\nfunc Foo() int { return 1 }\n\n// Bar computes another result.\nfunc Bar() int { return 2 }\n")

	definition, err := repoconfig.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	opts := UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true}
	providerTexts := func() (int64, map[string]int64) {
		t.Helper()
		collector := indexingperf.New()
		require.NoError(t, runUnifiedCoreForTest(t, indexingperf.WithCollector(context.Background(), collector), opts))
		var total int64
		byPhase := map[string]int64{}
		for _, counter := range collector.Snapshot().Counters {
			if counter.Name == string(indexingperf.ProviderTexts) {
				total += counter.Total
				byPhase[counter.Phase] += counter.Total
			}
		}
		return total, byPhase
	}

	initial, _ := providerTexts()
	require.Positive(t, initial, "the first index must embed the fixture")

	restart, byPhase := providerTexts()
	require.Zero(t, restart, "same-build catch-up must not re-embed unchanged content; provider texts by phase: %v", byPhase)

	// A build that bumps OntologyMaterializationVersion makes catch-up rebuild
	// the ontology and re-plan every body, so this run exercises the stored
	// embedding-state comparison rather than the unchanged-source skip.
	store, closeStore, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	_, err = store.DB().ExecContext(context.Background(), `UPDATE ontology_schema_state SET materialization_version = materialization_version - 1`)
	closeStore()
	require.NoError(t, err)
	rebuilt, byPhase := providerTexts()
	require.Zero(t, rebuilt, "an ontology rebuild over unchanged content must reuse stored embeddings; provider texts by phase: %v", byPhase)

	write("notes/decision-lane.md", "# Serialize indexing\n\nOne lane owns the index lock, and explicit index preempts it.\n")
	write("src/foo.go", "package fixture\n\n// Foo computes a result.\nfunc Foo() int { return 1 }\n\n// Bar computes a different result.\nfunc Bar() int { return 3 }\n")
	changed, byPhase := providerTexts()
	require.Positive(t, changed, "a real content change must embed")
	require.Less(t, changed, initial, "only changed content may embed; provider texts by phase: %v", byPhase)
}
