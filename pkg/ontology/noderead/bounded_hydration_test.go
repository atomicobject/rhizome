package noderead

import (
	"context"
	"errors"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type boundedHydrationStore struct {
	ReadStore
	rows                                               map[string]semdb.NoteMetadataRow
	edges                                              []semdb.OntologyEdgeRow
	metadataCalls, metadataPaths, typeCalls, typePaths int
	assessmentCalls, assessmentPaths                   int
	assessmentFlagCalls                                int
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

func (s *boundedHydrationStore) OntologyNeighborhood(context.Context, readmodel.NeighborhoodQuery) ([]semdb.OntologyEdgeRow, error) {
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

func TestScopeHydrateEmbeddedSnapshotsOnlyEligibleHosts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	vault, real, schema := buildFixture(t, `type UserStory implements Section @node(locator: EMBEDDED) {}`, "# Unrelated\n")
	store := &boundedHydrationStore{ReadStore: real, rows: map[string]semdb.NoteMetadataRow{
		"current.md":    {Path: "current.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
		"second.md":     {Path: "second.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
		"stale.md":      {Path: "stale.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusStale}},
		"fatal.md":      {Path: "fatal.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusFatal}},
		"descriptor.md": {Path: "descriptor.md", FormatID: "descriptor", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
	}}
	reader := &boundedHydrationReader{}
	scope := NewService(vault, reader, store, schema).NewScope(ScopeOptions{})
	refs := make([]ontology.NodeRef, 0, 6)
	for _, path := range []string{"current.md", "second.md", "stale.md", "fatal.md", "descriptor.md", "missing.md"} {
		refs = append(refs, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindEmbedded, Fragment: "^story", TypeName: "UserStory"})
	}
	records, err := scope.Hydrate(ctx, refs, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, []string{"current.md", "second.md"}, []string{records[0].Path, records[1].Path})
	require.Equal(t, 2, reader.contents)
	require.Equal(t, 1, store.metadataCalls)
	require.Equal(t, 6, store.metadataPaths)
	repeated, err := scope.Hydrate(ctx, refs, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Equal(t, records, repeated)
	require.Equal(t, 1, store.metadataCalls)
	require.Equal(t, 2, reader.contents)
}

func TestScopeSummaryStoreFailuresPropagateWithoutSourceFallback(t *testing.T) {
	for _, boundary := range []string{"metadata", "properties", "tags"} {
		t.Run(boundary, func(t *testing.T) {
			sentinel := errors.New(boundary + " unavailable")
			store := newBoundedHydrationStore(t, &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{
				"current.md": {Path: "current.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
			}})
			switch boundary {
			case "metadata":
				store.metadataErr = sentinel
			case "properties":
				store.propertyErr = sentinel
			case "tags":
				store.tagErr = sentinel
			}
			reader := &boundedHydrationReader{}
			scope := NewService(obsidian.VaultDefinition{}, reader, store, nil).NewScope(ScopeOptions{})
			records, err := scope.Hydrate(context.Background(), []ontology.NodeRef{{NotePath: "current.md", Kind: ontology.NodeKindNote}}, HydrateOptions{Profile: HydrateSummary})
			require.ErrorIs(t, err, sentinel)
			require.Empty(t, records)
			require.Zero(t, reader.contents)
		})
	}
}

func TestMetadataBatchErrorDoesNotReturnOrCachePartialRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	transient := errors.New("metadata batch failed")
	store := newBoundedHydrationStore(t, &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{
		"a.md": {Path: "a.md", Title: "A", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
		"b.md": {Path: "b.md", Title: "B", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
	}})
	scope := NewService(obsidian.VaultDefinition{}, &boundedHydrationReader{}, store, nil).NewScope(ScopeOptions{})
	updated, err := scope.NoteUpdatedAt(ctx, []string{"a.md"})
	require.NoError(t, err)
	require.Contains(t, updated, "a.md")
	store.metadataErr = transient

	records, err := scope.Hydrate(ctx, []ontology.NodeRef{
		{NotePath: "a.md", Kind: ontology.NodeKindNote},
		{NotePath: "b.md", Kind: ontology.NodeKindNote},
	}, HydrateOptions{Profile: HydrateSummary})
	require.ErrorIs(t, err, transient)
	require.Empty(t, records)

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
	instances, err := service.NewScope(ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Len(t, instances.Items, 1)
	reader.contents = 0
	scope := service.NewScope(ScopeOptions{})
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

func TestScopeHydrateAliasesPreserveKindAndRejectAmbiguity(t *testing.T) {
	t.Parallel()
	canonical := map[string]ontology.NodeRef{}
	rows := map[string]readmodel.NodeRow{}
	for _, id := range []string{"a", "b", "c", "d"} {
		fragment := "^" + id
		if id == "c" || id == "d" {
			fragment = "^dup"
		}
		ref := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, NodeID: id, Fragment: fragment, TypeName: "Story"}
		canonical[id] = ref
		rows["host.md#"+id] = readmodel.NodeRow{NodeID: id, NotePath: "host.md", NodeKind: string(ontology.NodeKindEmbedded),
			NodeRefJSON: mustNodeRefJSON(t, ref), Title: id, SourceLocator: "host.md#" + fragment}
	}
	base := newBoundedHydrationStore(t, &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{}})
	store := &aliasHydrationStore{ReadStore: base, rows: rows}
	scope := NewService(obsidian.VaultDefinition{}, &boundedHydrationReader{}, store, nil).NewScope(ScopeOptions{})
	ref := func(id string) ontology.NodeRef {
		return ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, NodeID: id}
	}
	requests := []ontology.NodeRef{
		ref("b"),
		{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, Fragment: "^a"},
		{NotePath: "host.md", Kind: ontology.NodeKindSection, Fragment: "^a"},
		{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, Fragment: "^dup"},
		canonical["c"], ref("a"), ref("c"), ref("d"),
	}
	records, err := scope.Hydrate(context.Background(), requests, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	identities := make([]string, 0, len(records))
	for _, record := range records {
		identities = append(identities, record.Ref.NodeID)
	}
	require.Equal(t, []string{"b", "a", "c", "a", "c", "d"}, identities)
	for _, record := range records {
		require.Equal(t, ontology.NodeKindEmbedded, record.Ref.Kind)
	}
}

type aliasHydrationStore struct {
	ReadStore
	rows map[string]readmodel.NodeRow
}

func (s *aliasHydrationStore) OntologyNodesBySourceLocators(_ context.Context, locators []string) (map[string]readmodel.NodeRow, error) {
	rows := map[string]readmodel.NodeRow{}
	for _, locator := range locators {
		if row, ok := s.rows[locator]; ok {
			rows[locator] = row
		}
	}
	return rows, nil
}

func TestEmbeddedProjectionWithNilStoreDoesNotParseRawReader(t *testing.T) {
	t.Parallel()
	reader := &boundedHydrationReader{}
	scope := NewService(obsidian.VaultDefinition{}, reader, nil, nil).NewScope(ScopeOptions{})
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
	schema, err := ontoschema.LoadSchema(root)
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
			store := newBoundedHydrationStore(t, &boundedHydrationStore{rows: currentRows})
			tc.configure(store)
			reader := &boundedHydrationReader{}
			scope := NewService(obsidian.VaultDefinition{}, reader, store, schema).NewScope(ScopeOptions{})
			records, err := scope.Hydrate(context.Background(), refs, HydrateOptions{Profile: HydrateContent})
			require.NoError(t, err)
			require.Len(t, records, tc.want)
			require.Equal(t, tc.want, reader.contents)
		})
	}
}

func TestInboundNeighborhoodBatchesCandidateTypeLookup(t *testing.T) {
	t.Parallel()
	store := newBoundedHydrationStore(t, &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{}})
	for i := 0; i < 20; i++ {
		store.edges = append(store.edges, semdb.OntologyEdgeRow{SrcPath: fmt.Sprintf("notes/%02d.md", i), DstPath: "target.md", RelationName: "linked"})
	}
	scope := NewService(obsidian.VaultDefinition{}, nil, store, nil).NewScope(ScopeOptions{})
	result, err := scope.Neighborhood(context.Background(), NeighborhoodRequest{
		Sources:   []ontology.NodeRef{{NotePath: "target.md", Kind: ontology.NodeKindNote}},
		Direction: TraversalDirectionInbound, FirstTotal: 1,
	})
	require.NoError(t, err)
	require.Len(t, result.Edges, 1)
	require.Equal(t, 1, store.typeCalls)
}

func newBoundedHydrationStore(t *testing.T, store *boundedHydrationStore) *boundedHydrationStore {
	t.Helper()
	real, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = real.Close() })
	store.ReadStore = real
	return store
}

func (s *boundedHydrationStore) OntologyAssessmentFlagsByPaths(_ context.Context, paths []string) (map[string]semdb.OntologyAssessmentFlags, error) {
	s.assessmentFlagCalls++
	if len(paths) > 1 && s.assessmentBatchErr != nil {
		return nil, s.assessmentBatchErr
	}
	return map[string]semdb.OntologyAssessmentFlags{}, nil
}
