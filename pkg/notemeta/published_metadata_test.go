package notemeta

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBuildPublishedMetadataDeltaProjectsMarkdownAndPreservesDescriptorOnlyIdentity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	markdown := "---\naliases: [DECISION]\nstatus: active\n---\n# Decision\n#tag\nSee [[published]].\n"
	html := "<p>Published HTML is descriptor-only.</p>\n"
	writePublishedTestFile(t, root, "notes/decision.md", markdown)
	htmlInfo := writePublishedTestFile(t, root, "notes/published.html", html)
	store := openPublishedMetadataStore(t, root)
	indexer, runtime := publishedMetadataIndexer(t)
	publishDescriptorOnlyHTML(t, ctx, store, runtime, "notes/published.html", html, htmlInfo, 11)

	delta, err := indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store,
		[]paths.NotePath{"notes/published.html", "notes/decision.md"}, nil)
	require.NoError(t, err)
	require.Len(t, delta.Notes, 1)
	require.Equal(t, "notes/decision.md", delta.Notes[0].Path)
	require.NotEmpty(t, delta.PropertyValues)
	require.NotEmpty(t, delta.Tags)
	require.Empty(t, delta.WikilinkEdges, "descriptor-only HTML must not be a link target")
	require.Equal(t, "html", descriptorRow(t, ctx, store, "notes/published.html").FormatID)

	expectedRaw := notesHashFromRows(append(append([]semdb.NoteMetadataRow(nil), delta.Notes...), descriptorRow(t, ctx, store, "notes/published.html")))
	require.Equal(t, expectedRaw, delta.State.RawNotesHash)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *delta))
	rows, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	aliases, err := store.CurrentNoteAliases(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"DECISION"}, aliases["notes/decision.md"])
	require.NotContains(t, aliases, "notes/published.html")
}

func TestBuildPublishedMetadataDeltaRefreshesMarkdownAliasesAndEdgesWithoutDescriptorTargets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writePublishedTestFile(t, root, "notes/source.md", "See [[OLD]] and [[published]].\n")
	writePublishedTestFile(t, root, "notes/target.md", "---\naliases: [OLD]\n---\n# Target\n")
	html := "<p>Published HTML is descriptor-only.</p>\n"
	htmlInfo := writePublishedTestFile(t, root, "notes/published.html", html)
	store := openPublishedMetadataStore(t, root)
	indexer, runtime := publishedMetadataIndexer(t)
	publishDescriptorOnlyHTML(t, ctx, store, runtime, "notes/published.html", html, htmlInfo, 12)
	pathsList := []paths.NotePath{"notes/source.md", "notes/target.md", "notes/published.html"}

	initial, err := indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, pathsList, nil)
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *initial))
	requireContainsEdge(t, initial.WikilinkEdges, "notes/source.md", "notes/target.md")
	requireNotContainsDestination(t, initial.WikilinkEdges, "notes/published.html")

	writePublishedTestFile(t, root, "notes/source.md", "See [[NEW]] and [[published]].\n")
	writePublishedTestFile(t, root, "notes/target.md", "---\naliases: [NEW]\n---\n# Target\n")
	changed, err := indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, pathsList, nil)
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *changed))
	requireContainsEdge(t, changed.WikilinkEdges, "notes/source.md", "notes/target.md")
	requireNotContainsDestination(t, changed.WikilinkEdges, "notes/published.html")
	aliases, err := store.CurrentNoteAliases(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"NEW"}, aliases["notes/target.md"])
	require.NotContains(t, aliases, "notes/published.html")
}

func TestBuildPublishedMetadataDeltaUsesSealedSourcesWithoutReaderDiscovery(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	html := "<p>Published HTML is descriptor-only.</p>\n"
	htmlInfo := writePublishedTestFile(t, root, "notes/published.html", html)
	store := openPublishedMetadataStore(t, root)
	indexer, runtime := publishedMetadataIndexer(t)
	publishDescriptorOnlyHTML(t, ctx, store, runtime, "notes/published.html", html, htmlInfo, 13)
	markdownProvider, ok := runtime.Provider("markdown")
	require.True(t, ok)
	sealedSource, err := noteformat.NewAuthoredSource(paths.NotePath("notes/decision.md"), markdownProvider.Descriptor(), []byte("---\naliases: [SEALED]\n---\n# Decision\n"), 22)
	require.NoError(t, err)
	reader := &publishedFailingReader{}

	delta, err := indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, reader, store,
		[]paths.NotePath{"notes/decision.md", "notes/published.html"}, map[paths.NotePath]noteformat.AuthoredSource{"notes/decision.md": sealedSource})
	require.NoError(t, err)
	require.Zero(t, reader.contentsCalls)
	require.Zero(t, reader.listCalls)
	require.Len(t, delta.Notes, 1)
	require.Equal(t, "notes/decision.md", delta.Notes[0].Path)
}

func TestBuildPublishedMetadataDeltaRetainsDurableUnreadableFatalWithoutReader(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openPublishedMetadataStore(t, root)
	indexer, runtime := publishedMetadataIndexer(t)
	markdown, ok := runtime.Provider("markdown")
	require.True(t, ok)
	descriptor := markdown.Descriptor()
	_, err := store.ApplyOwnershipTransitions(ctx, []semdb.OwnershipTransition{{
		Path: "notes/unreadable.md", Target: semdb.OwnershipTargetNote,
		Note: &semdb.NoteSourceState{
			Title: "unreadable", FormatID: string(descriptor.ID), ProviderVersion: descriptor.ProviderVersion, ProjectionVersion: descriptor.ProjectionVersion,
			Status: semdb.NoteProjectionStatusFatal, DiagnosticCode: "unreadable_source", DiagnosticDetail: "authored source could not be read", ObservedAt: 27,
		},
	}})
	require.NoError(t, err)
	reader := &publishedFailingReader{}

	delta, err := indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, reader, store, []paths.NotePath{"notes/unreadable.md"}, nil)
	require.NoError(t, err)
	require.NotNil(t, delta)
	require.Empty(t, delta.Notes, "the durable root-only row must not recreate metadata search terms")
	require.Zero(t, reader.contentsCalls)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *delta))
	row := descriptorRow(t, ctx, store, "notes/unreadable.md")
	require.Equal(t, semdb.NoteProjectionStatusFatal, row.Projection.Status)
	require.Equal(t, "unreadable_source", row.Projection.DiagnosticCode)
}

func TestPublishedOwnershipTransitionsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openPublishedMetadataStore(t, root)
	_, runtime := publishedMetadataIndexer(t)
	markdown, ok := runtime.Provider("markdown")
	require.True(t, ok)
	source, err := noteformat.NewAuthoredSource(paths.NotePath("notes/Fatal.md"), markdown.Descriptor(), []byte("# Fatal\n"), 44)
	require.NoError(t, err)
	projection, err := noteformat.NewProjection(markdown.Descriptor().ProviderVersion, markdown.Descriptor().ProjectionVersion, noteformat.ProjectionStatusFatal, []noteformat.Diagnostic{{Code: "projector_fatal", Message: "projection failed", Blocking: true}}, markdown.Descriptor().Capabilities)
	require.NoError(t, err)
	entry, err := projectionEntryFrom(source, projection)
	require.NoError(t, err)
	fatalPreparation := PublishedMetadataPreparation{sealedEntries: []projectedNoteEntry{entry}}
	transitions, err := fatalPreparation.OwnershipTransitions(55)
	require.NoError(t, err)
	require.Len(t, transitions, 1)
	require.Equal(t, "Fatal", transitions[0].Note.Title)

	first, err := store.ApplyOwnershipTransitions(ctx, transitions)
	require.NoError(t, err)
	require.Positive(t, first.ReconciliationGeneration)
	second, err := store.ApplyOwnershipTransitions(ctx, transitions)
	require.NoError(t, err)
	require.Zero(t, second.ReconciliationGeneration)

	currentProjection, err := runtime.Project(source)
	require.NoError(t, err)
	currentEntry, err := projectionEntryFrom(source, currentProjection)
	require.NoError(t, err)
	currentPreparation := PublishedMetadataPreparation{sealedEntries: []projectedNoteEntry{currentEntry}}
	currentTransitions, err := currentPreparation.OwnershipTransitions(56)
	require.NoError(t, err)
	recovered, err := store.ApplyOwnershipTransitions(ctx, currentTransitions)
	require.NoError(t, err)
	require.Greater(t, recovered.ReconciliationGeneration, first.ReconciliationGeneration, "fatal to current must create durable recovery debt")

	currentTransitions[0].Note.Title = "Benign title refresh"
	currentTransitions[0].Note.Mtime++
	currentTransitions[0].Note.ObservedAt++
	stable, err := store.ApplyOwnershipTransitions(ctx, currentTransitions)
	require.NoError(t, err)
	require.Zero(t, stable.ReconciliationGeneration)
}

func TestBuildPublishedMetadataDeltaFailsClosedForMissingOrMismatchedPublishedInputs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openPublishedMetadataStore(t, root)
	indexer, runtime := publishedMetadataIndexer(t)
	markdownProvider, ok := runtime.Provider("markdown")
	require.True(t, ok)
	source, err := noteformat.NewAuthoredSource(paths.NotePath("notes/decision.md"), markdownProvider.Descriptor(), []byte("# Decision\n"), 23)
	require.NoError(t, err)

	_, err = indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, &publishedFailingReader{}, store,
		[]paths.NotePath{"notes/decision.md"}, map[paths.NotePath]noteformat.AuthoredSource{"notes/other.md": source})
	require.ErrorContains(t, err, "not selected")

	_, err = indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, nil, store,
		[]paths.NotePath{"notes/published.html"}, nil)
	require.ErrorContains(t, err, "descriptor-only note metadata row is missing")

	reader := &publishedFailingReader{}
	_, err = indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, reader, store,
		[]paths.NotePath{"notes/decision.md"}, nil)
	require.ErrorContains(t, err, "read note notes/decision.md")
	require.Zero(t, reader.listCalls, "explicit paths must never trigger reader discovery")
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.False(t, state.Ready, "a failed builder must not write state")
}

func TestBuildPublishedMetadataDeltaRejectsMismatchedDescriptorProvenanceAndUnexpectedRows(t *testing.T) {
	ctx := context.Background()
	t.Run("descriptor provenance", func(t *testing.T) {
		root := t.TempDir()
		store := openPublishedMetadataStore(t, root)
		indexer, runtime := publishedMetadataIndexer(t)
		htmlProvider, ok := runtime.Provider("html")
		require.True(t, ok)
		descriptor := htmlProvider.Descriptor()
		require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
			State: semdb.NoteMetadataState{LoadedAt: 1, Ready: false},
			Notes: []semdb.NoteMetadataRow{{
				Path: "notes/published.html", ContentHash: "source", Mtime: 1, Size: 6, FormatID: string(descriptor.ID),
				Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusStale, SourceContentHash: "source", ProviderVersion: "wrong", ProjectionVersion: descriptor.ProjectionVersion, UpdatedAt: 1},
			}},
		}))
		_, err := indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, nil, store, []paths.NotePath{"notes/published.html"}, nil)
		require.ErrorContains(t, err, "not current durable provenance")
	})

	t.Run("unexpected materialized row", func(t *testing.T) {
		root := t.TempDir()
		store := openPublishedMetadataStore(t, root)
		indexer, runtime := publishedMetadataIndexer(t)
		markdownProvider, ok := runtime.Provider("markdown")
		require.True(t, ok)
		source, err := noteformat.NewAuthoredSource(paths.NotePath("notes/decision.md"), markdownProvider.Descriptor(), []byte("# Decision\n"), 24)
		require.NoError(t, err)
		require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
			State: semdb.NoteMetadataState{LoadedAt: 1, Ready: false},
			Notes: []semdb.NoteMetadataRow{{Path: "notes/unexpected.md", ContentHash: "old", Mtime: 1, Size: 3}},
		}))
		_, err = indexer.BuildPublishedMetadataDelta(ctx, obsidian.VaultDefinition{Path: root}, nil, store,
			[]paths.NotePath{"notes/decision.md"}, map[paths.NotePath]noteformat.AuthoredSource{"notes/decision.md": source})
		require.ErrorContains(t, err, "not in the complete selected path set")
	})
}

func TestBuildPublishedMetadataDeltaIsInputOrderIndependent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writePublishedTestFile(t, root, "notes/decision.md", "# Decision\n")
	html := "<p>Published HTML is descriptor-only.</p>\n"
	htmlInfo := writePublishedTestFile(t, root, "notes/published.html", html)
	store := openPublishedMetadataStore(t, root)
	indexer, runtime := publishedMetadataIndexer(t)
	publishDescriptorOnlyHTML(t, ctx, store, runtime, "notes/published.html", html, htmlInfo, 14)
	vault := obsidian.VaultDefinition{Path: root}
	first, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, []paths.NotePath{"notes/decision.md", "notes/published.html"}, nil)
	require.NoError(t, err)
	second, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, []paths.NotePath{"notes/published.html", "notes/decision.md"}, nil)
	require.NoError(t, err)
	require.Equal(t, first.State.RawNotesHash, second.State.RawNotesHash)
	require.Equal(t, first.State.NotesHash, second.State.NotesHash)
	require.Equal(t, projectedRowPaths(first.Notes), projectedRowPaths(second.Notes))
}

func TestBuildPublishedMetadataDeltaReturnsNoDeltaForCurrentPublishedState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writePublishedTestFile(t, root, "notes/decision.md", "# Decision\n")
	html := "<p>Published HTML is descriptor-only.</p>\n"
	htmlInfo := writePublishedTestFile(t, root, "notes/published.html", html)
	store := openPublishedMetadataStore(t, root)
	indexer, runtime := publishedMetadataIndexer(t)
	publishDescriptorOnlyHTML(t, ctx, store, runtime, "notes/published.html", html, htmlInfo, 15)
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md", "notes/*.html"}}
	pathsList := []paths.NotePath{"notes/decision.md", "notes/published.html"}

	initial, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, pathsList, nil)
	require.NoError(t, err)
	require.NotNil(t, initial)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *initial))
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)

	unchanged, err := indexer.BuildPublishedMetadataDelta(ctx, vault, &obsidian.Note{}, store, pathsList, nil)
	require.NoError(t, err)
	require.Nil(t, unchanged)
	updated, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, state, updated)
}

func openPublishedMetadataStore(t *testing.T, root string) *semdb.Store {
	t.Helper()
	store, err := sqlitefixture.Open(filepath.Join(root, "metadata.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

func publishedMetadataIndexer(t *testing.T) (Indexer, noteformat.Runtime) {
	t.Helper()
	runtime := descriptorOnlyHTMLRuntime(t)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	return indexer, runtime
}

func descriptorOnlyHTMLRuntime(t *testing.T) noteformat.Runtime {
	t.Helper()
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New())
	require.NoError(t, err)
	return runtime
}

func writePublishedTestFile(t *testing.T, root, notePath, content string) os.FileInfo {
	t.Helper()
	absPath := filepath.Join(root, filepath.FromSlash(notePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
	require.NoError(t, os.WriteFile(absPath, []byte(content), 0o644))
	info, err := os.Stat(absPath)
	require.NoError(t, err)
	return info
}

func publishDescriptorOnlyHTML(t *testing.T, ctx context.Context, store *semdb.Store, runtime noteformat.Runtime, notePath, content string, info os.FileInfo, observedAt int64) {
	t.Helper()
	provider, ok := runtime.Provider("html")
	require.True(t, ok)
	descriptor := provider.Descriptor()
	_, err := store.ApplyOwnershipTransitions(ctx, []semdb.OwnershipTransition{{
		Path: notePath, Target: semdb.OwnershipTargetNote,
		Note: &semdb.NoteSourceState{
			Title: "Published", FormatID: string(descriptor.ID), ContentHash: contentHash(content), Mtime: info.ModTime().Unix(), Size: info.Size(),
			ProviderVersion: descriptor.ProviderVersion, ProjectionVersion: descriptor.ProjectionVersion, Status: semdb.NoteProjectionStatusStale, ObservedAt: observedAt,
		},
	}})
	require.NoError(t, err)
}

func descriptorRow(t *testing.T, ctx context.Context, store *semdb.Store, notePath string) semdb.NoteMetadataRow {
	t.Helper()
	rows, err := store.DurableNoteMetadataRowsByPaths(ctx, []string{notePath})
	require.NoError(t, err)
	row, ok := rows[notePath]
	require.True(t, ok)
	return row
}

func requireContainsEdge(t *testing.T, edges []semdb.GraphDocEdgeRow, source, target string) {
	t.Helper()
	for _, edge := range edges {
		if edge.SrcPath == source && edge.DstPath == target {
			return
		}
	}
	require.Failf(t, "expected graph edge", "%s -> %s", source, target)
}

func requireNotContainsDestination(t *testing.T, edges []semdb.GraphDocEdgeRow, destination string) {
	t.Helper()
	for _, edge := range edges {
		require.NotEqual(t, destination, edge.DstPath)
	}
}

func projectedRowPaths(rows []semdb.NoteMetadataRow) []string {
	pathsList := make([]string, 0, len(rows))
	for _, row := range rows {
		pathsList = append(pathsList, row.Path)
	}
	return pathsList
}

type publishedFailingReader struct {
	contentsCalls int
	listCalls     int
}

func (r *publishedFailingReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	r.contentsCalls++
	return "", errors.New("reader must not be used")
}

func (r *publishedFailingReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	r.listCalls++
	return nil, errors.New("reader discovery must not be used")
}

func (r *publishedFailingReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, errors.New("reader must not be used")
}

func (*publishedFailingReader) Title(path string) (string, bool) { return path, true }
