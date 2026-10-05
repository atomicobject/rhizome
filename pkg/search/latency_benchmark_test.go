package search

import (
	"context"
	"fmt"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

type benchmarkTargetResolverStore struct {
	files []string
}

func (s *benchmarkTargetResolverStore) IntelAnchorsBySymbol(context.Context, string, int) ([]codeanchor.IntelAnchor, error) {
	return nil, nil
}

func (s *benchmarkTargetResolverStore) IntelAnchorsByFQNsLimited(context.Context, []string, int) (map[string][]codeanchor.IntelAnchor, error) {
	return map[string][]codeanchor.IntelAnchor{}, nil
}

func (s *benchmarkTargetResolverStore) IntelNotePaths(context.Context) ([]string, error) {
	return nil, nil
}

func (s *benchmarkTargetResolverStore) ListFiles(context.Context, int) ([]string, error) {
	return s.files, nil
}

func BenchmarkResolveQuerySpecTargets_IndexedPathInference(b *testing.B) {
	files := make([]string, 0, 12000)
	for i := 0; i < 4000; i++ {
		files = append(files,
			fmt.Sprintf("pkg/search/component_%04d/service.go", i),
			fmt.Sprintf("pkg/cache/component_%04d/service.go", i),
			fmt.Sprintf("pkg/runtime/component_%04d/service.go", i),
		)
	}
	files = append(files, "pkg/search/service.go")
	store := &benchmarkTargetResolverStore{files: files}
	spec := QuerySpec{
		Text:   "subsystem overview for pkg/search/service.go",
		Intent: IntentSubsystemOverview,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, warnings := ResolveQuerySpecTargets(ctx, "", store, spec)
		if len(warnings) != 0 {
			b.Fatalf("unexpected warnings: %#v", warnings)
		}
		if out.TargetStatus != TargetStatusInferredPath {
			b.Fatalf("unexpected target status %q", out.TargetStatus)
		}
	}
}

func BenchmarkApproxRankCandidates_Normalized(b *testing.B) {
	weights := map[EvidenceChannel]float64{
		EvidenceChannelSemantic: 1.0,
		EvidenceChannelLexical:  0.8,
		EvidenceChannelRefs:     1.1,
	}
	spec := QuerySpec{Intent: IntentDocsForCode, Limits: Limits{Total: 25}}
	candidates := make([]Candidate, 0, 1000)
	for i := 0; i < 1000; i++ {
		candidates = append(candidates, Candidate{
			Handle: knowledge.NoteHandle(fmt.Sprintf("notes/%04d.md", i)),
			Owner:  knowledge.NoteHandle(fmt.Sprintf("owner/%03d", i/2)),
			Path:   fmt.Sprintf("notes/%04d.md", i),
			Evidence: []Evidence{
				{Type: "note_vector_similarity", RawScore: 0.45 + float64(i%7)/20},
				{Type: "intel_fts_match", RawScore: 0.35 + float64(i%5)/20},
				{Type: "doc_link", RawScore: 0.25 + float64(i%3)/10},
			},
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := approxRankCandidates(spec, candidates, weights, 2)
		if len(results) == 0 {
			b.Fatal("expected ranked results")
		}
	}
}
