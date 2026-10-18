package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestOntologyNeighborhoodFiltersAndLimitsExactEndpoints(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{Edges: []OntologyEdgeRow{
		{SrcPath: "a.md", RelationName: "one", DstPath: "b.md", Provenance: "field", Structural: true},
		{SrcPath: "a.md", RelationName: "two", DstPath: "c.md", Provenance: "field", Structural: true},
		{SrcPath: "x.md", RelationName: "one", DstPath: "a.md", Provenance: "body_link"},
		{SrcPath: "a.md", SrcNodeID: "embedded", RelationName: "one", DstPath: "d.md", Provenance: "field", Structural: true},
	}, SchemaState: OntologySchemaState{Ready: true}}))
	for _, tc := range []struct {
		name string
		q    readmodel.NeighborhoodQuery
		want []string
	}{
		{"outbound", readmodel.NeighborhoodQuery{Direction: "outbound", IncludeStructural: true, IncludeAmbient: true}, []string{"b.md", "c.md"}},
		{"inbound", readmodel.NeighborhoodQuery{Direction: "inbound", IncludeStructural: true, IncludeAmbient: true}, []string{"a.md"}},
		{"multi relation", readmodel.NeighborhoodQuery{Direction: "outbound", Relations: []string{"one", "two"}, IncludeStructural: true}, []string{"b.md", "c.md"}},
		{"filter before limit", readmodel.NeighborhoodQuery{Direction: "outbound", Relations: []string{"two"}, IncludeStructural: true, PerSourceLimit: 1}, []string{"c.md"}},
		{"bounded", readmodel.NeighborhoodQuery{Direction: "outbound", IncludeStructural: true, PerSourceLimit: 1}, []string{"b.md"}},
		{"embedded", readmodel.NeighborhoodQuery{Sources: []readmodel.EdgeEndpoint{{Path: "a.md", NodeID: "embedded"}}, Direction: "outbound", IncludeStructural: true}, []string{"d.md"}},
		{"provenance", readmodel.NeighborhoodQuery{Direction: "both", Provenance: []string{"body_link"}, IncludeStructural: true, IncludeAmbient: true}, []string{"a.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.q.Sources) == 0 {
				tc.q.Sources = []readmodel.EdgeEndpoint{{Path: "a.md"}}
			}
			rows, err := store.OntologyNeighborhood(ctx, tc.q)
			require.NoError(t, err)
			got := make([]string, 0, len(rows))
			for _, row := range rows {
				got = append(got, row.DstPath)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestOntologyAssessmentFlagsByPathsDoesNotDecodeAssessments(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.db.ExecContext(ctx, `INSERT INTO ontology_note_assessments(note_path,declared_type,resolved_type,assessment_json,schema_hash,updated_at,has_issues,type_ambiguous) VALUES('a.md','','','invalid JSON','schema',1,1,0),('b.md','','','invalid JSON','schema',1,0,1)`)
	require.NoError(t, err)
	flags, err := store.OntologyAssessmentFlagsByPaths(ctx, []string{"a.md", "missing.md"})
	require.NoError(t, err)
	require.Equal(t, map[string]OntologyAssessmentFlags{"a.md": {HasIssues: true}}, flags)
}

func TestOntologyNeighborhoodKeepsInboundSelfLoopInFirstPage(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{Edges: []OntologyEdgeRow{
		{SrcPath: "a.md", DstPath: "a.md", RelationName: "related", Structural: true},
		{SrcPath: "b.md", DstPath: "a.md", RelationName: "related", Structural: true},
		{SrcPath: "c.md", DstPath: "a.md", RelationName: "related", Structural: true},
	}}))
	rows, err := store.OntologyNeighborhood(ctx, readmodel.NeighborhoodQuery{Sources: []readmodel.EdgeEndpoint{{Path: "a.md"}}, Direction: "inbound", IncludeStructural: true, PerSourceLimit: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "a.md", rows[0].SrcPath)
}

func TestOntologyNeighborhoodDeduplicatesAcrossSourceBatches(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	edge := OntologyEdgeRow{SrcPath: "first.md", DstPath: "last.md", RelationName: "related", Structural: true}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{Edges: []OntologyEdgeRow{edge}}))
	sources := []readmodel.EdgeEndpoint{{Path: edge.SrcPath}}
	for i := 0; i < 500; i++ {
		sources = append(sources, readmodel.EdgeEndpoint{Path: fmt.Sprintf("unrelated-%d.md", i)})
	}
	sources = append(sources, readmodel.EdgeEndpoint{Path: edge.DstPath})
	rows, err := store.OntologyNeighborhood(ctx, readmodel.NeighborhoodQuery{Sources: sources, Direction: "both", IncludeStructural: true})
	require.NoError(t, err)
	require.Equal(t, []OntologyEdgeRow{edge}, rows)
}
