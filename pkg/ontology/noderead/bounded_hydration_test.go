package noderead

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type boundedHydrationStore struct {
	Store
	rows                                               map[string]semdb.NoteMetadataRow
	edges                                              []semdb.OntologyEdgeRow
	metadataCalls, metadataPaths, typeCalls, typePaths int
	assessmentCalls, assessmentPaths                   int
	metadataErr, propertyErr, tagErr                   error
	metadataBatchErr, typeBatchErr, assessmentBatchErr error
	metadataFailPath                                   string
}

func (s *boundedHydrationStore) CurrentNoteMetadataRowsByPaths(_ context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	s.metadataCalls++
	s.metadataPaths += len(paths)
	out := make(map[string]semdb.NoteMetadataRow, len(paths))
	for _, path := range paths {
		if path == s.metadataFailPath {
			return nil, errors.New("host metadata failed")
		}
		if row, ok := s.rows[path]; ok {
			out[path] = row
		}
	}
	if len(paths) > 1 && s.metadataBatchErr != nil {
		return nil, s.metadataBatchErr
	}
	return out, s.metadataErr
}

func (s *boundedHydrationStore) CurrentNotePropertyValues(context.Context, []string, []string, semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	return nil, s.propertyErr
}

func (s *boundedHydrationStore) CurrentNoteTags(context.Context, []string) ([]semdb.NoteTagRow, error) {
	return nil, s.tagErr
}

func (s *boundedHydrationStore) OntologyTypesByPaths(_ context.Context, paths []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	s.typeCalls++
	s.typePaths += len(paths)
	if len(paths) > 1 && s.typeBatchErr != nil {
		return nil, s.typeBatchErr
	}
	return map[string]semdb.OntologyNoteTypeRow{}, nil
}

func (s *boundedHydrationStore) OntologyAssessmentsByPaths(_ context.Context, paths []string) (map[string]semdb.OntologyNoteAssessmentRow, error) {
	s.assessmentCalls++
	s.assessmentPaths += len(paths)
	if len(paths) > 1 && s.assessmentBatchErr != nil {
		return nil, s.assessmentBatchErr
	}
	return map[string]semdb.OntologyNoteAssessmentRow{}, nil
}

func (s *boundedHydrationStore) OntologyAssessmentFlags(context.Context) (map[string]semdb.OntologyAssessmentFlags, error) {
	return map[string]semdb.OntologyAssessmentFlags{}, nil
}

func (s *boundedHydrationStore) GetOntologySchemaState(context.Context) (semdb.OntologySchemaState, error) {
	return semdb.OntologySchemaState{Ready: true, MaterializationVersion: ontology.OntologyMaterializationVersion}, nil
}

func (s *boundedHydrationStore) OntologyEdgesForPaths(context.Context, []string, bool, string, int) ([]semdb.OntologyEdgeRow, error) {
	return s.edges, nil
}

type boundedHydrationReader struct {
	obsidian.NoteReader
	contents int
}

func (r *boundedHydrationReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	r.contents++
	return "# Target\n^story\n\nTarget body.\n", nil
}

func (*boundedHydrationReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, nil
}

func (*boundedHydrationReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return []string{"a.md", "b.md"}, nil
}

func (*boundedHydrationReader) Title(path string) (string, bool) {
	return path, true
}

func TestSnapshotEligibilityAndMultiHostMetadataBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{
		"current.md":    {Path: "current.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
		"second.md":     {Path: "second.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
		"stale.md":      {Path: "stale.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusStale}},
		"fatal.md":      {Path: "fatal.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusFatal}},
		"descriptor.md": {Path: "descriptor.md", FormatID: "descriptor", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
	}}
	reader := &boundedHydrationReader{}
	scope := NewService(obsidian.VaultDefinition{}, reader, store, nil).NewScope(ctx, ScopeOptions{})
	require.NoError(t, scope.ensurePathStateLocked(ctx, []string{"current.md", "second.md", "stale.md", "fatal.md", "descriptor.md", "missing.md"}))
	require.Equal(t, 1, store.metadataCalls)
	require.Equal(t, 6, store.metadataPaths)
	for _, path := range []string{"current.md", "second.md"} {
		snapshot, err := scope.snapshotForPathLocked(ctx, path)
		require.NoError(t, err)
		require.NotNil(t, snapshot)
	}
	for _, path := range []string{"stale.md", "fatal.md", "descriptor.md", "missing.md"} {
		snapshot, err := scope.snapshotForPathLocked(ctx, path)
		require.NoError(t, err)
		require.Nil(t, snapshot)
	}
	require.Equal(t, 2, reader.contents)
	_, err := scope.snapshotForPathLocked(ctx, "current.md")
	require.NoError(t, err)
	_, err = scope.snapshotForPathLocked(ctx, "missing.md")
	require.NoError(t, err)
	_, err = scope.snapshotForPathLocked(ctx, "missing.md")
	require.NoError(t, err)
	require.Equal(t, 1, store.metadataCalls)
	require.Equal(t, 6, store.metadataPaths)
	require.Equal(t, 5, store.typePaths)
	require.Equal(t, 5, store.assessmentPaths)
	require.Equal(t, 2, reader.contents)
}

func TestMetadataRecordFallbackRequiresProvenEligibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	propertyErr := errors.New("property read failed")
	store := &boundedHydrationStore{
		rows: map[string]semdb.NoteMetadataRow{
			"current.md": {Path: "current.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
		},
		propertyErr: propertyErr,
	}
	reader := &boundedHydrationReader{}
	scope := NewService(obsidian.VaultDefinition{}, reader, store, nil).NewScope(ctx, ScopeOptions{})
	records, err := scope.recordsForPaths(ctx, []string{"current.md"}, HydrateSummary)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, 1, reader.contents)

	store = &boundedHydrationStore{
		rows: map[string]semdb.NoteMetadataRow{
			"current.md": {Path: "current.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
		},
		tagErr: errors.New("tag read failed"),
	}
	reader = &boundedHydrationReader{}
	scope = NewService(obsidian.VaultDefinition{}, reader, store, nil).NewScope(ctx, ScopeOptions{})
	records, err = scope.recordsForPaths(ctx, []string{"current.md"}, HydrateSummary)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, 1, reader.contents)

	metadataErr := errors.New("metadata unavailable")
	store = &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{}, metadataErr: metadataErr, propertyErr: propertyErr}
	reader = &boundedHydrationReader{}
	scope = NewService(obsidian.VaultDefinition{}, reader, store, nil).NewScope(ctx, ScopeOptions{})
	_, err = scope.recordsForPaths(ctx, []string{"unknown.md"}, HydrateSummary)
	require.ErrorIs(t, err, metadataErr)
	require.Zero(t, reader.contents)
}

func TestMetadataBatchErrorDoesNotReturnOrCachePartialRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	transient := errors.New("metadata batch failed")
	store := &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{
		"a.md": {Path: "a.md", Title: "A", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
		"b.md": {Path: "b.md", Title: "B", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
	}}
	scope := NewService(obsidian.VaultDefinition{}, &boundedHydrationReader{}, store, nil).NewScope(ctx, ScopeOptions{})
	snapshot, err := scope.snapshotForPathLocked(ctx, "a.md")
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	store.metadataErr = transient

	records, err := scope.Hydrate(ctx, []ontology.NodeRef{
		{NotePath: "a.md", Kind: ontology.NodeKindNote},
		{NotePath: "b.md", Kind: ontology.NodeKindNote},
	}, HydrateOptions{Profile: HydrateSummary})
	require.ErrorIs(t, err, transient)
	require.Empty(t, records)
	require.False(t, scope.recordKnown["a.md"])
	require.False(t, scope.recordKnown["b.md"])

	store.metadataErr = nil
	records, err = scope.Hydrate(ctx, []ontology.NodeRef{
		{NotePath: "a.md", Kind: ontology.NodeKindNote},
		{NotePath: "b.md", Kind: ontology.NodeKindNote},
	}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, []string{"a.md", "b.md"}, []string{records[0].Path, records[1].Path})
}

func TestEmbeddedContentUpgradeDoesNotReuseSummaryRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
 stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section { stories: [UserStory!] @contains(level: H3) }
type UserStory implements Section @node(locator: EMBEDDED) { status: String @field }
`, "# Product\n\n## Stories\n\n### Story A\n^story-a\nstatus:: Planned\n\nDistinctive story body.\n")
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	service := NewService(vault, reader, store, schema)
	instances, err := service.NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 1)
	reader.contents = 0
	scope := service.NewScope(ctx, ScopeOptions{})
	partialRef := ontology.NodeRef{NotePath: instances.Items[0].Ref.NotePath, Kind: ontology.NodeKindEmbedded, Fragment: instances.Items[0].Ref.Fragment}
	summary, err := scope.Hydrate(ctx, []ontology.NodeRef{partialRef}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, summary, 1)
	require.Empty(t, summary[0].Content)
	content, err := scope.Hydrate(ctx, []ontology.NodeRef{partialRef}, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Len(t, content, 1)
	require.Contains(t, content[0].Content, "Distinctive story body.")
	readsAfterContent := reader.contents
	require.Positive(t, readsAfterContent)
	repeated, err := scope.Hydrate(ctx, []ontology.NodeRef{partialRef}, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Len(t, repeated, 1)
	require.Equal(t, readsAfterContent, reader.contents)
}

func TestLoadedRecordAliasIndexPreservesKindAndRejectsAmbiguity(t *testing.T) {
	t.Parallel()
	canonicalA := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, NodeID: "a", Fragment: "^a", TypeName: "Story"}
	canonicalB := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, NodeID: "b", Fragment: "^b", TypeName: "Story"}
	partialA := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, Fragment: "^a"}
	wrongKind := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindSection, Fragment: "^a"}
	ambiguous := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, Fragment: "^dup"}
	disambiguated := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, NodeID: "c", Fragment: "^dup"}
	loaded := []NodeRecord{
		{Ref: canonicalA, Title: "A"},
		{Ref: canonicalA, Title: "A duplicate row"},
		{Ref: canonicalB, Title: "B"},
		{Ref: ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, NodeID: "c", Fragment: "^dup"}, Title: "C"},
		{Ref: ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, NodeID: "d", Fragment: "^dup"}, Title: "D"},
	}
	aliases := indexLoadedRecordsForRequests([]ontology.NodeRef{canonicalB, partialA, wrongKind, ambiguous, disambiguated}, loaded)
	require.Equal(t, "B", aliases[nodeRefIdentityKey(canonicalB)].Title)
	require.Equal(t, "A", aliases[nodeRefIdentityKey(partialA)].Title)
	require.Equal(t, "C", aliases[nodeRefIdentityKey(disambiguated)].Title)
	require.NotContains(t, aliases, nodeRefIdentityKey(wrongKind))
	require.NotContains(t, aliases, nodeRefIdentityKey(ambiguous))
}

func TestEmbeddedProjectionWithNilStoreDoesNotParseRawReader(t *testing.T) {
	t.Parallel()
	reader := &boundedHydrationReader{}
	scope := NewService(obsidian.VaultDefinition{}, reader, nil, nil).NewScope(context.Background(), ScopeOptions{})
	records, err := scope.Hydrate(context.Background(), []ontology.NodeRef{{
		NotePath: "host.md", Kind: ontology.NodeKindEmbedded, Fragment: "^story",
	}}, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Empty(t, records)
	require.Zero(t, reader.contents)
}

func TestEmbeddedProjectionBatchStateErrorsFallBackPerHost(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`type UserStory implements Section @node(locator: EMBEDDED) {}`), 0o644))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	refs := []ontology.NodeRef{
		{NotePath: "a.md", Kind: ontology.NodeKindEmbedded, Fragment: "^story", TypeName: "UserStory"},
		{NotePath: "b.md", Kind: ontology.NodeKindEmbedded, Fragment: "^story", TypeName: "UserStory"},
	}
	currentRows := map[string]semdb.NoteMetadataRow{
		"a.md": {Path: "a.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
		"b.md": {Path: "b.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
	}
	for _, tc := range []struct {
		name      string
		configure func(*boundedHydrationStore)
		want      int
	}{
		{name: "metadata batch", configure: func(s *boundedHydrationStore) { s.metadataBatchErr = errors.New("metadata batch failed") }, want: 2},
		{name: "type batch", configure: func(s *boundedHydrationStore) { s.typeBatchErr = errors.New("type batch failed") }, want: 2},
		{name: "assessment batch", configure: func(s *boundedHydrationStore) { s.assessmentBatchErr = errors.New("assessment batch failed") }, want: 2},
		{name: "persistent host", configure: func(s *boundedHydrationStore) {
			s.metadataBatchErr = errors.New("metadata batch failed")
			s.metadataFailPath = "b.md"
		}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &boundedHydrationStore{rows: currentRows}
			tc.configure(store)
			reader := &boundedHydrationReader{}
			scope := NewService(obsidian.VaultDefinition{}, reader, store, schema).NewScope(context.Background(), ScopeOptions{})
			records, err := scope.embeddedRecordsFromProjectionNoOverlay(context.Background(), refs)
			require.NoError(t, err)
			require.Len(t, records, tc.want)
			require.Equal(t, tc.want, reader.contents)
		})
	}
}

func TestInboundNeighborhoodBatchesCandidateTypeLookup(t *testing.T) {
	t.Parallel()
	store := &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{}}
	for i := 0; i < 20; i++ {
		store.edges = append(store.edges, semdb.OntologyEdgeRow{SrcPath: fmt.Sprintf("notes/%02d.md", i), DstPath: "target.md", RelationName: "linked"})
	}
	scope := NewService(obsidian.VaultDefinition{}, nil, store, nil).NewScope(context.Background(), ScopeOptions{})
	result, err := scope.Neighborhood(context.Background(), NeighborhoodRequest{
		Sources:   []ontology.NodeRef{{NotePath: "target.md", Kind: ontology.NodeKindNote}},
		Direction: TraversalDirectionInbound, FirstTotal: 1,
	})
	require.NoError(t, err)
	require.Len(t, result.Edges, 1)
	require.Equal(t, 1, store.typeCalls)
	require.Equal(t, 20, store.typePaths)
}
