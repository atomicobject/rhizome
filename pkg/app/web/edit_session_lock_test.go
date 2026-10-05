package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestOntologyEditSessions_SaveYieldsBackgroundWorker(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "unchanged receipt"
		if changed {
			name = "edited note"
		}
		t.Run(name, func(t *testing.T) {
			fixture := prepareOntologyFixtureVault(t)
			srv := newFixtureServer(t, fixture, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req := OntologyEditSessionCreateRequest{}
			if changed {
				req.Ops = []OntologyEditOp{{ID: "summary", Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Saved during background work"}}
			}
			created, err := srv.createOntologyEditSessionResponse(ctx, req)
			require.NoError(t, err)
			worker := lane.New(lane.Options{LockPath: filepath.Join(fixture.root, ".rhizome", "index.lock"), PriorityPoll: 10 * time.Millisecond})
			defer worker.Close()
			started := make(chan struct{})
			job, _, err := worker.Submit(ctx, lane.Request{Kind: lane.KindEmbedCycle, Run: func(ctx context.Context, _ lane.Reporter) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}})
			require.NoError(t, err)
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("background job did not start")
			}
			commit := OntologyEditSessionCommitRequest{RequestID: "save-with-worker", ExpectedRevision: created.Revision}
			saved, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, commit)
			require.NoError(t, err)
			require.Empty(t, saved.Warnings)
			require.Empty(t, saved.Conflicts)
			require.False(t, saved.HasUncommittedChanges)
			if changed {
				require.Equal(t, ontology.CommitOutcomeCommitted, saved.Outcome)
				content, err := os.ReadFile(filepath.Join(fixture.root, "specs/100-demo/plan.md"))
				require.NoError(t, err)
				require.Contains(t, string(content), "Saved during background work")
			} else {
				require.Equal(t, ontology.CommitOutcomeUnchanged, saved.Outcome)
			}
			select {
			case <-job.Done():
			case <-ctx.Done():
				t.Fatal("background job did not yield")
			}
			require.ErrorIs(t, job.Err(), context.Canceled)
			restarted := newFixtureServer(t, fixture, nil)
			retried, err := restarted.commitOntologyEditSessionResponse(ctx, created.SessionID, commit)
			require.NoError(t, err)
			require.Equal(t, saved.Outcome, retried.Outcome)
			require.Equal(t, saved.Revision, retried.Revision)
		})
	}
}
