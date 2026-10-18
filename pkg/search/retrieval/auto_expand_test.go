package retrieval

import (
	"context"
	"fmt"
	"reflect"
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

func TestRunRetrieversGivesVectorLaneTimeAlongsideFiveFastLanes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	retrievers := []search.Retriever{delayedRetriever{name: "vector", delay: 1200 * time.Millisecond, result: []search.Candidate{{SourceRef: search.SourceRef{Path: "notes/vector.md"}, Handle: knowledge.NoteHandle("notes/vector.md"), Owner: knowledge.NoteHandle("notes/vector.md"), Type: "note"}}}}
	for _, path := range []string{"one", "two", "three", "four", "five"} {
		retrievers = append(retrievers, delayedRetriever{name: path, result: []search.Candidate{{SourceRef: search.SourceRef{Path: path}, Handle: knowledge.NoteHandle(path), Owner: knowledge.NoteHandle(path), Type: "note"}}})
	}
	items, err := runRetrievers(ctx, retrievers, search.QuerySpec{Text: "query", Intent: search.IntentOverview, Limits: search.Limits{Total: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("expected all six lanes including the first vector lane, got %#v", items)
	}
}

func TestRunRetrieversPreservesScoreOrderAcrossCompletionOrder(t *testing.T) {
	weak := search.Candidate{SourceRef: search.SourceRef{Path: "weak.go"}, Handle: knowledge.FileHandle("weak.go"), Owner: knowledge.FileHandle("weak.go"), Evidence: []search.Evidence{{Type: "intel_fts_match", RawScore: 0.2}}}
	strong := search.Candidate{SourceRef: search.SourceRef{Path: "strong.go"}, Handle: knowledge.FileHandle("strong.go"), Owner: knowledge.FileHandle("strong.go"), Evidence: []search.Evidence{{Type: "intel_fts_match", RawScore: 0.9}}}
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

func TestDeriveSeedsTieBreakIsDeterministic(t *testing.T) {
	evidence := []search.Evidence{
		{Type: "note_vector_similarity", RawScore: 0.62},
		{Type: "intel_fts_match", RawScore: 0.41},
		{Type: "doc_link", RawScore: 0.27},
		{Type: "graph_proximity", RawScore: 0.13},
		{Type: "query_specificity", RawScore: 0.67},
		{Type: "ontology_relation_structural", RawScore: 0.37},
	}
	var candidates []search.Candidate
	for i := 39; i >= 0; i-- {
		handle := knowledge.FileHandle(fmt.Sprintf("pkg/%02d.go", i))
		candidates = append(candidates, search.Candidate{Handle: handle, Owner: handle, Evidence: evidence})
	}
	want := []knowledge.Handle{
		knowledge.FileHandle("pkg/00.go"), knowledge.FileHandle("pkg/01.go"),
		knowledge.FileHandle("pkg/02.go"), knowledge.FileHandle("pkg/03.go"),
		knowledge.FileHandle("pkg/04.go"), knowledge.FileHandle("pkg/05.go"),
		knowledge.FileHandle("pkg/06.go"), knowledge.FileHandle("pkg/07.go"),
	}
	weights := map[search.EvidenceChannel]float64{
		search.EvidenceChannelSemantic:           0.7,
		search.EvidenceChannelLexical:            0.3,
		search.EvidenceChannelRefs:               1.2,
		search.EvidenceChannelGraph:              0.45,
		search.EvidenceChannelSpecificity:        0.9,
		search.EvidenceChannelOntologyStructural: 0.65,
	}
	for run := range 50 {
		got := deriveSeeds(candidates, 8, 8, weights, 0)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("equal-score seeds must use handle order on run %d: want %v, got %v", run, want, got)
		}
	}
}
