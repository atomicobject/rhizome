package web

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/stretchr/testify/require"
)

func TestOntologySavePublishesFreshReadDomainsOnce(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	cacheSvc, err := cache.NewService(fixture.root, cache.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cacheSvc.Close() })
	require.NoError(t, cacheSvc.EnsureReady(t.Context()))
	srv.cfg.Cache = cacheSvc
	events, unsubscribe := srv.globalEvents.Subscribe()
	defer unsubscribe()
	ctx := context.Background()
	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{{
		Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Fresh saved summary",
	}}})
	require.NoError(t, err)
	request := OntologyEditSessionCommitRequest{RequestID: "fresh-save", ExpectedRevision: created.Revision}
	committed, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, request)
	require.NoError(t, err)
	require.Equal(t, ontology.CommitOutcomeCommitted, committed.Outcome)
	require.Empty(t, committed.Warnings)
	select {
	case event := <-events:
		require.Equal(t, GlobalEventNodeChanged, event.Kind)
		data := event.Data.(NodeChangedEventData)
		require.Equal(t, []string{"specs/100-demo/plan.md"}, data.Paths)
		require.ElementsMatch(t, []string{"metadata", "links", "markdown_targets", "ontology"}, data.Domains)
	default:
		t.Fatal("save did not publish freshness before returning")
	}
	result, err := srv.executeOntologyQuery(ctx, `{ node: note(path: "specs/100-demo/plan.md") { frontmatter } }`)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, "Fresh saved summary", result.Data["node"].(map[string]any)["frontmatter"].(map[string]any)["summary"])
	replay, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, request)
	require.NoError(t, err)
	require.Equal(t, committed.Outcome, replay.Outcome)
	require.Equal(t, committed.Revision, replay.Revision)
	select {
	case event := <-events:
		t.Fatalf("receipt replay published another event: %+v", event)
	default:
	}
}

func TestCommittedEditWithWarningDoesNotPublishFreshness(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	events, unsubscribe := srv.globalEvents.Subscribe()
	defer unsubscribe()
	lineage := []ontology.PreviewRefLineage{{
		Original: ontology.NodeRef{NotePath: "specs/100-demo/plan.md", Structural: "before"},
		Preview:  ontology.NodeRef{NotePath: "specs/100-demo/plan.md", Structural: "committed"},
	}}
	response := srv.refreshCommittedOntologyEdit(t.Context(), OntologyEditSessionResponse{
		Outcome:      ontology.CommitOutcomeCommitted,
		TouchedPaths: []string{"specs/100-demo/plan.md"},
		Warnings:     []string{"saved, but repair completion needs recovery"},
		RefLineage:   lineage,
	}, &validate.PostApplyRefreshResult{
		Paths: []string{"specs/100-demo/plan.md"}, Domains: []string{"metadata", "ontology"},
	})
	require.Len(t, response.Warnings, 1)
	require.Equal(t, lineage, response.RefLineage)
	require.Equal(t, lineage, compactOntologyCommitReceiptResponse(response).RefLineage)
	select {
	case event := <-events:
		t.Fatalf("committed warning published freshness: %+v", event)
	default:
	}
}
