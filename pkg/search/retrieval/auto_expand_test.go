package retrieval

import (
	"context"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

type delayedRetriever struct {
	name   string
	delay  time.Duration
	result []search.Candidate
}

func (d delayedRetriever) Name() string { return d.name }

func (d delayedRetriever) Retrieve(ctx context.Context, _ search.QuerySpec) ([]search.Candidate, error) {
	if d.delay > 0 {
		select {
		case <-time.After(d.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return d.result, nil
}

func TestRunRetrievers_StageBudgetsSlowBroadLane(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	items, err := runRetrievers(ctx, []search.Retriever{
		delayedRetriever{name: "vector", delay: time.Second},
		delayedRetriever{name: "intel_lexical", result: []search.Candidate{{
			Handle: knowledge.NoteHandle("notes/fast.md"),
			Owner:  knowledge.NoteHandle("notes/fast.md"),
			Type:   "note",
			Path:   "notes/fast.md",
		}}},
	}, search.QuerySpec{Text: "fast", Intent: search.IntentOverview, Limits: search.Limits{Total: 5}})

	if err != nil {
		t.Fatalf("expected slow broad lane to degrade, got %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("expected stage budget to return before parent deadline, got %v", ctx.Err())
	}
	if len(items) != 1 || items[0].Path != "notes/fast.md" {
		t.Fatalf("expected fast lane result, got %#v", items)
	}
}

func TestRunRetrieversPreservesScoreOrderAcrossCompletionOrder(t *testing.T) {
	weak := search.Candidate{Handle: knowledge.FileHandle("weak.go"), Owner: knowledge.FileHandle("weak.go"), Path: "weak.go", Evidence: []search.Evidence{{Type: "intel_fts_match", RawScore: 0.2}}}
	strong := search.Candidate{Handle: knowledge.FileHandle("strong.go"), Owner: knowledge.FileHandle("strong.go"), Path: "strong.go", Evidence: []search.Evidence{{Type: "intel_fts_match", RawScore: 0.9}}}
	orders := [][]search.Retriever{
		{delayedRetriever{name: "slow", delay: 5 * time.Millisecond, result: []search.Candidate{weak}}, delayedRetriever{name: "fast", result: []search.Candidate{strong}}},
		{delayedRetriever{name: "slow", delay: 5 * time.Millisecond, result: []search.Candidate{strong}}, delayedRetriever{name: "fast", result: []search.Candidate{weak}}},
	}
	for _, retrievers := range orders {
		items, err := runRetrievers(context.Background(), retrievers, search.QuerySpec{Text: "query", Intent: search.IntentSearch})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 || items[0].Path != "strong.go" || items[1].Path != "weak.go" {
			t.Fatalf("expected relevance order, got %#v", items)
		}
	}
}

func TestDeriveSeeds_PrefersCorroboratedCandidateScore(t *testing.T) {
	seeds := deriveSeeds([]search.Candidate{
		{
			Handle: knowledge.FileHandle("pkg/one.go"),
			Owner:  knowledge.FileHandle("pkg/one.go"),
			Evidence: []search.Evidence{
				{Type: "note_vector_similarity", RawScore: 0.42},
				{Type: "intel_fts_match", RawScore: 0.39},
			},
		},
		{
			Handle: knowledge.FileHandle("pkg/two.go"),
			Owner:  knowledge.FileHandle("pkg/two.go"),
			Evidence: []search.Evidence{
				{Type: "note_vector_similarity", RawScore: 0.55},
			},
		},
	}, 2, 2, map[search.EvidenceChannel]float64{
		search.EvidenceChannelSemantic: 1.0,
		search.EvidenceChannelLexical:  1.0,
	}, 0)

	if len(seeds) < 2 {
		t.Fatalf("expected two seeds, got %#v", seeds)
	}
	if seeds[0].String() != "file:pkg/one.go" {
		t.Fatalf("expected corroborated candidate to rank first, got %#v", seeds)
	}
}

func TestDeriveSeeds_RequiresSpecificityWhenConfigured(t *testing.T) {
	seeds := deriveSeeds([]search.Candidate{
		{
			Handle: knowledge.FileHandle("pkg/generic.go"),
			Owner:  knowledge.FileHandle("pkg/generic.go"),
			Evidence: []search.Evidence{
				{Type: "note_vector_similarity", RawScore: 0.95},
			},
		},
		{
			Handle: knowledge.FileHandle("pkg/search/semantic/note_syncer.go"),
			Owner:  knowledge.FileHandle("pkg/search/semantic/note_syncer.go"),
			Evidence: []search.Evidence{
				{Type: "note_vector_similarity", RawScore: 0.55},
				{Type: "query_specificity", RawScore: 0.7},
			},
		},
	}, 2, 2, map[search.EvidenceChannel]float64{
		search.EvidenceChannelSemantic:    1.0,
		search.EvidenceChannelSpecificity: 1.0,
	}, 0.25)

	if len(seeds) != 1 {
		t.Fatalf("expected one specific seed, got %#v", seeds)
	}
	if seeds[0].String() != "file:pkg/search/semantic/note_syncer.go" {
		t.Fatalf("expected specific seed, got %#v", seeds)
	}
}

func TestDeriveSeeds_AllowsStrongLexicalSeedWhenSpecificityMissing(t *testing.T) {
	seeds := deriveSeeds([]search.Candidate{{
		Handle: knowledge.FileHandle("docs/hubs/Embeddings (Hub).md"),
		Owner:  knowledge.FileHandle("docs/hubs/Embeddings (Hub).md"),
		Evidence: []search.Evidence{
			{Type: "intel_doc_match", RawScore: 0.75},
		},
	}}, 2, 2, map[search.EvidenceChannel]float64{
		search.EvidenceChannelLexical: 1.0,
	}, 0.25)

	if len(seeds) != 1 {
		t.Fatalf("expected strong lexical seed, got %#v", seeds)
	}
}
