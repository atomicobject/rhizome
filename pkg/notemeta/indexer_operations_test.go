package notemeta

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIndexerEnsureIndexedPublishesCurrentProviderProvenance(t *testing.T) {
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	reader := fakeNoteReader{notes: map[string]string{"Decision.MD": "---\nwhen: 2026-08-03\n---\n# Decision\n"}}

	result, err := indexer.EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: t.TempDir()}, reader, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)

	rows, err := store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	row := rows[0]
	require.Equal(t, "Decision.MD", row.Path)
	require.Equal(t, "markdown", row.FormatID)
	require.Equal(t, semdb.NoteProjectionStatusCurrent, row.Projection.Status)
	require.Equal(t, row.ContentHash, row.Projection.SourceContentHash)
	require.Equal(t, "markdown-provider-v1", row.Projection.ProviderVersion)
	require.Equal(t, "markdown-projection-v3", row.Projection.ProjectionVersion)
}

func TestIndexerProviderVersionChangeReprojectsWithoutRawHashChange(t *testing.T) {
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	reader := fakeNoteReader{notes: map[string]string{"note.md": "# Note\n"}}
	vault := obsidian.VaultDefinition{Path: t.TempDir()}
	v1 := newIndexerTestRuntime(t, "provider-v1")
	indexerV1, err := NewIndexer(v1)
	require.NoError(t, err)
	_, err = indexerV1.EnsureIndexed(context.Background(), vault, reader, store)
	require.NoError(t, err)
	before, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)

	v2 := newIndexerTestRuntime(t, "provider-v2")
	indexerV2, err := NewIndexer(v2)
	require.NoError(t, err)
	dirty, err := indexerV2.DiscoverDirtyPaths(context.Background(), vault, reader, store)
	require.NoError(t, err)
	require.Equal(t, []string{"note.md"}, dirty.Changed)
	result, err := indexerV2.EnsureIndexed(context.Background(), vault, reader, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	after, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)
	require.Equal(t, before.RawNotesHash, after.RawNotesHash)
	clean, err := indexerV2.DiscoverDirtyPaths(context.Background(), vault, reader, store)
	require.NoError(t, err)
	require.Empty(t, clean.Changed)
}

func TestIndexerEnsureIndexedRetainsFatalSourceIdentityWithoutDerivedFacts(t *testing.T) {
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	projector := indexerTestProjector{descriptor: noteformat.Descriptor{
		ID: "markdown", Extensions: []string{".md"}, ProviderVersion: "provider-v1", ProjectionVersion: "projection-v1", OwnershipPolicy: noteformat.OwnershipDefault,
	}, fatal: true}
	registry, err := noteformat.NewRegistry(projector)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, projector)
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: t.TempDir()}
	reader := fakeNoteReader{notes: map[string]string{"fatal.md": "---\nstatus: source\n---\n"}}

	_, err = indexer.EnsureIndexed(context.Background(), vault, reader, store)
	require.NoError(t, err)
	rows, err := store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, semdb.NoteProjectionStatusFatal, rows[0].Projection.Status)
	require.NotEmpty(t, rows[0].ContentHash)
	require.Equal(t, rows[0].ContentHash, rows[0].Projection.SourceContentHash)
	properties, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "source", 0)
	require.NoError(t, err)
	require.Empty(t, properties)
	current, err := indexer.MetadataStateCurrent(context.Background(), vault, reader, store)
	require.NoError(t, err)
	require.False(t, current)
	second, err := indexer.EnsureIndexed(context.Background(), vault, reader, store)
	require.NoError(t, err)
	require.False(t, second.Dirty)
	dirty, err := indexer.DiscoverDirtyPaths(context.Background(), vault, reader, store)
	require.NoError(t, err)
	require.Empty(t, dirty.Changed, "fatal projections retain their current-source retry policy")
}

func TestIndexerRetriesStaleProjectionWithoutSourceChange(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	vault := obsidian.VaultDefinition{Path: t.TempDir()}
	reader := fakeNoteReader{notes: map[string]string{"note.md": "# Note\n"}}
	indexer := testIndexer(t)

	_, err = indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"note.md"})
	require.NoError(t, err)
	row := rows["note.md"]
	row.Projection.Status = semdb.NoteProjectionStatusStale
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, semdb.NoteMetadataDelta{
		State: state,
		Notes: []semdb.NoteMetadataRow{row},
	}))

	dirty, err := indexer.DiscoverDirtyPaths(ctx, vault, reader, store)
	require.NoError(t, err)
	require.Equal(t, []string{"note.md"}, dirty.Changed)
	require.Empty(t, dirty.Deleted)

	result, err := indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	rows, err = store.CurrentNoteMetadataRowsByPaths(ctx, []string{"note.md"})
	require.NoError(t, err)
	require.Equal(t, semdb.NoteProjectionStatusCurrent, rows["note.md"].Projection.Status)
}

func TestIndexerNormalDirtyAndDeltaUseProjectedFactsRatherThanMarkdownSyntax(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	projector := &projectionOnlyTestProjector{descriptor: noteformat.Descriptor{
		ID: "markdown", Extensions: []string{".md"}, ProviderVersion: "provider-v1", ProjectionVersion: "projection-v1", OwnershipPolicy: noteformat.OwnershipDefault,
	}}
	runtime, err := runtimeForProjector(projector)
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: t.TempDir(), Links: obsidian.LinkTypeBoth}
	initial := fakeNoteReader{notes: map[string]string{
		"source.md": "# authored heading\n[[wrong-target]]\n",
		"target.md": "---\naliases: [wrong-alias]\n---\n# wrong target\n",
	}}
	_, err = indexer.EnsureIndexed(ctx, vault, initial, store)
	require.NoError(t, err)

	projector.calls.Store(0)
	dirty, err := indexer.DiscoverDirtyPaths(ctx, vault, initial, store)
	require.NoError(t, err)
	require.Empty(t, dirty.Changed)
	require.Equal(t, int32(0), projector.calls.Load(), "dirty discovery compares source identity and must not project")

	updated := fakeNoteReader{notes: map[string]string{
		"source.md": "# a different authored heading\n[[still-wrong]]\n",
		"target.md": "---\naliases: [still-wrong]\n---\n# still wrong\n",
	}}
	delta, err := indexer.BuildPathDelta(ctx, vault, updated, store, []string{"source.md"}, nil)
	require.NoError(t, err)
	require.Len(t, delta.FragmentTargets, 1)
	require.Equal(t, "provider-fragment", delta.FragmentTargets[0].Target)
	require.Len(t, delta.WikilinkEdges, 2)
	for _, edge := range delta.WikilinkEdges {
		require.Equal(t, "source.md", edge.SrcPath)
		require.Equal(t, "target.md", edge.DstPath, "link resolution must follow the projector alias, not authored Markdown")
	}
}

func TestBuildPathDeltaReusesAliasSnapshotWithFreshSnapshotParity(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	indexer := testIndexer(t)
	vault := obsidian.VaultDefinition{Path: t.TempDir(), Links: obsidian.LinkTypeBoth}
	reader := fakeNoteReader{notes: map[string]string{
		"notes/source.md": "---\naliases: [SOURCE]\n---\nOriginal body.\n",
		"targets/a.md":    "---\naliases: [SHARED, UNIQUE]\n---\n# Target A\n",
		"targets/b.md":    "---\naliases: [SHARED]\n---\n# Target B\n",
	}}
	_, err = indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	reader.notes["notes/source.md"] = "---\naliases: [SOURCE]\n---\nSee [[SHARED]], [[UNIQUE]], and [relative](../targets/b.md).\n"

	counting := &aliasCountingStore{Store: store}
	delta, err := indexer.BuildPathDelta(ctx, vault, reader, counting, []string{"notes/source.md"}, nil)
	require.NoError(t, err)
	require.NotNil(t, delta)
	require.Len(t, delta.Notes, 1, "unchanged alias topology must use the incremental path")
	require.Equal(t, 1, counting.aliasCalls, "topology validation and cache population should share one alias snapshot")
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *delta))
	edges := requireGraphEdgesMatchFreshSnapshot(t, store, vault, reader)
	require.NotEmpty(t, edges, "parity must include resolved unique-alias and relative links")
}

type aliasCountingStore struct {
	Store
	aliasCalls int
}

func (s *aliasCountingStore) CurrentNoteAliases(ctx context.Context) (map[string][]string, error) {
	s.aliasCalls++
	return s.Store.CurrentNoteAliases(ctx)
}

func TestLinkLookupPathUsesDecodedRelativeMarkdownPath(t *testing.T) {
	link := noteformat.UnresolvedAuthoredLinkFact{
		Resolution:    noteformat.LinkResolutionRelativePath,
		ResolverInput: `<Folder/My Spec.md#details> "Readable title"`,
		Path:          "Folder/My Spec.md",
		Fragment:      "details",
	}
	require.Equal(t, "notes/Folder/My Spec.md", linkLookupPath("notes/source.md", nil, link))
}

type indexerTestProjector struct {
	descriptor noteformat.Descriptor
	fatal      bool
}

// projectionOnlyTestProjector deliberately disagrees with every authored
// Markdown construct in the fixture. Its facts make parser reachability in
// normal Indexer paths observable rather than relying on implementation shape.
type projectionOnlyTestProjector struct {
	descriptor noteformat.Descriptor
	calls      atomic.Int32
}

func (p *projectionOnlyTestProjector) Descriptor() noteformat.Descriptor { return p.descriptor }

func (p *projectionOnlyTestProjector) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	p.calls.Add(1)
	facts := noteformat.ProjectionFacts{Title: &noteformat.TitleFact{Value: "provider title"}}
	switch source.Path().String() {
	case "target.md":
		facts.Aliases = []noteformat.AliasFact{{Value: "provider-target"}}
	case "source.md":
		facts.FragmentTargets = []noteformat.FragmentTargetFact{{Kind: "heading", Text: "provider-fragment", NormalizedText: "provider-fragment", Ordinal: 1}}
		facts.Links = []noteformat.UnresolvedAuthoredLinkFact{{
			Resolution:    noteformat.LinkResolutionNoteReference,
			Syntax:        "controlled",
			Subtype:       "basic",
			ResolverInput: "provider-target",
			Target:        "provider-target",
			Path:          "provider-target",
		}}
	}
	return noteformat.NewProjectionWithFacts(p.descriptor.ProviderVersion, p.descriptor.ProjectionVersion, noteformat.ProjectionStatusCurrent, nil, p.descriptor.Capabilities, facts)
}

func runtimeForProjector(projector noteformat.Projector) (noteformat.Runtime, error) {
	registry, err := noteformat.NewRegistry(projector)
	if err != nil {
		return noteformat.Runtime{}, err
	}
	return noteformat.NewRuntime(registry, projector)
}

func (p indexerTestProjector) Descriptor() noteformat.Descriptor { return p.descriptor }

func (p indexerTestProjector) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	if p.fatal {
		return noteformat.NewProjectionWithFacts(
			p.descriptor.ProviderVersion, p.descriptor.ProjectionVersion, noteformat.ProjectionStatusFatal,
			[]noteformat.Diagnostic{{Code: "projection_failed", Message: "invalid authored source", Blocking: true}}, p.descriptor.Capabilities, noteformat.ProjectionFacts{},
		)
	}
	return noteformat.NewProjectionWithFacts(
		p.descriptor.ProviderVersion,
		p.descriptor.ProjectionVersion,
		noteformat.ProjectionStatusCurrent,
		nil,
		p.descriptor.Capabilities,
		noteformat.ProjectionFacts{Title: &noteformat.TitleFact{Value: source.Path().String()}},
	)
}

func newIndexerTestRuntime(t *testing.T, providerVersion string) noteformat.Runtime {
	t.Helper()
	projector := indexerTestProjector{descriptor: noteformat.Descriptor{
		ID:                "markdown",
		Extensions:        []string{".md"},
		ProviderVersion:   providerVersion,
		ProjectionVersion: "projection-v1",
		OwnershipPolicy:   noteformat.OwnershipDefault,
	}}
	registry, err := noteformat.NewRegistry(projector)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, projector)
	require.NoError(t, err)
	return runtime
}
