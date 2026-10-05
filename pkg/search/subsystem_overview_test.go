package search

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestSubsystemOverviewShaper_PromotesLocalDocsAndCode(t *testing.T) {
	spec := QuerySpec{
		Intent:            IntentSubsystemOverview,
		ExplicitSeedPaths: []string{"pkg/app/mcp/semantic_query_unified.go"},
		HasExplicitSeeds:  true,
		Limits:            Limits{Total: 10},
	}
	results := []RankedResult{
		{Candidate: Candidate{Handle: knowledge.NoteHandle("README.md"), Type: "note", Path: "README.md", DocClass: DocClassRepoGlobal}, FinalScore: 0.99},
		{Candidate: Candidate{Handle: knowledge.NoteHandle("pkg/app/mcp/CONTEXT.md"), Type: "note", Path: "pkg/app/mcp/CONTEXT.md", DocClass: DocClassModule, PrimaryDoc: true}, FinalScore: 0.76},
		{Candidate: Candidate{Handle: knowledge.NoteHandle("docs/hubs/Search (Hub).md"), Type: "note", Path: "docs/hubs/Search (Hub).md", DocClass: DocClassHub}, FinalScore: 0.82},
		{Candidate: Candidate{Handle: knowledge.FileHandle("pkg/app/mcp/semantic_query_unified.go"), Type: "code", Path: "pkg/app/mcp/semantic_query_unified.go"}, FinalScore: 0.74},
	}

	shaped, err := (&SubsystemOverviewShaper{}).Shape(context.Background(), spec, results)
	require.NoError(t, err)
	require.Len(t, shaped, 4)
	require.Equal(t, "pkg/app/mcp/CONTEXT.md", shaped[0].Path)
	require.Equal(t, "pkg/app/mcp/semantic_query_unified.go", shaped[1].Path)
	require.Equal(t, "README.md", shaped[len(shaped)-1].Path)
}

func TestDetectWarnings_SubsystemOverviewFallback(t *testing.T) {
	spec := QuerySpec{
		Intent:            IntentSubsystemOverview,
		ExplicitSeedPaths: []string{"pkg/app/mcp/semantic_query_unified.go"},
		HasExplicitSeeds:  true,
	}
	results := []RankedResult{
		{Candidate: Candidate{Type: "note", Path: "README.md", DocClass: DocClassRepoGlobal}},
		{Candidate: Candidate{Type: "note", Path: "RHIZOME.md", DocClass: DocClassRepoGlobal}},
		{Candidate: Candidate{Type: "note", Path: ".codex/skills/foo/SKILL.md", DocClass: DocClassGenerated}},
	}
	warns := DetectWarnings(spec, results)
	require.Len(t, warns, 1)
	require.Equal(t, "subsystem_overview_fallback", warns[0].Code)
}

func TestDetectWarnings_SubsystemOverview_NoFallbackForLocalCodeOnly(t *testing.T) {
	spec := QuerySpec{
		Intent:            IntentSubsystemOverview,
		ExplicitSeedPaths: []string{"pkg/app/mcp/semantic_query_unified.go"},
		HasExplicitSeeds:  true,
	}
	results := []RankedResult{
		{Candidate: Candidate{Type: "code", Path: "pkg/app/mcp/semantic_query_unified.go"}},
		{Candidate: Candidate{Type: "code", Path: "pkg/app/mcp/semantic_query_scoring.go"}},
		{Candidate: Candidate{Type: "note", Path: "README.md", DocClass: DocClassRepoGlobal}},
		{Candidate: Candidate{Type: "note", Path: "RHIZOME.md", DocClass: DocClassRepoGlobal}},
		{Candidate: Candidate{Type: "note", Path: ".codex/skills/foo/SKILL.md", DocClass: DocClassGenerated}},
	}

	require.Empty(t, DetectWarnings(spec, results))
}
