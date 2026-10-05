package web

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	mcpapi "github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestMapSearchResponseOnlyPublishesSourceBackedSnippets(t *testing.T) {
	resp := mapSearchResponse(agentapi.SemanticQueryResponse{
		Matches: []mcpapi.SemanticMatchPayload{
			{
				Type:    "note",
				Path:    "docs/spec.md",
				Title:   "Search contract",
				Heading: "Search contract",
				Preview: "Search contract",
			},
			{
				Type:        "code",
				Path:        "pkg/search/service.go",
				Title:       "Service.Search",
				Preview:     "func (s *Service) Search(ctx context.Context, spec QuerySpec)",
				ContentKind: "excerpt",
				StartLine:   81,
			},
			{
				Type:        "note",
				Path:        "docs/duplicate.md",
				Title:       "Duplicate",
				Preview:     "variant of docs/spec.md; read if needed",
				ContentKind: "stub",
			},
		},
	})

	require.Len(t, resp.Matches, 3)
	require.Equal(t, "unavailable", resp.Matches[0].SnippetStatus)
	require.Empty(t, resp.Matches[0].Snippet)
	require.Equal(t, "available", resp.Matches[1].SnippetStatus)
	require.Contains(t, resp.Matches[1].Snippet, "Service")
	require.Equal(t, 81, resp.Matches[1].StartLine)
	require.Equal(t, "unavailable", resp.Matches[2].SnippetStatus)
	require.Empty(t, resp.Matches[2].Snippet)
}

func TestMapSearchResponsePreservesCanonicalNoteTargets(t *testing.T) {
	ref := &ontology.NodeRef{NotePath: "docs/spec.md", Fragment: "requirements-1234", Kind: "structural"}
	resp := mapSearchResponse(agentapi.SemanticQueryResponse{
		Matches: []mcpapi.SemanticMatchPayload{{
			Type:     "note",
			Path:     "docs/spec.md",
			NoteType: "TechnicalSpec",
			NodeRef:  ref,
		}},
	})

	require.Len(t, resp.Matches, 1)
	require.Equal(t, ref, resp.Matches[0].NodeRef)
	require.Equal(t, "TechnicalSpec", resp.Matches[0].NoteType)
}
