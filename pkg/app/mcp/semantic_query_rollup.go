package mcp

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
)

func normalizeSemanticTypes(types []string) []string {
	if len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	seen := map[string]struct{}{}
	for _, t := range types {
		normalized := strings.ToLower(strings.TrimSpace(t))
		switch normalized {
		case "note", "notes":
			normalized = "note"
		case "code":
			normalized = "code"
		default:
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func matchFromRanked(r search.RankedResult) SemanticMatchPayload {
	switch r.Type {
	case "note", "doc_section":
		notePath := strings.TrimSpace(firstNonEmpty(r.NoteID, r.Path))
		notePath = string(paths.NormalizeNotePath(notePath))
		title := firstNonEmpty(r.Title, titleFromPath(notePath))
		chunkIndex := -1
		if r.Type == "doc_section" {
			chunkIndex = r.ChunkIndex
		}
		return SemanticMatchPayload{
			Type:          "note",
			Path:          notePath,
			Title:         title,
			Score:         r.FinalScore,
			Breadcrumb:    r.Breadcrumb,
			Heading:       firstNonEmpty(r.Heading, r.Title),
			Granularity:   r.Granularity,
			ChunkIndex:    chunkIndex,
			Specificity:   queryframe.SpecificityScore(r.Evidence),
			NodeRef:       r.NodeRef,
			NodeID:        r.NodeID,
			NodeRefJSON:   r.NodeRefJSON,
			SourceLocator: r.SourceLocator,
			NodeKind:      r.NodeKind,
			NodeType:      r.NodeType,
			ParentNodeID:  r.ParentNodeID,
		}
	case "code", "anchor", "file":
		codePath := strings.TrimSpace(r.Path)
		codePath = string(paths.NormalizeCode(filepath.ToSlash(codePath)))
		label := firstNonEmpty(r.Symbol, r.FQN, r.Title, filepath.Base(codePath))
		return SemanticMatchPayload{
			Type:          "code",
			Path:          codePath,
			Title:         label,
			Symbol:        firstNonEmpty(r.Symbol, r.Title),
			FQN:           r.FQN,
			Kind:          r.Kind,
			Granularity:   r.Granularity,
			ChunkIndex:    -1,
			Specificity:   queryframe.SpecificityScore(r.Evidence),
			Score:         r.FinalScore,
			Breadcrumb:    r.Breadcrumb,
			Heading:       r.Heading,
			AnchorID:      r.AnchorID,
			SourceLocator: r.SourceLocator,
			NodeRef:       r.NodeRef,
		}
	default:
		path := strings.TrimSpace(r.Path)
		label := firstNonEmpty(r.Title, r.Symbol, r.FQN, path)
		return SemanticMatchPayload{
			Type:        r.Type,
			Path:        path,
			Title:       label,
			Score:       r.FinalScore,
			Breadcrumb:  r.Breadcrumb,
			Heading:     r.Heading,
			ChunkIndex:  r.ChunkIndex,
			Specificity: queryframe.SpecificityScore(r.Evidence),
			NodeRef:     r.NodeRef,
		}
	}
}
