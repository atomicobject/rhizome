package search

import (
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestSearchKeepsMetadataPrecedenceAcrossRetrieverCompletionOrder(t *testing.T) {
	for _, primaryLast := range []bool{true, false} {
		t.Run(fmt.Sprintf("primary finishes last=%v", primaryLast), func(t *testing.T) {
			entered := make(chan mergeLaneStart, 2)
			releases := []chan []Candidate{make(chan []Candidate, 1), make(chan []Candidate, 1)}
			retrievers := []Retriever{
				mergeOrderedRetriever{index: 0, entered: entered, release: releases[0]},
				mergeOrderedRetriever{index: 1, entered: entered, release: releases[1]},
			}
			type outcome struct {
				response Response
				err      error
			}
			done := make(chan outcome, 1)
			go func() {
				response, err := (&Service{Retrievers: retrievers, Ranker: stubRanker{}}).Search(context.Background(), QuerySpec{Text: "shared"})
				done <- outcome{response, err}
			}()
			contexts := make(map[int]context.Context)
			for range 2 {
				started := <-entered
				contexts[started.index] = started.ctx
			}
			candidates := [][]Candidate{
				{{SourceRef: SourceRef{Title: "Authored title"}, Handle: knowledge.NoteHandle("shared"), Evidence: []Evidence{{Type: "note_title_match", Score: 1}}}},
				{{SourceRef: SourceRef{Title: "Filename title", NoteID: "shared"}, Handle: knowledge.NoteHandle("shared"), Evidence: []Evidence{{Type: "note_vector_similarity", Score: .8}}}},
			}
			first, last := 0, 1
			if primaryLast {
				first, last = last, first
			}
			releases[first] <- candidates[first]
			<-contexts[first].Done()
			releases[last] <- candidates[last]
			result := <-done
			require.NoError(t, result.err)
			require.Len(t, result.response.Results, 1)
			candidate := result.response.Results[0]
			require.Equal(t, "Authored title", candidate.Title)
			require.Equal(t, "shared", candidate.NoteID)
			require.True(t, HasEvidence(candidate.Evidence, "note_title_match"))
			require.True(t, HasEvidence(candidate.Evidence, "note_vector_similarity"))
		})
	}
}

type mergeLaneStart struct {
	index int
	ctx   context.Context
}

type mergeOrderedRetriever struct {
	index   int
	entered chan<- mergeLaneStart
	release <-chan []Candidate
}

func (r mergeOrderedRetriever) Name() string { return fmt.Sprintf("lane%d", r.index) }
func (r mergeOrderedRetriever) Retrieve(ctx context.Context, _ QuerySpec) ([]Candidate, error) {
	r.entered <- mergeLaneStart{index: r.index, ctx: ctx}
	return <-r.release, nil
}
