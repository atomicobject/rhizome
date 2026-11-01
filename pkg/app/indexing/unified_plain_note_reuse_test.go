package indexing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/embeddings"
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
	// cache rows. At the default section limit the first index after the
	// upgrade backfills them, so editing one section sends only that section.
	exec(upgradeFromV77...)
	write("notes/guide.md", strings.Replace(guide, "Run the index after edits.", "Run the index after every edit.", 1))
	require.Equal(t, map[string]int64{"embed_notes": 1}, providerTexts(), "an upgrade must backfill plain-note vectors")
}

// upgradeFromV77 returns a current database to the state a v77 database
// upgrades from: no vector cache and no plain-note backfill marker.
var upgradeFromV77 = []string{
	`DROP TABLE intel_embedding_cache`,
	`UPDATE rzm_migration_state SET version = 77 WHERE domain = 'intel'`,
	`DELETE FROM index_metadata WHERE key = 'intel_embedding_cache_note_backfill'`,
}

// Builds before the cache hashed plain-note chunks at the default section
// limit whatever limit embedded them. Under a non-default limit the upgrade
// backfill must not key a truncated vector by that hash: when the limit later
// returns to the default, the untruncated text is embedded, not served the
// truncated vector.
func TestPlainNoteBackfillSkipsVectorsUnderNonDefaultSectionLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	config := func(limit string) {
		write(".rhizome/config.yml", "notes:\n  includes: [\"notes/*.md\"]\nnoteEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\n"+limit+"codeEmbeddings:\n  enabled: false\n")
	}
	guide := "## Long\n\n" + strings.Repeat("Every word of this section matters. ", 20) + "\n\n## Short\n\nRun the index after edits.\n"
	config("  maxsectionbytes: 120\n")
	write("notes/guide.md", guide)

	definition, err := repoconfig.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	opts := UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: testkit.NoteMetadataIndexer(t), SkipConfigPersistence: true}
	providerTexts := func() int64 {
		t.Helper()
		collector := indexingperf.New()
		require.NoError(t, runUnifiedCoreForTest(t, indexingperf.WithCollector(context.Background(), collector), opts))
		var total int64
		for _, counter := range collector.Snapshot().Counters {
			if counter.Name == string(indexingperf.ProviderTexts) {
				total += counter.Total
			}
		}
		return total
	}
	require.Equal(t, int64(2), providerTexts())

	// The stored chunk hash names the truncated text that was embedded.
	// Reproduce a pre-cache database, whose stored hash is that of the
	// default-limit text though the vector embeds the truncated text.
	longText := "# guide\n\n## Long\n\n## Long\n\n" + strings.TrimSpace(strings.Repeat("Every word of this section matters. ", 20))
	store, closeStore, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	var stored string
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT content_hash FROM intel_chunks WHERE owner_type = 'doc_section' AND heading = 'Long'`).Scan(&stored))
	require.Equal(t, embeddings.HashText(strings.TrimSpace(longText[:120])), stored)
	_, err = store.DB().ExecContext(context.Background(), `UPDATE intel_chunks SET content_hash = ? WHERE content_hash = ?`, embeddings.HashText(longText), stored)
	require.NoError(t, err)
	for _, stmt := range upgradeFromV77 {
		_, err = store.DB().ExecContext(context.Background(), stmt)
		require.NoError(t, err)
	}
	closeStore()
	require.Equal(t, int64(0), providerTexts(), "an unchanged vault re-embeds nothing after the upgrade")

	config("")
	write("notes/guide.md", strings.Replace(guide, "Run the index after edits.", "Run the index after every edit.", 1))
	require.Equal(t, int64(2), providerTexts(), "the untruncated long section and the edited section embed; neither is served a truncated vector")
}
