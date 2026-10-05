package retrieval

import (
	"context"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestNestedRetrieversKeepMetadataPrecedenceAcrossCompletionOrder(t *testing.T) {
	handle := knowledge.NoteHandle("notes/guide.md")
	primary := search.Candidate{Handle: handle, Owner: handle, Type: "note", Path: "notes/guide.md", Title: "Authored title", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}
	fallback := search.Candidate{Handle: handle, Owner: handle, Type: "note", Path: "notes/guide.md", Title: "Filename title", NoteID: "guide", Evidence: []search.Evidence{{Type: "note_vector_similarity", RawScore: 0.8}}}
	for _, slow := range []string{"primary", "fallback"} {
		t.Run(slow+" finishes last", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			completed := make(chan context.Context, 1)
			first := completionOrderedRetriever{name: "primary", result: primary}
			second := completionOrderedRetriever{name: "fallback", result: fallback}
			if slow == "primary" {
				first.wait, second.publish = completed, completed
			} else {
				second.wait, first.publish = completed, completed
			}
			items, err := (&CompositeRetriever{Retrievers: []search.Retriever{first, second}}).Retrieve(ctx, search.QuerySpec{Intent: search.IntentSearch})
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.Equal(t, primary.Title, items[0].Title)
			require.Equal(t, fallback.NoteID, items[0].NoteID, "later lanes fill missing metadata")
			require.Len(t, items[0].Evidence, 2, "both lanes retain evidence")
		})
	}
}

// A lane cancels its stage context after merging its results. Waiting for that
// cancellation orders the merge itself, not merely the Retrieve return.
type completionOrderedRetriever struct {
	name    string
	result  search.Candidate
	wait    <-chan context.Context
	publish chan<- context.Context
}

func (r completionOrderedRetriever) Name() string { return r.name }
func (r completionOrderedRetriever) Retrieve(ctx context.Context, _ search.QuerySpec) ([]search.Candidate, error) {
	if r.publish != nil {
		r.publish <- ctx
	}
	if r.wait != nil {
		first := <-r.wait
		select {
		case <-first.Done():
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []search.Candidate{r.result}, nil
}
