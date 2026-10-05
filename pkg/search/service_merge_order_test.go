package search

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestSearchKeepsMetadataPrecedenceAcrossRetrieverCompletionOrder(t *testing.T) {
	// Two occupied slots let a third retriever witness completion of the first
	// merge: the service releases its semaphore only after recording the lane.
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	for _, primaryLast := range []bool{true, false} {
		t.Run(fmt.Sprintf("primary finishes last=%v", primaryLast), func(t *testing.T) {
			entered := make(chan int, 3)
			retrievers := make([]Retriever, 3)
			releases := make([]chan []Candidate, 3)
			for i := range retrievers {
				releases[i] = make(chan []Candidate, 1)
				retrievers[i] = mergeOrderedRetriever{index: i, entered: entered, release: releases[i]}
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
			// Whichever lanes the scheduler starts first, the earlier configured one
			// is authoritative. The remaining lane contributes no candidate.
			a, b := <-entered, <-entered
			primary, fallback := min(a, b), max(a, b)
			candidates := map[int][]Candidate{
				primary:  {{Handle: knowledge.NoteHandle("shared"), Title: "Authored title", Evidence: []Evidence{{Type: "note_title_match", Score: 1}}}},
				fallback: {{Handle: knowledge.NoteHandle("shared"), Title: "Filename title", NoteID: "shared", Evidence: []Evidence{{Type: "note_vector_similarity", Score: .8}}}},
			}
			first, last := primary, fallback
			if primaryLast {
				first, last = fallback, primary
			}
			releases[first] <- candidates[first]
			witness := <-entered // First lane has merged and released its slot.
			releases[witness] <- nil
			releases[last] <- candidates[last]
			result := <-done
			require.NoError(t, result.err)
			require.Len(t, result.response.Results, 1)
			candidate := result.response.Results[0]
			require.Equal(t, "Authored title", candidate.Title)
			require.Equal(t, "shared", candidate.NoteID)
			require.True(t, hasAnyEvidence(candidate.Evidence, "note_title_match"))
			require.True(t, hasAnyEvidence(candidate.Evidence, "note_vector_similarity"))
		})
	}
}

type mergeOrderedRetriever struct {
	index   int
	entered chan<- int
	release <-chan []Candidate
}

func (r mergeOrderedRetriever) Name() string                  { return fmt.Sprintf("lane%d", r.index) }
func (r mergeOrderedRetriever) CostClass() RetrieverCostClass { return RetrieverCostCPUHeavy }
func (r mergeOrderedRetriever) Retrieve(context.Context, QuerySpec) ([]Candidate, error) {
	r.entered <- r.index
	return <-r.release, nil
}
