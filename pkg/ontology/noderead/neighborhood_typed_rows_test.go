package noderead

import (
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

type edgeRowCountingStore struct {
	ReadStore
	rows int
}

func (s *edgeRowCountingStore) OntologyNeighborhood(ctx context.Context, query readmodel.NeighborhoodQuery) ([]semdb.OntologyEdgeRow, error) {
	rows, err := s.ReadStore.OntologyNeighborhood(ctx, query)
	s.rows += len(rows)
	return rows, err
}

// SPEC-0136 R8: typed neighbours filter the target type in SQL, so rows read
// stay within a constant multiple of rows returned however many neighbours of
// other types each record has.
func TestTypedNeighboursReadRowsProportionalToResult(t *testing.T) {
	const sources, wantedPer, otherPer = 40, 2, 25
	var edges []semdb.OntologyEdgeRow
	var refs []ontology.NodeRef
	for i := range sources {
		src := fmt.Sprintf("s/%02d.md", i)
		refs = append(refs, ontology.NodeRef{NotePath: src, Kind: ontology.NodeKindNote, TypeName: "Concept"})
		for j := range wantedPer {
			edges = append(edges, semdb.OntologyEdgeRow{SrcPath: src, RelationName: "linked", DstPath: fmt.Sprintf("a/%02d-%d.md", i, j), DstType: "Assertion", Provenance: "body_link"})
		}
		for j := range otherPer {
			edges = append(edges, semdb.OntologyEdgeRow{SrcPath: src, RelationName: "linked", DstPath: fmt.Sprintf("o/%02d-%d.md", i, j), DstType: "Source", Provenance: "body_link"})
		}
	}
	schema := &ontoschema.Schema{
		Types:      map[string]*ontoschema.NoteType{"Assertion": {Name: "Assertion", Implements: []string{"Claim"}}, "Source": {Name: "Source"}, "Concept": {Name: "Concept"}},
		Interfaces: map[string]*ontoschema.InterfaceType{"Claim": {Name: "Claim"}},
	}
	for _, tc := range []struct {
		name              string
		types, interfaces []string
	}{
		{"concrete type", []string{"Assertion"}, nil},
		{"interface", nil, []string{"Claim"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := neighborhoodCatalogScope(t, nil, edges)
			scope.service.Schema = schema
			store := &edgeRowCountingStore{ReadStore: scope.service.Store}
			scope.service.Store = store
			result, err := scope.Edges(t.Context(), NeighborhoodRequest{
				Sources: refs, Direction: TraversalDirectionOutbound, Provenance: map[string]struct{}{"body_link": {}},
				IncludeAmbient: true, TargetTypes: tc.types, TargetInterfaces: tc.interfaces, FirstPerSource: 200,
			})
			require.NoError(t, err)
			require.Len(t, result.Edges, sources*wantedPer)
			require.LessOrEqual(t, store.rows, 2*len(result.Edges), "rows read for typed neighbours must track rows returned")
		})
	}
}
