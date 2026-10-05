package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOntologyEditSessions_RestoredCollectionOrderWitnessDetectsDrift(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	specPath := filepath.Join(fixture.root, "specs/100-demo/spec.md")
	specContent := `---
summary: Demo spec summary
---
# Demo Spec

## Stories

### Story A
status:: TODO
^story-a

### Story B
status:: TODO
^story-b
`
	require.NoError(t, os.WriteFile(specPath, []byte(specContent), 0o644))
	_, err := ontology.EnsureFreshRuntimeWithStore(
		context.Background(),
		testNoteMetadataIndexer(t),
		fixture.vaultDef,
		&obsidian.Note{},
		fixture.intelStore,
	)
	require.NoError(t, err)

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{{
			Kind:             "reorderCollection",
			Path:             "specs/100-demo/spec.md#Stories",
			Collection:       "stories",
			OrderedFragments: []string{"^story-b", "^story-a"},
		}},
	}, &created)
	require.Equal(t, OntologyEditSessionStatusDirty, created.Status)
	require.Contains(t, created.BaseFingerprints, "specs/100-demo/spec.md")
	require.Len(t, created.BaseDocuments, 1)

	deleteReq, err := http.NewRequest(http.MethodDelete, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID, nil)
	require.NoError(t, err)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	require.NoError(t, err)
	deleteResp.Body.Close()
	require.Equal(t, http.StatusOK, deleteResp.StatusCode)

	// The client snapshot still represents the original A,B collection. A
	// concurrent writer reorders the collection before the restored commit.
	externalContent := `---
summary: Demo spec summary
---
# Demo Spec

## Stories

### Story B
status:: TODO
^story-b

### Story A
status:: TODO
^story-a
`
	require.NoError(t, os.WriteFile(specPath, []byte(externalContent), 0o644))

	commitBody, err := json.Marshal(OntologyEditSessionCommitRequest{
		Snapshot: &OntologyEditSessionSnapshot{
			Version:          3,
			Revision:         created.Revision,
			SessionID:        created.SessionID,
			Ops:              created.Ops,
			BaseFingerprints: created.BaseFingerprints,
			BaseDocuments:    created.BaseDocuments,
		},
	})
	require.NoError(t, err)
	commitReq, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", bytes.NewReader(commitBody))
	require.NoError(t, err)
	commitReq.Header.Set("Content-Type", "application/json")
	commitResp, err := http.DefaultClient.Do(commitReq)
	require.NoError(t, err)
	defer commitResp.Body.Close()
	require.Equal(t, http.StatusConflict, commitResp.StatusCode)

	var conflicted OntologyEditSessionResponse
	require.NoError(t, json.NewDecoder(commitResp.Body).Decode(&conflicted))
	require.Equal(t, OntologyEditSessionStatusConflicted, conflicted.Status)
	require.Len(t, conflicted.Conflicts, 1)
	require.Equal(t, ontology.ConflictKindCollectionDrift, conflicted.Conflicts[0].Kind)

	data, err := os.ReadFile(specPath)
	require.NoError(t, err)
	require.Equal(t, externalContent, string(data))
}

func TestOntologyEditSessions_EmptyCommitIsSuccessfulNoOp(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{}, &created)
	require.Equal(t, OntologyEditSessionStatusClean, created.Status)
	require.False(t, created.HasUncommittedChanges)

	commitReq, err := http.NewRequest(
		http.MethodPost,
		httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit",
		bytes.NewReader([]byte(`{}`)),
	)
	require.NoError(t, err)
	commitReq.Header.Set("Content-Type", "application/json")
	commitResp, err := http.DefaultClient.Do(commitReq)
	require.NoError(t, err)
	defer commitResp.Body.Close()
	require.Equal(t, http.StatusOK, commitResp.StatusCode)

	var committed OntologyEditSessionResponse
	require.NoError(t, json.NewDecoder(commitResp.Body).Decode(&committed))
	require.Equal(t, OntologyEditSessionStatusClean, committed.Status)
	require.Equal(t, ontology.CommitOutcomeUnchanged, committed.Outcome)
	require.False(t, committed.HasUncommittedChanges)
	require.Empty(t, committed.Conflicts)
}

func TestOntologyEditSessions_StageAndCommitRequestsAreRevisionedAndIdempotent(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	ctx := context.Background()

	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(1), created.Revision)

	stage := OntologyEditSessionStageRequest{
		RequestID: "stage-1", ExpectedRevision: created.Revision,
		Ops: []OntologyEditOp{{ID: "summary", Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Updated once"}},
	}
	first, err := srv.stageOntologyEditSessionResponse(ctx, created.SessionID, stage)
	require.NoError(t, err)
	require.Equal(t, uint64(2), first.Revision)
	require.Len(t, first.Ops, 1)
	retry, err := srv.stageOntologyEditSessionResponse(ctx, created.SessionID, stage)
	require.NoError(t, err)
	require.Equal(t, first.Revision, retry.Revision)
	require.Equal(t, first.Ops, retry.Ops)

	_, err = srv.stageOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionStageRequest{
		RequestID: "stage-stale", ExpectedRevision: created.Revision,
		Ops: []OntologyEditOp{{ID: "title", Kind: "setField", Path: "specs/100-demo/plan.md", Field: "title", Value: "Stale"}},
	})
	require.ErrorContains(t, err, "revision changed")

	commitRequest := OntologyEditSessionCommitRequest{
		RequestID: "commit-1", ExpectedRevision: first.Revision,
	}
	committed, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, commitRequest)
	require.NoError(t, err)
	require.Equalf(t, ontology.CommitOutcomeCommitted, committed.Outcome, "conflicts: %#v", committed.Conflicts)
	require.Equal(t, uint64(3), committed.Revision)
	retriedCommit, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, commitRequest)
	require.NoError(t, err)
	require.Equal(t, committed.SessionID, retriedCommit.SessionID)
	require.Equal(t, committed.Revision, retriedCommit.Revision)
	require.Equal(t, committed.Outcome, retriedCommit.Outcome)
	require.Nil(t, retriedCommit.Plan)
	require.Empty(t, retriedCommit.BaseDocuments)
	receiptPath, err := srv.ontologyCommitReceiptPath(created.SessionID, commitRequest.RequestID)
	require.NoError(t, err)
	receiptBytes, err := os.ReadFile(receiptPath)
	require.NoError(t, err)
	require.NotContains(t, string(receiptBytes), "Updated once")
	require.Less(t, len(receiptBytes), 4096)

	restarted := newFixtureServer(t, fixture, nil)
	restartRequest := commitRequest
	restartRequest.Snapshot = &OntologyEditSessionSnapshot{
		Version: 3, Revision: first.Revision, SessionID: created.SessionID,
		Ops: first.Ops, BaseDocuments: first.BaseDocuments, BaseFingerprints: first.BaseFingerprints,
	}
	recoveredReceipt, err := restarted.commitOntologyEditSessionResponse(ctx, created.SessionID, restartRequest)
	require.NoError(t, err)
	require.Equal(t, committed.Outcome, recoveredReceipt.Outcome)
	require.Equal(t, committed.Revision, recoveredReceipt.Revision)
	reused := restartRequest
	reused.ExpectedRevision++
	_, err = restarted.commitOntologyEditSessionResponse(ctx, created.SessionID, reused)
	require.ErrorContains(t, err, "reused for a different submission")
	reused = restartRequest
	reused.Snapshot = &OntologyEditSessionSnapshot{
		Version: 3, Revision: first.Revision, SessionID: created.SessionID,
		Ops: cloneOntologyEditOps(first.Ops), BaseDocuments: append([]ontology.EditBaseDocument(nil), first.BaseDocuments...),
		BaseFingerprints: first.BaseFingerprints,
	}
	reused.Snapshot.BaseDocuments[0].Fingerprint = "different-base"
	_, err = restarted.commitOntologyEditSessionResponse(ctx, created.SessionID, reused)
	require.ErrorContains(t, err, "reused for a different submission")
}

func TestOntologyEditSessions_UnchangedRetryPersistsReceiptAfterCanceledWait(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	notePath := filepath.Join(fixture.root, "specs/100-demo/spec.md")
	before, err := os.ReadFile(notePath)
	require.NoError(t, err)
	created, err := srv.createOntologyEditSessionResponse(context.Background(), OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{{ID: "same-order", Kind: "reorderCollection", Path: "specs/100-demo/spec.md#Stories", Collection: "stories", OrderedFragments: []string{"^validation"}}},
	})
	require.NoError(t, err)
	require.Len(t, created.Ops, 1, "the unchanged commit must contain a staged operation")
	req := OntologyEditSessionCommitRequest{RequestID: "unchanged-retry", ExpectedRevision: created.Revision}
	lockPath := obsidian.IndexLockPath(fixture.root)
	releaseWriter, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer releaseWriter()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type commitResult struct {
		response OntologyEditSessionResponse
		err      error
	}
	result := make(chan commitResult, 1)
	go func() {
		response, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, req)
		result <- commitResult{response: response, err: err}
	}()
	waitDeadline := time.Now().Add(5 * time.Second)
	for {
		if indexlock.CheckPriority(lockPath) {
			break
		}
		select {
		case early := <-result:
			t.Fatalf("commit returned before receipt wait: outcome=%s conflicts=%v error=%v", early.response.Outcome, early.response.Conflicts, early.err)
		default:
		}
		if time.Now().After(waitDeadline) {
			t.Fatal("unchanged commit did not reach receipt wait")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	first := <-result
	require.NoError(t, first.err)
	require.Equal(t, ontology.CommitOutcomeUnchanged, first.response.Outcome)
	require.Equal(t, created.Revision+1, first.response.Revision)
	require.Contains(t, strings.Join(first.response.Warnings, "\n"), "could not persist the retry receipt")
	require.NoError(t, releaseWriter())
	mismatched := req
	mismatched.Snapshot = &OntologyEditSessionSnapshot{
		Version: 3, Revision: created.Revision, SessionID: created.SessionID,
		Ops:           []OntologyEditOp{{ID: "different-order", Kind: "reorderCollection", Path: "specs/100-demo/spec.md#Stories", Collection: "stories", OrderedFragments: []string{"^validation"}}},
		BaseDocuments: created.BaseDocuments,
	}
	_, err = srv.commitOntologyEditSessionResponse(context.Background(), created.SessionID, mismatched)
	require.ErrorContains(t, err, "reused for a different submission")

	retried, err := srv.commitOntologyEditSessionResponse(context.Background(), created.SessionID, req)
	require.NoError(t, err)
	require.Equal(t, first.response.Outcome, retried.Outcome)
	require.Equal(t, first.response.Revision, retried.Revision)
	after, err := os.ReadFile(notePath)
	require.NoError(t, err)
	require.Equal(t, before, after)
	receiptPath, err := srv.ontologyCommitReceiptPath(created.SessionID, req.RequestID)
	require.NoError(t, err)
	require.FileExists(t, receiptPath)
	restarted := newFixtureServer(t, fixture, nil)
	restartedResponse, err := restarted.commitOntologyEditSessionResponse(context.Background(), created.SessionID, req)
	require.NoError(t, err)
	require.Equal(t, retried.Revision, restartedResponse.Revision)
}

func TestOntologyEditSessions_FirstFieldInputUsesWorkspaceSourceWitness(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	notePath := "specs/100-demo/plan.md"
	absPath := filepath.Join(fixture.root, filepath.FromSlash(notePath))
	original, err := os.ReadFile(absPath)
	require.NoError(t, err)
	snapshot, err := ontology.BuildDocumentSnapshot(notePath, string(original), time.Time{})
	require.NoError(t, err)

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{{
			ID: "field:summary", Kind: "setField", Path: notePath, Field: "summary",
			FieldValue: &OntologyEditFieldValue{Kind: "scalar", Scalar: "My edit"},
			Expected: &OntologyEditExpected{
				Field:      &OntologyEditFieldValue{Kind: "scalar", Scalar: "Demo plan summary"},
				SourceHash: snapshot.ContentFingerprint, SourceContent: string(original),
			},
		}},
	}, &created)
	require.Len(t, created.BaseDocuments, 1)
	require.Equal(t, snapshot.ContentFingerprint, created.BaseDocuments[0].Fingerprint)

	external := strings.Replace(string(original), "summary: Demo plan summary", "summary: External edit", 1)
	require.NotEqual(t, string(original), external)
	require.NoError(t, os.WriteFile(absPath, []byte(external), 0o644))
	var diff ModifiedNotesResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/diff", map[string]any{}, &diff)
	require.Len(t, diff.Conflicts, 1)
	require.Equal(t, "field:summary", diff.Conflicts[0].OperationID)

	body, err := json.Marshal(OntologyEditSessionCommitRequest{})
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusConflict, response.StatusCode)
	var conflicted OntologyEditSessionResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&conflicted))
	require.Equal(t, ontology.CommitOutcomeConflicted, conflicted.Outcome)
	require.Equal(t, ontology.ConflictKindFieldChanged, conflicted.Conflicts[0].Kind)
	require.Equal(t, "field:summary", conflicted.Conflicts[0].OperationID)

	resolvedOps := cloneOntologyEditOps(created.Ops)
	resolvedOps[0].Expected.Field = &OntologyEditFieldValue{Kind: "scalar", Scalar: "External edit"}
	var resolved OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: resolvedOps, Replace: true,
	}, &resolved)
	require.True(t, resolved.HasUncommittedChanges)

	changedAgain := strings.Replace(external, "summary: External edit", "summary: Later edit", 1)
	require.NoError(t, os.WriteFile(absPath, []byte(changedAgain), 0o644))
	var changedAgainConflict OntologyEditSessionResponse
	postJSONWithStatus(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", OntologyEditSessionCommitRequest{}, http.StatusConflict, &changedAgainConflict)
	require.Equal(t, ontology.ConflictKindFieldChanged, changedAgainConflict.Conflicts[0].Kind)
	require.Equal(t, []string{"Later edit"}, changedAgainConflict.Conflicts[0].CurrentValues)

	resolvedOps[0].Expected.Field = &OntologyEditFieldValue{Kind: "scalar", Scalar: "Later edit"}
	resolvedOps[0].FieldValue = &OntologyEditFieldValue{Kind: "scalar", Scalar: "Demo plan summary"}
	resolvedOps[0].Value = "Demo plan summary"
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{
		Ops: resolvedOps, Replace: true,
	}, &resolved)

	var committed OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", OntologyEditSessionCommitRequest{}, &committed)
	require.Equal(t, ontology.CommitOutcomeCommitted, committed.Outcome)
	updated, err := os.ReadFile(absPath)
	require.NoError(t, err)
	require.Contains(t, string(updated), "summary: Demo plan summary")
}

func TestOntologyEditSessions_RestoredRelationIsRevalidatedAgainstCurrentTargets(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	ctx := context.Background()
	notePath := "specs/100-demo/plan.md"
	source, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(notePath)))
	require.NoError(t, err)
	document, err := ontology.BuildDocumentSnapshot(notePath, string(source), time.Time{})
	require.NoError(t, err)
	op := OntologyEditOp{
		ID: "relation:spec", Kind: "setLinkField", Path: notePath, Field: "spec",
		FieldValue: &OntologyEditFieldValue{Kind: "list", Items: []string{"specs/100-demo/research"}},
		Expected: &OntologyEditExpected{
			Field:      &OntologyEditFieldValue{Kind: "list", Items: []string{"specs/100-demo/spec.md"}},
			SourceHash: document.ContentFingerprint, SourceContent: string(source),
		},
	}
	snapshot := &OntologyEditSessionSnapshot{
		Version: 3, Revision: 1, SessionID: "restored-relation", Ops: []OntologyEditOp{op},
		BaseDocuments: []ontology.EditBaseDocument{{
			NotePath: notePath, Fingerprint: document.ContentFingerprint, Content: string(source),
		}},
	}

	response, err := srv.previewOntologyEditSessionResponse(ctx, snapshot.SessionID, OntologyEditSessionPreviewRequest{Snapshot: snapshot})
	require.NoError(t, err)
	require.Equal(t, OntologyEditSessionStatusConflicted, response.Status)
	require.Len(t, response.Conflicts, 1)
	require.Equal(t, ontology.ConflictKindUnsupportedTarget, response.Conflicts[0].Kind)
	require.Equal(t, op.ID, response.Conflicts[0].OperationID)
}

func TestPublicOntologyEditSessionPreservesExplicitFieldValueKinds(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	schemaPath := filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql")
	schemaData, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	schemaData = []byte(strings.Replace(string(schemaData),
		"  summary: String!\n  spec: Spec! @link(inverse: \"plans\")",
		"  summary: String!\n  emptyString: String\n  unsetString: String\n  tags: [String!]\n  aliases: [String!]\n  enabled: Boolean\n  count: Int\n  spec: Spec! @link(inverse: \"plans\")", 1))
	require.NoError(t, os.WriteFile(schemaPath, schemaData, 0o644))

	planPath := filepath.Join(fixture.root, "specs", "100-demo", "plan.md")
	planData, err := os.ReadFile(planPath)
	require.NoError(t, err)
	planData = []byte(strings.Replace(string(planData),
		"summary: Demo plan summary\nspec: specs/100-demo/spec.md",
		"summary: Demo plan summary\nempty-string: Original\nunset-string: Remove me\ntags: [old-tag]\naliases: [old-alias]\nenabled: true\ncount: 7\nspec: specs/100-demo/spec.md", 1))
	require.NoError(t, os.WriteFile(planPath, planData, 0o644))
	_, err = ontology.EnsureFreshRuntimeWithStore(
		context.Background(),
		testNoteMetadataIndexer(t),
		fixture.vaultDef,
		&obsidian.Note{},
		fixture.intelStore,
	)
	require.NoError(t, err)

	srv := newFixtureServer(t, fixture, nil)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	var created OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions", OntologyEditSessionCreateRequest{}, &created)
	require.Equal(t, OntologyEditSessionStatusClean, created.Status)

	ops := []OntologyEditOp{
		{
			Kind: "setField", Path: "specs/100-demo/plan.md", Field: "emptyString",
			FieldValue: &OntologyEditFieldValue{Kind: "scalar", Scalar: ""},
		},
		{
			Kind: "setField", Path: "specs/100-demo/plan.md", Field: "unsetString",
			FieldValue: &OntologyEditFieldValue{Kind: "unset"},
		},
		{
			Kind: "setField", Path: "specs/100-demo/plan.md", Field: "tags",
			FieldValue: &OntologyEditFieldValue{Kind: "list", Items: []string{}},
		},
		{
			Kind: "setField", Path: "specs/100-demo/plan.md", Field: "aliases",
			FieldValue: &OntologyEditFieldValue{Kind: "list", Items: []string{"new-alias", "second-alias"}},
		},
		{
			Kind: "setField", Path: "specs/100-demo/plan.md", Field: "enabled",
			FieldValue: &OntologyEditFieldValue{Kind: "scalar", Scalar: "false"},
		},
		{
			Kind: "setField", Path: "specs/100-demo/plan.md", Field: "count",
			FieldValue: &OntologyEditFieldValue{Kind: "scalar", Scalar: "0"},
		},
	}

	var staged OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/stage", OntologyEditSessionStageRequest{Ops: ops}, &staged)
	require.Equal(t, OntologyEditSessionStatusDirty, staged.Status)
	require.Len(t, staged.Ops, len(ops))
	byField := make(map[string]OntologyEditFieldValue, len(staged.Ops))
	for _, op := range staged.Ops {
		require.NotNil(t, op.FieldValue)
		byField[op.Field] = *op.FieldValue
	}
	require.Equal(t, OntologyEditFieldValue{Kind: "scalar", Scalar: ""}, byField["emptyString"])
	require.Equal(t, OntologyEditFieldValue{Kind: "unset"}, byField["unsetString"])
	require.Equal(t, "list", byField["tags"].Kind)
	require.Empty(t, byField["tags"].Items)
	require.Equal(t, OntologyEditFieldValue{Kind: "list", Items: []string{"new-alias", "second-alias"}}, byField["aliases"])
	require.Equal(t, OntologyEditFieldValue{Kind: "scalar", Scalar: "false"}, byField["enabled"])
	require.Equal(t, OntologyEditFieldValue{Kind: "scalar", Scalar: "0"}, byField["count"])

	var committed OntologyEditSessionResponse
	postJSON(t, httpSrv.URL+"/api/v1/edit-sessions/"+created.SessionID+"/commit", OntologyEditSessionCommitRequest{}, &committed)
	require.Equal(t, OntologyEditSessionStatusClean, committed.Status)
	require.Equal(t, ontology.CommitOutcomeCommitted, committed.Outcome)

	updated, err := os.ReadFile(planPath)
	require.NoError(t, err)
	content := string(updated)
	require.Contains(t, content, "empty-string: \"\"")
	require.NotContains(t, content, "unset-string:")
	require.Contains(t, content, "tags: []")
	require.Contains(t, content, "aliases: [new-alias, second-alias]")
	require.Contains(t, content, "enabled: false")
	require.Contains(t, content, "count: 0")
}

func postJSONWithStatus(t *testing.T, url string, body any, wantStatus int, out any) {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, wantStatus, response.StatusCode)
	require.NoError(t, json.NewDecoder(response.Body).Decode(out))
}
