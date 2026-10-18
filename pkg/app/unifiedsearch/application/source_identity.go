package application

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/search"
)

// DedupeCanonicalSources preserves the first ranked occurrence of each source
// and folds later evidence into it. Adapters therefore cannot expose multiple
// presentation rows for one canonical source identity.
func DedupeCanonicalSources(results []search.RankedResult) []search.RankedResult {
	out := make([]search.RankedResult, 0, len(results))
	indexes := make(map[string]int, len(results))
	for _, result := range results {
		key := CanonicalSourceIdentity(result)
		if index, ok := indexes[key]; ok {
			out[index].Candidate = search.MergeCandidate(out[index].Candidate, result.Candidate)
			continue
		}
		indexes[key] = len(out)
		out = append(out, result)
	}
	return out
}

// CanonicalizeSources coalesces note chunks by their owner and then removes
// duplicate canonical sources. It preserves the earliest rank while retaining
// the best note score and all deterministic evidence facts.
func CanonicalizeSources(results []search.RankedResult) []search.RankedResult {
	out := make([]search.RankedResult, 0, len(results))
	noteIndexes := make(map[string]int)
	for _, result := range results {
		if result.Type != "note" || result.Owner.String() == "" {
			out = append(out, result)
			continue
		}
		owner := result.Owner.String()
		if index, ok := noteIndexes[owner]; ok {
			bestScore := out[index].FinalScore
			out[index].Candidate = search.MergeCandidate(out[index].Candidate, result.Candidate)
			if result.FinalScore > bestScore {
				out[index].FinalScore = result.FinalScore
			}
			continue
		}
		noteIndexes[owner] = len(out)
		out = append(out, result)
	}
	return DedupeCanonicalSources(out)
}

func CanonicalSourceIdentity(result search.RankedResult) string {
	if result.NodeRef != nil && result.NodeRef.Kind != "" && result.NodeRef.Kind != "NOTE" {
		return strings.Join([]string{"node", result.NodeRef.NotePath, result.NodeRef.NodeID, result.NodeRef.Fragment, result.NodeRef.Structural}, "\x00")
	}
	if result.FQN != "" {
		return strings.Join([]string{"fqn", result.FQN, result.Path}, "\x00")
	}
	if result.Path != "" {
		return "path\x00" + result.Path
	}
	if handle := result.Handle.String(); handle != "" {
		return "handle\x00" + handle
	}
	return "title\x00" + result.Title
}

func resultTieKey(result search.RankedResult) string {
	return strings.Join([]string{CanonicalSourceIdentity(result), result.Symbol, result.Granularity, result.Handle.String()}, "\x00")
}

func resultIdentity(result search.RankedResult) string {
	if result.NodeRef != nil {
		return strings.Join([]string{"node", result.NodeRef.NotePath, result.NodeRef.NodeID, result.NodeRef.Fragment, result.NodeRef.Structural}, "\x00")
	}
	if result.FQN != "" {
		return strings.Join([]string{"fqn", result.FQN, result.Path}, "\x00")
	}
	if result.Path != "" {
		return "path\x00" + result.Path
	}
	if handle := result.Handle.String(); handle != "" {
		return "handle\x00" + handle
	}
	return ""
}

func itemIdentity(item answer.Item) string {
	if item.NodeRef != nil {
		return strings.Join([]string{"node", item.NodeRef.NotePath, item.NodeRef.NodeID, item.NodeRef.Fragment, item.NodeRef.StructuralFingerprint}, "\x00")
	}
	if item.FQN != "" {
		return strings.Join([]string{"fqn", item.FQN, item.Path}, "\x00")
	}
	return "path\x00" + item.Path
}
