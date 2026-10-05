package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestCommittedFieldLineageSupportsAnotherEditAfterMultipleItemOffsetsShift(t *testing.T) {
	ctx := context.Background()
	fixture := prepareOntologyFixtureVault(t)
	schemaPath := filepath.Join(fixture.root, ".rhizome/ontology/schema.graphql")
	schema, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(schemaPath, []byte(strings.Replace(string(schema), "  summary: String! @field(sourceKind: ITEM_SUMMARY)\n", "  summary: String! @field(sourceKind: ITEM_SUMMARY)\n  verification: String @field\n", 1)), 0o644))
	path := filepath.Join(fixture.root, "specs/100-demo/spec.md")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(source), "- Story tied to validation checks.\n", "- Story tied to validation checks.\n  verification:: initial\n")+"- Untouched sibling.\n"), 0o644))
	_, err = ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	srv := newFixtureServer(t, fixture, nil)
	defs, err := srv.ontologyDefinitions()
	require.NoError(t, err)
	listed, err := srv.nodeReadScope(ctx, defs).TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: "Criterion"})
	require.NoError(t, err)
	require.Len(t, listed.Items, 3)
	var first, second, untouched ontology.NodeRef
	for _, item := range listed.Items {
		switch item.Title {
		case "Story tied to validation checks.":
			first = item.Ref
		case "Follow [[plan]] while validating.":
			second = item.Ref
		case "Untouched sibling.":
			untouched = item.Ref
		}
	}
	op := func(ref ontology.NodeRef, value string) OntologyEditOp {
		return OntologyEditOp{Kind: "setField", Path: ref.String(), NodeID: ref.NodeID, Structural: ref.Structural, Field: "verification", Value: value}
	}
	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{
		op(first, "First criterion has a much longer summary."), op(second, "Second criterion changed."),
	}})
	require.NoError(t, err)
	request := OntologyEditSessionCommitRequest{RequestID: "item-lineage", ExpectedRevision: created.Revision}
	committed, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, request)
	require.NoError(t, err)
	require.Equal(t, ontology.CommitOutcomeCommitted, committed.Outcome)
	require.Empty(t, committed.Warnings)
	require.Len(t, committed.RefLineage, 2)
	var mapped ontology.NodeRef
	for _, entry := range committed.RefLineage {
		require.NotEqual(t, entry.Original.Structural, entry.Preview.Structural)
		if entry.Original.Structural == second.Structural {
			mapped = entry.Preview
		}
	}
	require.NotEmpty(t, mapped.Structural)
	require.NotEqual(t, second.NodeID, mapped.NodeID)
	replay, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, request)
	require.NoError(t, err)
	require.Equal(t, committed.RefLineage, replay.RefLineage)
	_, err = srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{op(second, "Must not retarget.")}})
	require.ErrorContains(t, err, "structural fingerprint did not resolve")
	following, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{
		op(mapped, "Second edited again."), op(untouched, "Untouched sibling edited after moving."),
	}})
	require.NoError(t, err, "a moved unchanged sibling still resolves through its strong fingerprint")
	final, err := srv.commitOntologyEditSessionResponse(ctx, following.SessionID, OntologyEditSessionCommitRequest{})
	require.NoError(t, err)
	require.Equal(t, ontology.CommitOutcomeCommitted, final.Outcome)
	require.Empty(t, final.Warnings)
	bytes, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(bytes), "- Story tied to validation checks.\n  verification:: First criterion has a much longer summary.")
	require.Regexp(t, `- Follow \[\[plan\]\] while validating\.\n\s*verification:: Second edited again\.`, string(bytes))
	require.Regexp(t, `- Untouched sibling\.\n\s*verification:: Untouched sibling edited after moving\.`, string(bytes))
	require.False(t, strings.Contains(string(bytes), "Must not retarget."))
}
