package operations

import (
	"context"
	"errors"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"

	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

type failedPresentationEdges struct {
	*semdb.Store
	calls int
}

func (s *failedPresentationEdges) OntologyNeighborhood(context.Context, readmodel.NeighborhoodQuery) ([]semdb.OntologyEdgeRow, error) {
	s.calls++
	return nil, errors.New("neighbor store failed")
}

func TestOntologyPresentationReportsFailedEdges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{NoteTypes: []semdb.OntologyNoteTypeRow{{NotePath: "source.md", TypeName: "Document"}}, SchemaState: semdb.OntologySchemaState{Ready: true}}))
	field := &ontoschema.Field{Name: "related", Kind: ontoschema.FieldKindLink, ContextInclude: true}
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{"Document": {Name: "Document", Fields: []*ontoschema.Field{field}, ByName: map[string]*ontoschema.Field{"related": field}}}}
	failed := &failedPresentationEdges{Store: store}
	onto := &ontologyTextContext{Store: store, Schema: schema, Scope: noderead.NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, failed, schema).NewScope(noderead.ScopeOptions{})}
	got := renderNoteOntologyBlockWithContext(ctx, "source.md", onto)
	require.Contains(t, got, "ontology_read_failed")
	require.Contains(t, got, "neighbor store failed")
	require.Equal(t, 1, failed.calls)
	got, _ = renderNoteContextIncludesWithContext(ctx, &obsidian.Note{}, obsidian.VaultDefinition{Path: root}, "source.md", onto, 1000, nil)
	require.Contains(t, got, "ontology_read_failed")
	require.Contains(t, got, "neighbor store failed")
	require.Equal(t, 2, failed.calls)
}
