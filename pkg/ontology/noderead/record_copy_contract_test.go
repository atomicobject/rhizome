package noderead

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestHydrateCopiesTypedFieldValuesAcrossCacheHits(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, "index.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ref := ontology.NodeRef{NotePath: "host.md", Kind: ontology.NodeKindEmbedded, Fragment: "^item", NodeID: "host.md#^item"}
	encoded, err := json.Marshal(ref)
	require.NoError(t, err)
	boolValue, intValue, realValue, dateValue, timestampValue := true, int64(7), 1.5, "2026-10-17", "2026-10-17T12:00:00Z"
	rows := []readmodel.FieldValueRow{
		{FieldName: "done", ValueBool: &boolValue},
		{FieldName: "points", ValueInt: &intValue},
		{FieldName: "ratio", ValueReal: &realValue},
		{FieldName: "date", ValueDate: &dateValue},
		{FieldName: "timestamp", ValueDateTime: &timestampValue},
	}
	for i := range rows {
		rows[i].NodeID, rows[i].NotePath = "catalog-item", ref.NotePath
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, readmodel.NodeReadModel{
		NotePaths:   []string{ref.NotePath},
		Nodes:       []readmodel.NodeRow{{NodeID: "catalog-item", NotePath: ref.NotePath, NodeRefJSON: string(encoded), NodeKind: string(ref.Kind), SourceLocator: ontology.NodeSourceLocator(ref)}},
		FieldValues: rows,
	}))
	scope := NewService(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, nil).NewScope(ScopeOptions{})
	first, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, first, 1)
	*first[0].FieldValues["done"][0].ValueBool = false
	*first[0].FieldValues["points"][0].ValueInt = 99
	*first[0].FieldValues["ratio"][0].ValueReal = 99
	*first[0].FieldValues["date"][0].ValueDate = "2099-01-01"
	*first[0].FieldValues["timestamp"][0].ValueDateTime = "2099-01-01T00:00:00Z"
	again, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, true, *again[0].FieldValues["done"][0].ValueBool)
	require.Equal(t, int64(7), *again[0].FieldValues["points"][0].ValueInt)
	require.Equal(t, 1.5, *again[0].FieldValues["ratio"][0].ValueReal)
	require.Equal(t, "2026-10-17", *again[0].FieldValues["date"][0].ValueDate)
	require.Equal(t, "2026-10-17T12:00:00Z", *again[0].FieldValues["timestamp"][0].ValueDateTime)
}
