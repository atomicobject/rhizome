package search

import (
	"context"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type laneTestRetriever struct {
	name string
	run  func(context.Context) ([]Candidate, error)
}

func (r laneTestRetriever) Name() string { return r.name }
func (r laneTestRetriever) Retrieve(ctx context.Context, _ QuerySpec) ([]Candidate, error) {
	return r.run(ctx)
}

func TestSearchStartsFilteredLanesTogether(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	var retrievers []Retriever
	for _, name := range []string{"vector", "note_lexical", "intel_lexical"} {
		retrievers = append(retrievers, laneTestRetriever{name: name, run: func(ctx context.Context) ([]Candidate, error) {
			started <- struct{}{}
			select {
			case <-release:
				return []Candidate{{SourceRef: SourceRef{Path: name}, Handle: knowledge.NoteHandle(name), Owner: knowledge.NoteHandle(name), Type: "note"}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}})
	}
	type outcome struct {
		response Response
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		response, err := (&Service{Retrievers: retrievers, Ranker: stubRanker{}}).Search(ctx, QuerySpec{Text: "query", Filters: Filters{Types: []string{"note"}}})
		done <- outcome{response: response, err: err}
	}()
	for range 3 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("all lanes must start before waiting for any lane to finish")
		}
	}
	close(release)
	result := <-done
	require.NoError(t, result.err)
	require.Len(t, result.response.Results, 3)
	require.Empty(t, result.response.Warnings)
}

func TestRunLanesCancellationWaitsForStartedRetrievers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 2)
	exited := make(chan struct{}, 2)
	var retrievers []Retriever
	for _, name := range []string{"definition", "intel_lexical"} {
		retrievers = append(retrievers, laneTestRetriever{name: name, run: func(ctx context.Context) ([]Candidate, error) {
			started <- struct{}{}
			<-ctx.Done()
			exited <- struct{}{}
			return nil, ctx.Err()
		}})
	}
	done := make(chan error, 1)
	go func() {
		_, err := RunLanes(ctx, QuerySpec{Intent: IntentGoToDef}, retrievers, "")
		done <- err
	}()
	for range 2 {
		<-started
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Len(t, exited, 2, "every started retriever must exit before the request returns")
}

func TestRunLanesNestedGroupsShareRetrievalDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var groupDeadline, leafDeadline time.Time
	leaf := laneTestRetriever{name: "vector", run: func(ctx context.Context) ([]Candidate, error) {
		leafDeadline, _ = ctx.Deadline()
		return []Candidate{{Handle: knowledge.NoteHandle("vector")}}, nil
	}}
	group := laneTestRetriever{name: "composite", run: func(ctx context.Context) ([]Candidate, error) {
		groupDeadline, _ = ctx.Deadline()
		return RunLanes(ctx, QuerySpec{Intent: IntentSearch}, []Retriever{leaf}, "composite")
	}}
	items, err := RunLanes(ctx, QuerySpec{Intent: IntentSearch}, []Retriever{group}, "")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.False(t, groupDeadline.IsZero())
	require.Equal(t, groupDeadline, leafDeadline, "nested groups must not subtract the ranking reserve again")
}
