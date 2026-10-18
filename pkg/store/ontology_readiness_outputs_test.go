package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOntologyReadinessSnapshotIncludesAllOutputsAndOrphanSources(t *testing.T) {
	store, delta := readinessFixture(t)
	delta.NoteTypes = append(delta.NoteTypes, OntologyNoteTypeRow{
		NotePath: "orphan.md", TypeName: "Stale", SchemaHash: "old-schema", UpdatedAt: 3,
	})
	delta.Edges = append(delta.Edges, OntologyEdgeRow{
		SrcPath: "orphan.md", SrcNodeID: "orphan-node", RelationName: "stale", DstPath: "gone.md",
		DstNodeID: "gone-node", DstType: "Stale", Provenance: "body_link", Structural: true, SchemaHash: "old-schema", UpdatedAt: 3,
	})
	delta.ReadModels[0].LinkDependencies = append(delta.ReadModels[0].LinkDependencies, readmodel.LinkDependencyRow{
		SourceNotePath: "orphan.md", NodeID: "orphan-node", TypeName: "Stale", FieldName: "stale",
		TargetInput: "Gone", TargetInputNorm: "gone", ResolvedTargetNotePath: "gone.md", ResolvedTargetTypeName: "Stale",
		SchemaHash: "old-schema", UpdatedAt: 3,
	})
	delta.TypePolicies = append(delta.TypePolicies, OntologyTypePolicyRow{
		TypeName: "Stale", PolicyJSON: `{"stale":true}`, SchemaHash: "old-schema", UpdatedAt: 3,
	})
	called := false
	delta.Readiness.Validate = func(snapshot OntologyReadinessSnapshot) (bool, error) {
		called = true
		require.Equal(t, delta.NoteTypes, snapshot.NoteTypes)
		require.Equal(t, delta.Edges, snapshot.Edges)
		require.Equal(t, delta.ReadModels[0].LinkDependencies, snapshot.LinkDependencies)
		require.Equal(t, delta.TypePolicies, snapshot.TypePolicies)
		return true, nil
	}
	require.NoError(t, store.ApplyOntologyDelta(context.Background(), delta))
	require.True(t, called)
}

func TestOntologyDeltaEmptyPolicyReplacementSupersedesBufferedPolicies(t *testing.T) {
	store, initial := readinessFixture(t)
	ctx := context.Background()
	require.NoError(t, store.ApplyOntologyDelta(ctx, initial))
	var pending OntologyDelta
	MergeOntologyDelta(&pending, OntologyDelta{TypePolicies: []OntologyTypePolicyRow{{TypeName: "Old", PolicyJSON: "{}", SchemaHash: "schema-v1"}}})
	MergeOntologyDelta(&pending, OntologyDelta{ReplaceTypePolicies: true})
	require.Positive(t, pending.RowCount())
	require.NoError(t, store.ApplyOntologyDelta(ctx, pending))
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_type_policies`).Scan(&count))
	require.Zero(t, count)
	requireReadiness(t, store, false)
}

func TestIndexedOntologySummaryRequiresCurrentProviderProof(t *testing.T) {
	ctx := context.Background()
	store, delta := readinessFixture(t)
	require.NoError(t, store.ApplyOntologyDelta(ctx, delta))
	before, err := store.IndexedOntologySummary(ctx, "provider-witness-v1", 10, 1)
	require.NoError(t, err)
	require.True(t, before.Ready)
	wrongProvider, err := store.IndexedOntologySummary(ctx, "different-provider", 10, 1)
	require.NoError(t, err)
	require.False(t, wrongProvider.Ready)
	_, err = store.db.ExecContext(ctx, `DELETE FROM ontology_readiness`)
	require.NoError(t, err)
	legacy, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.True(t, legacy.Ready)
	after, err := store.IndexedOntologySummary(ctx, "provider-witness-v1", 10, 1)
	require.NoError(t, err)
	require.False(t, after.Ready)
	before.Ready = false
	require.Equal(t, before, after, "invalidating only the proof preserves all summary facts")
}

func TestOntologyDeltaPolicyReplacementKeepsLatestMergedUpsert(t *testing.T) {
	ctx := context.Background()
	store, initial := readinessFixture(t)
	require.NoError(t, store.ApplyOntologyDelta(ctx, initial))
	var pending OntologyDelta
	MergeOntologyDelta(&pending, OntologyDelta{ReplaceTypePolicies: true, TypePolicies: []OntologyTypePolicyRow{
		{TypeName: "A", PolicyJSON: `{"version":"old"}`, SchemaHash: "schema-v1"},
		{TypeName: "B", PolicyJSON: `{"retained":true}`, SchemaHash: "schema-v1"},
	}})
	for i := 0; i < 32; i++ {
		MergeOntologyDelta(&pending, OntologyDelta{TypePolicies: []OntologyTypePolicyRow{
			{TypeName: "A", PolicyJSON: fmt.Sprintf(`{"intermediate":%d}`, i), SchemaHash: "schema-v1"},
		}})
	}
	MergeOntologyDelta(&pending, OntologyDelta{TypePolicies: []OntologyTypePolicyRow{
		{TypeName: "A", PolicyJSON: `{"version":"new"}`, SchemaHash: "schema-v1"},
	}})
	require.NoError(t, store.ApplyOntologyDelta(ctx, pending))
	rows, err := store.db.QueryContext(ctx, `SELECT type_name, policy_json FROM ontology_type_policies ORDER BY type_name`)
	require.NoError(t, err)
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var name, policy string
		require.NoError(t, rows.Scan(&name, &policy))
		actual = append(actual, name+":"+policy)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{`A:{"version":"new"}`, `B:{"retained":true}`}, actual)
}
