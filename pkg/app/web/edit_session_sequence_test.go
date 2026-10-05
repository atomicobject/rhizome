package web

import (
	"context"
	"fmt"
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

// A note root is addressed by its path, so every later edit to the same note
// must replay on top of the staged state regardless of edit order.
func TestOntologyEditSessions_RepeatedEditsToOneNoteStageAndSave(t *testing.T) {
	notePath := "specs/100-demo/plan.md"
	summary := func(value string) OntologyEditOp {
		return OntologyEditOp{ID: "field:summary", Kind: "setField", Path: notePath, Field: "summary", Value: value}
	}
	relation := func(target string) OntologyEditOp {
		return OntologyEditOp{ID: "relation:spec", Kind: "setLinkField", Path: notePath, Field: "spec", Values: []string{target}}
	}
	// A row read through the staged overlay carries the preview fingerprint.
	stagedRead := func(op OntologyEditOp) OntologyEditOp {
		op.Structural = "fingerprint-from-a-staged-read"
		return op
	}
	for name, sequence := range map[string][]OntologyEditOp{
		"staged-read identity":            {relation("[[specs/100-demo/spec]]"), stagedRead(summary("Edited")), stagedRead(relation("[[spec]]"))},
		"field then relation":             {summary("A much longer edited summary"), relation("[[specs/100-demo/spec]]")},
		"relation then field":             {relation("[[specs/100-demo/spec]]"), summary("A much longer edited summary")},
		"same relation twice":             {relation("[[specs/100-demo/spec]]"), relation("[[spec]]")},
		"relation, field, relation again": {relation("[[specs/100-demo/spec]]"), summary("Short"), relation("[[spec]]")},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareOntologyFixtureVault(t)
			srv := newFixtureServer(t, fixture, nil)
			ctx := context.Background()
			created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{})
			require.NoError(t, err)
			revision := created.Revision
			for index, op := range sequence {
				staged, err := srv.stageOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionStageRequest{
					RequestID: fmt.Sprintf("stage-%d", index), ExpectedRevision: revision, Ops: []OntologyEditOp{op},
				})
				require.NoErrorf(t, err, "stage %d (%s)", index, op.Kind)
				require.Emptyf(t, staged.Conflicts, "stage %d", index)
				revision = staged.Revision
			}
			committed, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionCommitRequest{
				RequestID: "commit", ExpectedRevision: revision,
			})
			require.NoError(t, err)
			require.Equalf(t, ontology.CommitOutcomeCommitted, committed.Outcome, "conflicts: %#v", committed.Conflicts)
			saved, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(notePath)))
			require.NoError(t, err)
			final := map[string]any{}
			for _, op := range sequence {
				final[op.Field] = op.Value
				if op.Kind == "setLinkField" {
					final[op.Field] = op.Values[0]
				}
			}
			frontmatter, err := obsidian.ExtractFrontmatter(string(saved))
			require.NoError(t, err)
			for field, value := range final {
				require.Equalf(t, value, frontmatter[field], "field %s in saved note:\n%s", field, saved)
			}
		})
	}
}

// A conflict on one note must not take down staged reads: the conflicted note
// keeps showing what the author staged and every other staged note still reads.
func TestReadOverlayForEditSessionKeepsStagedReadsWhileConflicted(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	ctx := context.Background()
	planPath, tasksPath := "specs/100-demo/plan.md", "specs/100-demo/tasks.md"
	planFile := filepath.Join(fixture.root, filepath.FromSlash(planPath))
	original, err := os.ReadFile(planFile)
	require.NoError(t, err)
	document, err := ontology.BuildDocumentSnapshot(planPath, string(original), time.Time{})
	require.NoError(t, err)

	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{
		{
			ID: "field:plan-summary", Kind: "setField", Path: planPath, Field: "summary",
			FieldValue: &OntologyEditFieldValue{Kind: "scalar", Scalar: "Staged plan summary"},
			Expected: &OntologyEditExpected{
				Field:      &OntologyEditFieldValue{Kind: "scalar", Scalar: "Demo plan summary"},
				SourceHash: document.ContentFingerprint, SourceContent: string(original),
			},
		},
		{ID: "field:tasks-summary", Kind: "setField", Path: tasksPath, Field: "summary", Value: "Staged tasks summary"},
	}})
	require.NoError(t, err)
	defs, err := srv.ontologyDefinitions()
	require.NoError(t, err)
	// Warm the session overlay before the disk change so the conflicted read
	// below must refresh it rather than build it for the first time.
	warm, err := srv.readOverlayForEditSession(ctx, &EditSessionReadRequest{SessionID: created.SessionID})
	require.NoError(t, err)
	require.NotNil(t, warm)
	require.False(t, warm.Conflicted)
	warmPlan, err := srv.nodeReadScopeWithOverlay(ctx, defs, warm).Projection(ctx, ontology.NodeRef{NotePath: planPath, Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	require.Equal(t, []string{"Staged plan summary"}, warmPlan.Fields["summary"].Values)

	external := strings.Replace(string(original), "summary: Demo plan summary", "summary: External edit", 1)
	require.NoError(t, os.WriteFile(planFile, []byte(external), 0o644))

	overlay, err := srv.readOverlayForEditSession(ctx, &EditSessionReadRequest{SessionID: created.SessionID})
	require.NoError(t, err)
	require.NotNil(t, overlay)
	require.True(t, overlay.Conflicted)
	require.Equal(t, string(OntologyEditSessionStatusConflicted), overlay.Status)

	scope := srv.nodeReadScopeWithOverlay(ctx, defs, overlay)
	for path, want := range map[string]string{planPath: "Staged plan summary", tasksPath: "Staged tasks summary"} {
		projection, err := scope.Projection(ctx, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
		require.NoError(t, err)
		require.Equal(t, []string{want}, projection.Fields["summary"].Values, path)
	}

	// The conflict itself is still reported through the session.
	preview, err := srv.previewOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionPreviewRequest{})
	require.NoError(t, err)
	require.Equal(t, OntologyEditSessionStatusConflicted, preview.Status)
	require.Len(t, preview.Conflicts, 1)
	require.Equal(t, "field:plan-summary", preview.Conflicts[0].OperationID)
}

// After a save the runtime briefly holds the index lock (watcher batch,
// validation projection refresh). The next save waits instead of failing.
func TestOntologyEditSessions_SaveWaitsOutABriefIndexLockHolder(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	ctx := context.Background()
	notePath := "specs/100-demo/plan.md"
	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{
		{ID: "field:summary", Kind: "setField", Path: notePath, Field: "summary", Value: "Saved while locked"},
	}})
	require.NoError(t, err)
	release, acquired, err := indexlock.TryAcquireWithOptions(filepath.Join(fixture.root, ".rhizome", "index.lock"), indexlock.AcquireOptions{Role: "runtime/watcher-batch"})
	require.NoError(t, err)
	require.True(t, acquired)
	time.AfterFunc(300*time.Millisecond, func() { _ = release() })

	committed, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionCommitRequest{
		RequestID: "commit-locked", ExpectedRevision: created.Revision,
	})
	require.NoError(t, err)
	require.Equalf(t, ontology.CommitOutcomeCommitted, committed.Outcome, "conflicts: %#v", committed.Conflicts)
	saved, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(notePath)))
	require.NoError(t, err)
	require.Contains(t, string(saved), "summary: Saved while locked")
}
