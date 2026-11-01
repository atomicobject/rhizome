package indexing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// Plain notes publish their vectors to Intel, so their reuse must come from
// the Intel vector cache: an edit sends only the edited section, and a revert
// or a projection-version bump sends nothing.
func TestPlainNoteEditRevertAndReprojectionReuseCachedVectors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	guide := "## Setup\n\nInstall the tools first.\n\n## Usage\n\nRun the index after edits.\n"
	write(".rhizome/config.yml", "notes:\n  includes: [\"notes/*.md\"]\nnoteEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\ncodeEmbeddings:\n  enabled: false\n")
	write("notes/guide.md", guide)

	definition, err := repoconfig.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	opts := UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true}
	providerTexts := func() map[string]int64 {
		t.Helper()
		collector := indexingperf.New()
		require.NoError(t, runUnifiedCoreForTest(t, indexingperf.WithCollector(context.Background(), collector), opts))
		byPhase := map[string]int64{}
		for _, counter := range collector.Snapshot().Counters {
			if counter.Name == string(indexingperf.ProviderTexts) && counter.Total > 0 {
				byPhase[counter.Phase] += counter.Total
			}
		}
		return byPhase
	}

	require.Equal(t, map[string]int64{"embed_notes": 2}, providerTexts(), "the first index embeds each plain-note section once")

	write("notes/guide.md", strings.Replace(guide, "Install the tools first.", "Install the tools before anything else.", 1))
	require.Equal(t, map[string]int64{"embed_notes": 1}, providerTexts(), "only the edited section may embed")

	write("notes/guide.md", guide)
	require.Empty(t, providerTexts(), "reverting a plain-note edit must reuse its cached vectors")

	exec := func(stmts ...string) {
		t.Helper()
		store, closeStore, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
		require.NoError(t, err)
		defer closeStore()
		for _, stmt := range stmts {
			_, err = store.DB().ExecContext(context.Background(), stmt)
			require.NoError(t, err)
		}
	}
	exec(`UPDATE note_projection_state SET projection_version = 'markdown-projection-v4'`)
	require.Empty(t, providerTexts(), "a projection-version bump over unchanged plain notes must reuse cached vectors")

	// A database from before the cache has published plain-note vectors but no
	// cache rows. The upgrade backfills them, so editing one section after it
	// sends only that section.
	exec(`DROP TABLE intel_embedding_cache`, `UPDATE rzm_migration_state SET version = 77 WHERE domain = 'intel'`)
	write("notes/guide.md", strings.Replace(guide, "Run the index after edits.", "Run the index after every edit.", 1))
	require.Equal(t, map[string]int64{"embed_notes": 1}, providerTexts(), "an upgrade must backfill plain-note vectors")
}
