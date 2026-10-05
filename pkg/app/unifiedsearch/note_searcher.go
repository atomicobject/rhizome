package unifiedsearch

import (
	"context"
	"strings"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/search"
)

// NoteSearcher adapts the shared application search to the GraphQL search root.
type NoteSearcher struct {
	Runtime Options
	// runRuntime is the package's existing retrieval seam, forwarded so tests
	// can exercise the adapter without a real index.
	runRuntime func(context.Context, Options) (Result, error)
}

func (s NoteSearcher) SearchNotes(ctx context.Context, request ontologyquery.NoteSearchRequest) (ontologyquery.NoteSearchResponse, error) {
	opts := s.Runtime
	opts.QueryInputs = nil
	for _, term := range request.Queries {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		opts.QueryInputs = append(opts.QueryInputs, QueryInput{Text: term})
	}
	opts.UseVector = true
	opts.UseIntel = true
	opts.UseRefs = true
	// Graph expansion needs seeds, which the search root never supplies.
	opts.UseGraph = false
	opts.Filters.Types = []string{"note"}
	opts.Filters.NoteTypes = request.NoteTypes

	policy, err := ResolveEffectivePolicy(ProfileInteractive, search.IntentSearch, 0, 0, 0)
	if err != nil {
		return ontologyquery.NoteSearchResponse{}, err
	}
	visibleLimit := policy.VisibleLimit
	if request.First > 0 {
		visibleLimit = request.First
	}

	result, err := Execute(ctx, ApplicationOptions{
		Runtime:         opts,
		Profile:         ProfileInteractive,
		VisibleLimit:    visibleLimit,
		CandidateWindow: visibleLimit,
		runRuntime:      s.runRuntime,
	})
	if err != nil {
		return ontologyquery.NoteSearchResponse{}, err
	}

	response := ontologyquery.NoteSearchResponse{}
	byPath := make(map[string]int, len(result.Sources))
	for _, source := range result.Sources {
		path := strings.TrimSpace(source.Result.Path)
		if source.Result.NodeRef != nil && strings.TrimSpace(source.Result.NodeRef.NotePath) != "" {
			path = strings.TrimSpace(source.Result.NodeRef.NotePath)
		}
		if path == "" {
			continue
		}
		if index, ok := byPath[path]; ok {
			if source.Result.FinalScore > response.Hits[index].Score {
				response.Hits[index].Score = source.Result.FinalScore
			}
			continue
		}
		byPath[path] = len(response.Hits)
		response.Hits = append(response.Hits, ontologyquery.NoteSearchHit{Path: path, Score: source.Result.FinalScore})
	}

	seenWarning := make(map[string]struct{}, len(result.Warnings))
	for _, warning := range result.Warnings {
		key := warning.Code + "\x00" + warning.Message
		if _, ok := seenWarning[key]; ok {
			continue
		}
		seenWarning[key] = struct{}{}
		response.Warnings = append(response.Warnings, ontologyquery.RuntimeWarning{Code: warning.Code, Message: warning.Message})
	}
	return response, nil
}
