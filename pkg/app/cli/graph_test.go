package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type snapshotBackedNote struct {
	entries []cache.Entry
}

func (n *snapshotBackedNote) EntriesSnapshot(context.Context) ([]cache.Entry, error) {
	out := make([]cache.Entry, 0, len(n.entries))
	for _, entry := range n.entries {
		out = append(out, entry)
	}
	return out, nil
}

func (n *snapshotBackedNote) NoteEntriesSnapshot(context.Context) ([]obsidian.NoteEntry, error) {
	out := make([]obsidian.NoteEntry, 0, len(n.entries))
	for _, entry := range n.entries {
		out = append(out, obsidian.NoteEntry{
			Path:        entry.Path,
			Content:     entry.Content,
			ContentTime: entry.ModTime,
		})
	}
	return out, nil
}

func (n *snapshotBackedNote) GetContents(_ obsidian.VaultDefinition, _ string) (string, error) {
	return "", errors.New("unexpected direct content read")
}

func (n *snapshotBackedNote) GetNotesList(_ obsidian.VaultDefinition) ([]string, error) {
	out := make([]string, 0, len(n.entries))
	for _, entry := range n.entries {
		out = append(out, entry.Path)
	}
	return out, nil
}

func (n *snapshotBackedNote) GetModTime(_ obsidian.VaultDefinition, notePath string) (time.Time, error) {
	for _, entry := range n.entries {
		if entry.Path == notePath {
			return entry.ModTime, nil
		}
	}
	return time.Time{}, os.ErrNotExist
}

func (n *snapshotBackedNote) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}

func TestPersistedGraphSnapshotSupportsMarkdownBacklinksAndSuppressedTags(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.md"), []byte("# Target\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ref-keep.md"), []byte("[target](target.md)\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ref-hide.md"), []byte("#no-prompt\n[target](target.md)\n"), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Name: "vault", Path: root, Links: obsidian.LinkTypeBoth}, &obsidian.Note{}, store)
	require.NoError(t, err)

	vaultDef := obsidian.VaultDefinition{Name: "vault", Path: root, Links: obsidian.LinkTypeBoth}
	persisted, cleanup, err := loadPersistedGraphSnapshotWithStore(vaultDef, &obsidian.Note{}, testNoteMetadataIndexer(t), store, MetadataStoreFallbackOpen)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	if cleanup != nil {
		defer cleanup()
	}

	backlinks := obsidian.CollectBacklinksFromGraph(persisted, []string{"target.md"}, []string{"no-prompt"})
	require.Contains(t, backlinks, "target.md")
	require.Len(t, backlinks["target.md"], 1)
	require.Equal(t, "ref-keep.md", backlinks["target.md"][0].Referrer)
	require.Equal(t, obsidian.BacklinkTypeBasic, backlinks["target.md"][0].LinkType)
}

func TestGraphAnalysisPersistedConsumerMatchesLiveStructure(t *testing.T) {
	root := t.TempDir()
	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "vault", Path: root, Links: obsidian.LinkTypeBoth},
		name: "vault",
	}
	note := &snapshotBackedNote{
		entries: []cache.Entry{
			{Path: "alpha.md", Content: "[[beta]]", ModTime: time.Unix(10, 0)},
			{Path: "beta.md", Content: "[[alpha]]", ModTime: time.Unix(11, 0)},
			{Path: "orphan.md", Content: "", ModTime: time.Unix(12, 0)},
		},
	}
	store := seedGraphMetadataStore(t, root, note)
	defer func() { _ = store.Close() }()

	persisted, err := GraphAnalysis(vault, note, GraphAnalysisParams{SessionStore: store, NoteMetadata: testNoteMetadataIndexer(t)})
	require.NoError(t, err)
	live, err := obsidian.ComputeGraphAnalysis(vault.def, note, obsidian.GraphAnalysisOptions{
		WikilinkOptions:    obsidian.DefaultWikilinkOptions,
		IncludeDocsInGraph: true,
		DocMinBytes:        200,
		RecencyCascade:     true,
		RecencyCascadeSet:  true,
	})
	require.NoError(t, err)

	require.Equal(t, live.Stats, persisted.Stats)
	require.Equal(t, 1, persisted.Nodes["alpha.md"].Outbound)
	require.Equal(t, 1, persisted.Nodes["beta.md"].Inbound)
	require.Equal(t, 0, persisted.Nodes["orphan.md"].Outbound)
	for path, liveNode := range live.Nodes {
		persistedNode, ok := persisted.Nodes[path]
		require.True(t, ok, path)
		require.Equal(t, liveNode.Inbound, persistedNode.Inbound, path)
		require.Equal(t, liveNode.Outbound, persistedNode.Outbound, path)
		require.Equal(t, liveNode.Neighbors, persistedNode.Neighbors, path)
	}
}

func TestGraphAnalysisUsesSuppliedManagedReadOnlySnapshot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "vault", Path: root, Links: obsidian.LinkTypeBoth},
		name: "vault",
	}
	indexed := &snapshotBackedNote{entries: []cache.Entry{
		{Path: "alpha.md", Content: "[[beta]]", ModTime: time.Unix(10, 0)},
		{Path: "beta.md", Content: "", ModTime: time.Unix(11, 0)},
	}}
	store := seedGraphMetadataStore(t, root, indexed)
	require.NoError(t, store.Close())

	indexPath := filepath.Join(root, ".rhizome", "db.sqlite")
	before, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	readOnly, err := semdb.OpenReadOnlyExisting(indexPath, ctx, sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })

	live := &snapshotBackedNote{entries: []cache.Entry{
		{Path: "alpha.md", Content: "", ModTime: time.Unix(20, 0)},
		{Path: "beta.md", Content: "", ModTime: time.Unix(21, 0)},
	}}
	analysis, err := GraphAnalysis(vault, live, GraphAnalysisParams{
		SessionStore:          readOnly,
		NoteMetadata:          testNoteMetadataIndexer(t),
		MetadataStoreFallback: MetadataStoreFallbackLive,
	})
	require.NoError(t, err)
	require.Equal(t, 1, analysis.Nodes["alpha.md"].Outbound, "must use the supplied persisted snapshot instead of live notes")
	require.Equal(t, 1, analysis.Nodes["beta.md"].Inbound)
	after, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "managed read-only graph analysis must not write the index")
}

func TestGraphAnalysisManagedReaderWithoutSnapshotFallsBackToLive(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	vault := &fixedVault{def: obsidian.VaultDefinition{Name: "vault", Path: root, Links: obsidian.LinkTypeBoth}, name: "vault"}
	store, err := sqlitefixture.Open(filepath.Join(root, "graph.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.Close())
	before, err := os.ReadFile(filepath.Join(root, "graph.sqlite"))
	require.NoError(t, err)
	readOnly, err := semdb.OpenReadOnlyExisting(filepath.Join(root, "graph.sqlite"), ctx, sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })

	live := &snapshotBackedNote{entries: []cache.Entry{
		{Path: "alpha.md", Content: "[[beta]]", ModTime: time.Unix(10, 0)},
		{Path: "beta.md", Content: "", ModTime: time.Unix(11, 0)},
	}}
	analysis, err := GraphAnalysis(vault, live, GraphAnalysisParams{
		SessionStore:          readOnly,
		NoteMetadata:          testNoteMetadataIndexer(t),
		MetadataStoreFallback: MetadataStoreFallbackLive,
	})
	require.NoError(t, err)
	require.NotNil(t, analysis)
	require.Contains(t, analysis.Nodes, "alpha.md")
	require.Equal(t, 1, analysis.Nodes["alpha.md"].Outbound)
	require.Equal(t, 1, analysis.Nodes["beta.md"].Inbound)
	after, err := os.ReadFile(filepath.Join(root, "graph.sqlite"))
	require.NoError(t, err)
	require.Equal(t, before, after, "live fallback must not write the managed store")
}

func TestGraphAnalysisManagedReaderLoadErrorReturnsUnavailable(t *testing.T) {
	root := t.TempDir()
	vault := &fixedVault{def: obsidian.VaultDefinition{Name: "vault", Path: root, Links: obsidian.LinkTypeBoth}, name: "vault"}
	store, err := sqlitefixture.Open(filepath.Join(root, "graph.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.Close())

	analysis, err := GraphAnalysis(vault, &snapshotBackedNote{}, GraphAnalysisParams{
		SessionStore:          store,
		NoteMetadata:          testNoteMetadataIndexer(t),
		MetadataStoreFallback: MetadataStoreFallbackLive,
	})
	require.Nil(t, analysis)
	require.ErrorIs(t, err, ErrManagedGraphSnapshotUnavailable)
}

func TestGraphAnalysisWithoutManagedReaderFallsBackToLive(t *testing.T) {
	root := t.TempDir()
	vault := &fixedVault{def: obsidian.VaultDefinition{Name: "vault", Path: root, Links: obsidian.LinkTypeBoth}, name: "vault"}
	live := &snapshotBackedNote{entries: []cache.Entry{
		{Path: "alpha.md", Content: "[[beta]]", ModTime: time.Unix(10, 0)},
		{Path: "beta.md", Content: "", ModTime: time.Unix(11, 0)},
	}}
	analysis, err := GraphAnalysis(vault, live, GraphAnalysisParams{MetadataStoreFallback: MetadataStoreFallbackLive})
	require.NoError(t, err)
	require.Equal(t, 1, analysis.Nodes["alpha.md"].Outbound)
}

func TestDocGraphAnalysisManagedReadOnlySkipsMaterializationForDriftedEntries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	alphaPath := filepath.Join(root, "Alpha.md")
	require.NoError(t, os.WriteFile(alphaPath, []byte("# Alpha\n\nOriginal indexed entry.\n"), 0o644))
	vault := &fixedVault{def: obsidian.VaultDefinition{Name: "vault", Path: root}, name: "vault"}

	indexPath := filepath.Join(root, ".rhizome", "db.sqlite")
	writable, err := sqlitefixture.Open(indexPath)
	require.NoError(t, err)
	require.NoError(t, writable.ReplaceGraphDocScores(ctx, []semdb.GraphDocScore{{
		DocPath: "Alpha.md", DocType: "note", Authority: 1,
	}}))
	require.NoError(t, writable.Close())
	require.NoError(t, os.WriteFile(alphaPath, []byte("# Alpha\n\nThis entry changed after graph scoring.\n"), 0o644))
	before, err := os.ReadFile(indexPath)
	require.NoError(t, err)

	readOnly, err := semdb.OpenReadOnlyExisting(indexPath, ctx, sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })
	analysis, err := DocGraphAnalysis(ctx, vault, &obsidian.Note{}, DocGraphAnalysisParams{
		IntelStore:            readOnly,
		MetadataStoreFallback: MetadataStoreFallbackLive,
		WikilinkOptions:       obsidian.DefaultWikilinkOptions,
	})
	require.NoError(t, err)
	require.Contains(t, analysis.Nodes, "Alpha.md")
	after, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "managed read-only community analysis must not materialize graph scores")
}

func seedGraphMetadataStore(t *testing.T, root string, note obsidian.NoteReader) *semdb.Store {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), obsidian.VaultDefinition{Name: "vault", Path: root, Links: obsidian.LinkTypeBoth}, note, store)
	require.NoError(t, err)
	return store
}
