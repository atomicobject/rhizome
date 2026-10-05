package noderead

import (
	"context"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func neighborhoodErrorFixture(t *testing.T) (*semdb.Store, string, *ontology.Schema) {
	t.Helper()
	root := t.TempDir()
	store := openWalkStore(t, root)
	ctx := context.Background()
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		NoteTypes: []semdb.OntologyNoteTypeRow{
			{NotePath: "people/Alice.md", TypeName: "Person", SchemaHash: "abc", UpdatedAt: 1},
			{NotePath: "teams/Eng.md", TypeName: "Team", SchemaHash: "abc", UpdatedAt: 1},
		},
		Edges: []semdb.OntologyEdgeRow{
			{SrcPath: "teams/Eng.md", RelationName: "members", DstPath: "people/Alice.md", DstType: "Person", Provenance: "field", Structural: true, SchemaHash: "abc", UpdatedAt: 1},
		},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "abc", NotesHash: "def", LoadedAt: 1, Ready: true},
	}))
	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{
		"Person": {Name: "Person"},
		"Team":   {Name: "Team", Implements: []string{"Group"}},
	}}
	return store, root, schema
}

func newNeighborhoodScope(t *testing.T, store *semdb.Store, root string, schema *ontology.Schema) *Scope {
	t.Helper()
	return NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, schema).NewScope(context.Background(), ScopeOptions{})
}

// The inbound target-type read is the only source of a target's type for
// inbound edges; when it fails the caller must see the error rather than a
// successful, silently filtered result.
func TestNeighborhoodInboundTypeReadFailureSurfaces(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  NeighborhoodRequest
	}{
		{
			name: "target types inbound",
			req: NeighborhoodRequest{
				Sources:           []ontology.NodeRef{{NotePath: "people/Alice.md", Kind: ontology.NodeKindNote}},
				Direction:         TraversalDirectionInbound,
				IncludeStructural: true,
				IncludeAmbient:    true,
				TargetTypes:       []string{"Team"},
			},
		},
		{
			name: "target interfaces both",
			req: NeighborhoodRequest{
				Sources:           []ontology.NodeRef{{NotePath: "people/Alice.md", Kind: ontology.NodeKindNote}},
				Direction:         TraversalDirectionBoth,
				IncludeStructural: true,
				IncludeAmbient:    true,
				TargetInterfaces:  []string{"Group"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, root, schema := neighborhoodErrorFixture(t)
			ctx := context.Background()

			healthy, err := newNeighborhoodScope(t, store, root, schema).Neighborhood(ctx, tc.req)
			require.NoError(t, err)
			require.Len(t, healthy.Edges, 1, "sanity: inbound edge is visible when the type read works")

			// The edge read still works; only the type read fails.
			_, err = store.DB().ExecContext(ctx, `DROP TABLE ontology_note_types`)
			require.NoError(t, err)

			result, err := newNeighborhoodScope(t, store, root, schema).Neighborhood(ctx, tc.req)
			require.Error(t, err, "type read failure was swallowed; edges returned=%d", len(result.Edges))
			require.ErrorContains(t, err, "no such table")
		})
	}
}
