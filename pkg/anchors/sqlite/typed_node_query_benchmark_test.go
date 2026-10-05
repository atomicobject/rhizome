package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func BenchmarkTypedSortUnrelatedRows(b *testing.B) {
	ctx := context.Background()
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "index.db"))
			require.NoError(b, err)
			b.Cleanup(func() { _ = store.Close() })
			model := codeanchor.IntelOntologyNodeReadModel{}
			for i := 0; i < count; i++ {
				path := fmt.Sprintf("notes/%05d.md", i)
				id := fmt.Sprintf("n%d", i)
				typ := "Other"
				if i < 10 {
					typ = "Wanted"
				}
				model.NotePaths = append(model.NotePaths, path)
				model.Nodes = append(model.Nodes, codeanchor.IntelOntologyNode{NodeID: id, NotePath: path, NodeRefJSON: "{}", NodeKind: "NOTE", TypeName: typ, Title: path, UpdatedAt: 1})
				model.FieldValues = append(model.FieldValues, codeanchor.IntelOntologyNodeFieldValue{NodeID: id, NotePath: path, TypeName: typ, FieldName: "rank", ValueKind: "string", ValueText: id, ValueNorm: id, UpdatedAt: 1})
			}
			require.NoError(b, store.ReplaceOntologyNodeReadModel(ctx, model))
			plan := codeanchor.OntologyNodeQueryPlan{TypeNames: []string{"Wanted"}, Limit: 5, Sort: []codeanchor.OntologyFieldSort{{FieldName: "rank", ValueKind: "string", NullsLast: true}}}
			rows, err := store.OntologyNodesByTypePlan(ctx, plan)
			require.NoError(b, err)
			require.Len(b, rows, 5)
			wantIDs := []string{"n0", "n1", "n2", "n3", "n4"}
			for i, row := range rows {
				require.Equal(b, wantIDs[i], row.NodeID)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows, err := store.OntologyNodesByTypePlan(ctx, plan)
				if err != nil || len(rows) != 5 {
					b.Fatalf("rows=%d err=%v", len(rows), err)
				}
				for j, row := range rows {
					if row.NodeID != wantIDs[j] {
						b.Fatalf("unexpected result at %d: %s", j, row.NodeID)
					}
				}
			}
		})
	}
}
