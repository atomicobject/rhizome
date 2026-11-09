package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
)

// SPEC-0136 R8: relation lists cost the same per parent at any parent count,
// so every batch of a 5,000-parent read must seek the edge index.
func TestOntologyNeighborhoodSeeksEdgeIndexAt5000Parents(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	const parents = 5000
	edges := make([]OntologyEdgeRow, 0, parents*2)
	sources := make([]readmodel.EdgeEndpoint, 0, parents)
	for i := range parents {
		src := fmt.Sprintf("p/%05d.md", i)
		sources = append(sources, readmodel.EdgeEndpoint{Path: src})
		edges = append(edges,
			OntologyEdgeRow{SrcPath: src, RelationName: "rel", DstPath: fmt.Sprintf("t/%05d.md", i), DstType: "Target", Provenance: "field", Structural: true},
			OntologyEdgeRow{SrcPath: src, RelationName: "body", DstPath: fmt.Sprintf("o/%05d.md", i), DstType: "Other", Provenance: "body_link"},
		)
	}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{Edges: edges, SchemaState: OntologySchemaState{Ready: true}}))
	for _, analyzed := range []bool{false, true} {
		if analyzed {
			_, err := store.db.ExecContext(ctx, "ANALYZE")
			require.NoError(t, err)
		}
		for _, q := range []readmodel.NeighborhoodQuery{
			{Direction: "outbound", Relations: []string{"rel"}, IncludeStructural: true},
			{Direction: "outbound", Provenance: []string{"body_link"}, DstTypes: []string{"Other"}, IncludeAmbient: true, PerSourceLimit: 21},
			{Direction: "inbound", IncludeStructural: true, IncludeAmbient: true},
			{Direction: "both", IncludeStructural: true},
		} {
			t.Run(fmt.Sprintf("%s/analyzed=%v", q.Direction, analyzed), func(t *testing.T) {
				batchSize := (sqliteValuesBatchMaxParams - len(q.Relations) - len(q.Provenance) - len(q.DstTypes) - 1) / 2
				for start := 0; start < parents; start += batchSize {
					query, args := ontologyNeighborhoodSQL(sources[start:min(start+batchSize, parents)], q)
					plan := explainQueryPlan(t, store, query, args)
					require.Regexp(t, `SEARCH e USING (PRIMARY KEY|(COVERING )?INDEX idx_ontology_edges_\w+|MULTI-INDEX OR)`, plan)
					require.NotContains(t, plan, "SCAN e")
					require.NotContains(t, plan, "AUTOMATIC")
				}
				rows, err := store.OntologyNeighborhood(ctx, readmodel.NeighborhoodQuery{Sources: sources, Direction: q.Direction, Relations: q.Relations, Provenance: q.Provenance, DstTypes: q.DstTypes, IncludeStructural: q.IncludeStructural, IncludeAmbient: q.IncludeAmbient, PerSourceLimit: q.PerSourceLimit})
				require.NoError(t, err)
				want := parents
				if q.Direction == "inbound" {
					want = 0
				}
				require.Len(t, rows, want)
			})
		}
	}
}

func TestOntologyNeighborhoodNarrowsOutboundTargetTypes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{Edges: []OntologyEdgeRow{
		{SrcPath: "a.md", RelationName: "r", DstPath: "keep.md", DstType: "Wanted", Provenance: "body_link"},
		{SrcPath: "a.md", RelationName: "r", DstPath: "drop.md", DstType: "Other", Provenance: "body_link"},
		{SrcPath: "a.md", RelationName: "r", DstPath: "untyped.md", Provenance: "body_link"},
		{SrcPath: "a.md", RelationName: "r", DstPath: "host.md", DstNodeID: "host.md#^x", DstType: "Other", Provenance: "body_link"},
	}}))
	rows, err := store.OntologyNeighborhood(ctx, readmodel.NeighborhoodQuery{Sources: []readmodel.EdgeEndpoint{{Path: "a.md"}}, Direction: "outbound", DstTypes: []string{"Wanted"}, IncludeAmbient: true})
	require.NoError(t, err)
	var got []string
	for _, row := range rows {
		got = append(got, row.DstPath)
	}
	require.ElementsMatch(t, []string{"keep.md", "untyped.md", "host.md"}, got)
}

func explainQueryPlan(t *testing.T, store *Store, query string, args []any) string {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	require.NoError(t, err)
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		details = append(details, detail)
	}
	require.NoError(t, rows.Err())
	return strings.Join(details, "\n")
}

// The other VALUES-driven reads must also seek their indexes at large n.
func TestRequestedPathJoinsSeekIndexes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	for _, tc := range []struct {
		name  string
		query string
		n     int
		want  []string
	}{
		{"call anchors", callAnchorsByPathsSQL(5000), 5000, []string{"SEARCH caller USING COVERING INDEX idx_intel_code_anchors_path", "SEARCH e USING", "SEARCH r USING COVERING INDEX idx_intel_go_derived_relationship_source", "SEARCH callee USING INDEX idx_intel_code_anchors_lang_fqn"}},
		{"ownership states", ownershipCurrentStatesSQL(400), 800, []string{"SEARCH n USING", "SEARCH files USING"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := make([]any, tc.n)
			for i := range args {
				args[i] = fmt.Sprintf("p/%05d.go", i)
			}
			plan := explainQueryPlan(t, store, tc.query, args)
			t.Log(plan)
			for _, want := range tc.want {
				require.Contains(t, plan, want)
			}
			require.NotContains(t, plan, "AUTOMATIC")
		})
	}
}
