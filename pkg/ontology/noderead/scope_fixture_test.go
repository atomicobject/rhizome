package noderead

import (
	"context"
	"encoding/json"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func overlayIndexRecordForPath(t *testing.T, index *ReadOverlayIndex, path string) NodeRecord {
	t.Helper()
	for _, record := range index.RecordsByRef {
		if record.Ref.NotePath == path && record.Ref.Kind == ontology.NodeKindNote {
			return record
		}
	}
	t.Fatalf("overlay index missing note record for %s", path)
	return NodeRecord{}
}

func buildFixture(t *testing.T, schemaSDL, note string) (obsidian.VaultDefinition, *semdb.Store, *ontoschema.Schema) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(schemaSDL), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "product.md"), []byte(note), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	vaultDef := obsidian.VaultDefinition{Name: "noderead-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontologytest.Publish(context.Background(), testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NotNil(t, runtime.Schema)
	return vaultDef, store, runtime.Schema
}

func writeFixtureNote(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

func countNodeReadTestNodesByType(nodes []readmodel.NodeRow, typeName string) int {
	count := 0
	for _, node := range nodes {
		if node.TypeName == typeName {
			count++
		}
	}
	return count
}

func withFirstPerSource(selection RelationSelection, first int) RelationSelection {
	selection.FirstPerSource = first
	return selection
}

func withFirstTotal(selection RelationSelection, first int) RelationSelection {
	selection.FirstTotal = first
	return selection
}

func relationNames(edges []NeighborhoodEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.RelationName)
	}
	sort.Strings(out)
	return out
}

func neighborhoodTargetPaths(edges []NeighborhoodEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Target.NotePath)
	}
	sort.Strings(out)
	return out
}

func sortedFragments(refs []ontology.NodeRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Fragment)
	}
	sort.Strings(out)
	return out
}

func nodeListItemPaths(items []ontology.NodeListItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.NotePath)
	}
	return out
}

func nodeListItemRefs(items []ontology.NodeListItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Ref.String())
	}
	sort.Strings(out)
	return out
}

func sortedRecordFragments(records []NodeRecord) []string {
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.Ref.Fragment)
	}
	sort.Strings(out)
	return out
}

func mustNodeRefJSON(t *testing.T, ref ontology.NodeRef) string {
	t.Helper()
	data, err := json.Marshal(ref)
	require.NoError(t, err)
	return string(data)
}

func graphEndpointIDs(nodes []GraphEndpoint) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.ID)
	}
	sort.Strings(out)
	return out
}

func graphEndpointByID(nodes []GraphEndpoint, id string) GraphEndpoint {
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}
	return GraphEndpoint{}
}

func graphEdgeKinds(edges []GraphReadEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Kind)
	}
	sort.Strings(out)
	return out
}

func graphEdgeRelationNames(edges []GraphReadEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		if edge.RelationName != "" {
			out = append(out, edge.RelationName)
		}
	}
	sort.Strings(out)
	return out
}

func graphEdgePairKinds(edges []GraphReadEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Source+"|"+edge.Target+"|"+edge.Kind)
	}
	sort.Strings(out)
	return out
}

func graphFactPairKinds(edges []GraphFactEdge) []string {
	out := make([]string, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge.Source+"|"+edge.Target+"|"+edge.Kind)
	}
	sort.Strings(out)
	return out
}

func graphFactByPairKind(edges []GraphFactEdge, source, target, kind string) GraphFactEdge {
	for _, edge := range edges {
		if edge.Source == source && edge.Target == target && edge.Kind == kind {
			return edge
		}
	}
	return GraphFactEdge{}
}

type countingStore struct {
	*semdb.Store

	currentNoteMetadataRows       int
	ontologyAssessmentsByPaths    int
	ontologyAssessmentFlags       int
	ontologyAssessmentFlagsByPath int
	ontologyNeighborhood          int
	ontologyTypesByPaths          int
	ontologyPathsByType           int
	currentNoteMetadataRowsByPath int
	currentNotePropertyValues     int
	currentNoteTags               int
}

type countingNoteReader struct {
	*obsidian.Note
	contents int
	lists    int
}

func (r *countingNoteReader) GetContents(vaultDef obsidian.VaultDefinition, path string) (string, error) {
	r.contents++
	return r.Note.GetContents(vaultDef, path)
}

func (r *countingNoteReader) GetNotesList(vaultDef obsidian.VaultDefinition) ([]string, error) {
	r.lists++
	return r.Note.GetNotesList(vaultDef)
}

func (s *countingStore) CurrentNoteMetadataRows(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	s.currentNoteMetadataRows++
	return s.Store.CurrentNoteMetadataRows(ctx)
}

func (s *countingStore) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	s.currentNoteMetadataRowsByPath++
	return s.Store.CurrentNoteMetadataRowsByPaths(ctx, paths)
}

func (s *countingStore) CurrentNotePropertyValues(ctx context.Context, paths []string, names []string, source semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	s.currentNotePropertyValues++
	return s.Store.CurrentNotePropertyValues(ctx, paths, names, source)
}

func (s *countingStore) CurrentNoteTags(ctx context.Context, paths []string) ([]semdb.NoteTagRow, error) {
	s.currentNoteTags++
	return s.Store.CurrentNoteTags(ctx, paths)
}

func (s *countingStore) OntologyAssessmentsByPaths(ctx context.Context, paths []string) (map[string]semdb.OntologyNoteAssessmentRow, error) {
	s.ontologyAssessmentsByPaths++
	return s.Store.OntologyAssessmentsByPaths(ctx, paths)
}

func (s *countingStore) OntologyAssessmentFlags(ctx context.Context) (map[string]semdb.OntologyAssessmentFlags, error) {
	s.ontologyAssessmentFlags++
	return s.Store.OntologyAssessmentFlags(ctx)
}

func (s *countingStore) OntologyTypesByPaths(ctx context.Context, paths []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	s.ontologyTypesByPaths++
	return s.Store.OntologyTypesByPaths(ctx, paths)
}

func (s *countingStore) OntologyPathsByType(ctx context.Context, typeName string, limit int) ([]string, error) {
	s.ontologyPathsByType++
	return s.Store.OntologyPathsByType(ctx, typeName, limit)
}

func markdownRuntime(t *testing.T) noteformat.Runtime {
	t.Helper()
	provider := markdown.New()
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	return runtime
}

func (s *countingStore) OntologyNeighborhood(ctx context.Context, query readmodel.NeighborhoodQuery) ([]semdb.OntologyEdgeRow, error) {
	s.ontologyNeighborhood++
	return s.Store.OntologyNeighborhood(ctx, query)
}

func (s *countingStore) OntologyAssessmentFlagsByPaths(ctx context.Context, paths []string) (map[string]semdb.OntologyAssessmentFlags, error) {
	s.ontologyAssessmentFlagsByPath++
	return s.Store.OntologyAssessmentFlagsByPaths(ctx, paths)
}
