package answer

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildOverviewPacket(t *testing.T) {
	resp := Build(search.IntentSubsystemOverview, "search subsystem overview", search.TargetStatusExplicitPath, nil, []Input{
		{Type: "note", Path: "pkg/search/CONTEXT.md", Role: "doc", Score: 0.9, DirectSpecificity: 0.8},
		{Type: "code", Path: "pkg/search/service.go", Role: "entry_point", Score: 0.88, DirectSpecificity: 0.8},
		{Type: "code", Path: "pkg/search/ranker.go", Role: "impl", Score: 0.84, DirectSpecificity: 0.8},
		{Type: "code", Path: "pkg/search/service_test.go", Role: "test", Score: 0.8, DirectSpecificity: 0.8},
	})

	require.Len(t, resp.MustRead, 4)
	assert.Equal(t, "high", resp.Confidence.Level)
	assert.True(t, resp.Coverage.Docs)
	assert.True(t, resp.Coverage.Code)
	assert.True(t, resp.Coverage.Tests)
	assert.Empty(t, resp.Coverage.Missing)
}

func TestBuildPreservesNodeRef(t *testing.T) {
	resp := Build(search.IntentSearch, "story context", search.TargetStatusNone, nil, []Input{
		{
			Type:    "note",
			Path:    "docs/spec.md",
			Title:   "Spec",
			Score:   0.9,
			NodeRef: &NodeRef{NotePath: "docs/spec.md", Kind: "NOTE", TypeName: "TechnicalSpec", NodeID: "story-a"},
		},
	})

	require.NotEmpty(t, resp.MustRead)
	require.Equal(t, "docs/spec.md", resp.MustRead[0].NodeRef.NotePath)
	require.Equal(t, "NOTE", resp.MustRead[0].NodeRef.Kind)
	require.Equal(t, "TechnicalSpec", resp.MustRead[0].NodeRef.TypeName)
	require.Equal(t, "story-a", resp.MustRead[0].NodeRef.NodeID)
}

func TestRenderTextIncludesOntologyNodeWikilink(t *testing.T) {
	resp := Build(search.IntentSearch, "story context", search.TargetStatusNone, nil, []Input{
		{
			Type:       "note",
			Path:       "docs/spec.md",
			Title:      "Story A",
			Score:      0.9,
			LinkTarget: &LinkTarget{Wikilink: "[[spec#^story-a]]", Exists: true},
		},
	})

	require.Contains(t, RenderText(resp), "link: [[spec#^story-a]]")
}

func TestRenderTextIncludesCodeLineAndSymbol(t *testing.T) {
	for _, tc := range []struct {
		name         string
		input        Input
		want, absent string
	}{
		{"path and symbol", Input{Type: "code", Path: "pkg/search/service.go", Symbol: "Service.Search", Line: 61, Score: 0.9}, "pkg/search/service.go:61 Service.Search", ""},
		{"symbol without path", Input{Type: "code", Symbol: "FooBar", Line: 42, Score: 0.9}, "FooBar:42", "FooBar:42 FooBar"},
		{"path and FQN", Input{Type: "code", Path: "pkg/search/service.go", FQN: "github.com/atomicobject/rhizome/pkg/search.Service.Search", Line: 61, Score: 0.9}, "pkg/search/service.go:61 github.com/atomicobject/rhizome/pkg/search.Service.Search", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := RenderText(Build(search.IntentSearch, "how does search run?", search.TargetStatusNone, nil, []Input{tc.input}))
			require.Contains(t, text, tc.want)
			if tc.absent != "" {
				require.NotContains(t, text, tc.absent)
			}
		})
	}
}

func TestBuildPrimaryNodeDocumentationCoverage(t *testing.T) {
	resp := Build(search.IntentSearch, "how does the story connect?", search.TargetStatusNone, nil, []Input{
		{
			Type:              "note",
			Path:              "docs/specs/product/parent-spec.md",
			Title:             "Parent Spec",
			Role:              "doc",
			Score:             0.7,
			Granularity:       "node_body",
			DirectSpecificity: 0.8,
			NodeRef:           &NodeRef{NotePath: "docs/specs/product/parent-spec.md", TypeName: "TechnicalSpec", Kind: "NOTE"},
		},
		{Type: "code", Path: "pkg/story/service.go", Role: "impl", Score: 0.75, DirectSpecificity: 0.8},
		{Type: "code", Path: "pkg/story/entry.go", Role: "entry_point", Score: 0.72, DirectSpecificity: 0.8},
		{Type: "note", Path: "docs/hubs/Story (Hub).md", Role: "doc", Score: 0.7, DirectSpecificity: 0.8},
	})

	require.NotEmpty(t, resp.MustRead)
	contextItem := findItemByPath(resp.MustRead, "docs/specs/product/parent-spec.md")
	require.NotNil(t, contextItem)
	require.Equal(t, "documentation", contextItem.Role)
	require.Equal(t, "documentation tied to the task", contextItem.Why)
	require.True(t, resp.Coverage.Docs)
	require.NotContains(t, resp.Coverage.Missing, "docs")
	require.Equal(t, "high", resp.Confidence.Level)
	withoutRef := Build(search.IntentSearch, "how is the story handled?", search.TargetStatusNone, nil, []Input{{
		Type: "note", Path: "docs/specs/product/parent-spec.md", Role: "doc", Score: 0.6, Granularity: "node_body", DirectSpecificity: 0.8,
	}})
	require.True(t, withoutRef.Coverage.Docs)
	require.NotContains(t, withoutRef.Coverage.Missing, "docs")
}

func TestBuildKeepsEffortNodeSupporting(t *testing.T) {
	resp := Build(search.IntentSearch, "current effort", search.TargetStatusNone, nil, []Input{{
		Type:        "note",
		Path:        "docs/efforts/current.md",
		Title:       "Current Effort",
		Score:       0.7,
		Granularity: "node_body",
		NodeRef:     &NodeRef{NotePath: "docs/efforts/current.md", TypeName: "EffortNote", Kind: "NOTE"},
	}})
	item := findItemByPath(resp.MustRead, "docs/efforts/current.md")
	if item == nil {
		item = findItemByPath(resp.Supporting, "docs/efforts/current.md")
	}
	require.NotNil(t, item)
	require.Equal(t, "supporting", item.Role)
	require.Equal(t, "supporting evidence", item.Why)
	normal := Build(search.IntentSearch, "checkout story", search.TargetStatusNone, nil, []Input{{Type: "note", Path: "docs/spec.md", Title: "Faster checkout", Score: 0.9, Granularity: "node_body"}})
	require.NotEmpty(t, normal.MustRead)
	require.Equal(t, "supporting evidence", normal.MustRead[0].Why)
}

func findItemByPath(items []Item, path string) *Item {
	for i := range items {
		if items[i].Path == path {
			return &items[i]
		}
	}
	return nil
}

func TestBuildPrecisionPacketLowersConfidenceWhenCoverageMissing(t *testing.T) {
	resp := Build(search.IntentGoToDef, "definition for Service.Search", search.TargetStatusAmbiguous, nil, []Input{
		{Type: "code", Path: "pkg/search/service.go", Role: "impl", Score: 0.9},
	})

	assert.Equal(t, "low", resp.Confidence.Level)
	assert.NotContains(t, resp.Coverage.Missing, "tests")
	exact := Build(search.IntentGoToDef, "definition for Service.Search", search.TargetStatusExplicitPath, nil, []Input{
		{Type: "code", Path: "pkg/search/service.go", Role: "impl", Score: 0.9},
	})

	assert.NotEqual(t, "low", exact.Confidence.Level)
	assert.NotContains(t, exact.Coverage.Missing, "tests")
}

func TestBuildDefaultSearchPacketSkipsWeakEntrypoint(t *testing.T) {
	resp := Build(search.IntentSearch, "ontology", search.TargetStatusNone, nil, []Input{
		{Type: "note", Path: "docs/hubs/Ontology (Hub).md", Role: "doc", Score: 0.8, Specificity: 0.9},
		{Type: "code", Path: "pkg/ontology/query/execute.go", Role: "impl", Score: 0.7, Specificity: 0.9},
		{Type: "code", Path: "pkg/generic/store.go", Role: "entry_point", Score: 0.99, Specificity: 0.9, DirectSpecificity: 0.4},
	})
	rendered := RenderText(resp)
	require.Contains(t, rendered, "pkg/ontology/query/execute.go")
	require.Contains(t, rendered, "docs/hubs/Ontology (Hub).md")
	require.NotContains(t, rendered, "pkg/generic/store.go")
}

func TestRenderTextIncludesConfidenceAndNextQueries(t *testing.T) {
	resp := Build(search.IntentSubsystemOverview, "search subsystem overview", search.TargetStatusExplicitPath, nil, []Input{
		{Type: "code", Path: "pkg/search/service.go", Role: "impl", Score: 0.9},
	})

	text := RenderText(resp)
	assert.Contains(t, text, "Confidence:")
	assert.Contains(t, text, "Next queries:")
	assert.Contains(t, text, string(search.IntentDocsForCode))
}

func TestBuildExplanationAndTestCoverageByIntent(t *testing.T) {
	inputs := []Input{
		{Type: "note", Path: "docs/hubs/Search (Hub).md", Role: "doc", Score: 0.8, Specificity: 0.9},
		{Type: "code", Path: "pkg/search/service.go", Role: "impl", Score: 0.7, Specificity: 0.9},
	}
	for _, tc := range []struct {
		name         string
		intent       search.Intent
		query        string
		missingTests bool
	}{
		{"explain", search.IntentSearch, "how are notes embedded?", false},
		{"default search", search.IntentSearch, "ontology", false},
		{"requested regression", search.IntentSearch, "ontology regression coverage", true},
		{"tests for code intent", search.IntentTestsForCode, "ontology", true},
		{"explain symbol", search.IntentExplainSymbol, "explain this function", false},
		{"explain symbol regression", search.IntentExplainSymbol, "explain this function regression coverage", true},
		{"explain overview", search.IntentOverview, "how does search work?", false},
		{"explain overview regression", search.IntentOverview, "how does search regression coverage work?", true},
		{"explain refactor", search.IntentRefactorImpact, "why change search ranking?", false},
		{"explain refactor coverage", search.IntentRefactorImpact, "why change search ranking test coverage?", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := Build(tc.intent, tc.query, search.TargetStatusNone, nil, inputs)
			rendered := RenderText(resp)
			if tc.missingTests {
				require.Contains(t, resp.Coverage.Missing, "tests")
				require.Contains(t, rendered, "Missing: tests")
			} else {
				require.NotContains(t, resp.Coverage.Missing, "tests")
				require.NotContains(t, rendered, "Missing: tests")
				if tc.name == "explain" || tc.name == "default search" {
					require.Equal(t, "high", resp.Confidence.Level)
				}
			}
			require.True(t, resp.Coverage.Docs)
			require.True(t, resp.Coverage.Code)
		})
	}
}

func TestBuildExplainPacketFillsDocAndImplementationSlotsBySpecificity(t *testing.T) {
	resp := Build(search.IntentSearch, "how are notes embedded?", search.TargetStatusNone, nil, []Input{
		{Type: "note", Path: "docs/generic.md", Role: "doc", Score: 0.99, Specificity: 0.1},
		{Type: "note", Path: "docs/hubs/Embeddings (Hub).md", Role: "doc", Score: 0.7, Specificity: 0.8},
		{Type: "code", Path: "pkg/search/semantic/note_syncer.go", Role: "impl", Score: 0.6, Specificity: 0.9},
	})

	require.Len(t, resp.MustRead, 3)
	assert.Equal(t, "docs/hubs/Embeddings (Hub).md", resp.MustRead[0].Path)
	assert.Equal(t, "pkg/search/semantic/note_syncer.go", resp.MustRead[1].Path)
	require.True(t, resp.Coverage.Code)
	require.Contains(t, RenderText(resp), "pkg/search/semantic/note_syncer.go")
}

func TestBuildExplainPacketUsesDirectSpecificityWhenEvidenceSpecificityMissing(t *testing.T) {
	resp := Build(search.IntentSearch, "how does Service.Search run?", search.TargetStatusNone, nil, []Input{
		{Type: "note", Path: "docs/search.md", Role: "doc", Score: 0.7, DirectSpecificity: 0.8},
		{Type: "code", Path: "pkg/search/generic.go", Role: "impl", Symbol: "Generic", Score: 0.99, DirectSpecificity: 0.1},
		{Type: "code", Path: "pkg/search/service.go", Role: "impl", FQN: "pkg/search.Service.Search", Score: 0.6, DirectSpecificity: 0.9},
	})

	require.NotEmpty(t, resp.MustRead)
	assert.Equal(t, "pkg/search/service.go", resp.MustRead[0].Path)
	assert.Equal(t, 0.9, resp.MustRead[0].Specificity)
}

func TestBuildTreatsCodeAnchorsAsImplementation(t *testing.T) {
	resp := Build(search.IntentSearch, "how are notes embedded?", search.TargetStatusNone, nil, []Input{
		{Type: "note", Path: "docs/hubs/Embeddings (Hub).md", Role: "doc", Score: 0.8},
		{Type: "anchor", Path: "pkg/search/semantic/embedder.go", Symbol: "QueryEmbeddings", Score: 0.9, Specificity: 0.7},
	})

	require.Len(t, resp.MustRead, 2)
	assert.Equal(t, "implementation", resp.MustRead[1].Role)
	assert.True(t, resp.Coverage.Code)
}

func TestBuildExplainPacketKeepsWeakDecisionOutOfMustRead(t *testing.T) {
	resp := Build(search.IntentSearch, "how do we query ontology nodes?", search.TargetStatusNone, nil, []Input{
		{Type: "note", Path: "docs/hubs/Ontology (Hub).md", Role: "doc", Score: 0.8, DirectSpecificity: 0.9},
		{Type: "code", Path: "pkg/ontology/query/execute.go", Role: "impl", Score: 0.7, DirectSpecificity: 0.9},
		{Type: "note", Path: "docs/rhizome-md-templates/Ontology query guide.md", Role: "doc", Score: 0.7, DirectSpecificity: 0.9},
		{Type: "note", Path: "docs/reference/decisions/Use SSE first for ontology node subscriptions.md", Score: 0.95, DirectSpecificity: 0.55},
	})

	for _, item := range resp.MustRead {
		require.NotEqual(t, "docs/reference/decisions/Use SSE first for ontology node subscriptions.md", item.Path)
	}
	require.NotEmpty(t, resp.Supporting)
	assert.Equal(t, "docs/reference/decisions/Use SSE first for ontology node subscriptions.md", resp.Supporting[0].Path)
}

func TestAssessedMustReadPrefersScoreOverTokenOverlapAmongUsefulSources(t *testing.T) {
	inputs := func(overlapSpecificity float64) []Input {
		return []Input{
			{Type: "note", Path: "operations/order-corrections.md", Title: "Order Corrections", Role: "doc", Score: 1.22, Specificity: 0.69, DirectSpecificity: 0.3, Relevance: "useful", Eligibility: "supporting"},
			{Type: "note", Path: "operations/volunteer-shifts.md", Title: "Volunteer Shift Changes", Role: "doc", Score: 0.9, Specificity: 0.54, DirectSpecificity: overlapSpecificity, Relevance: "useful", Eligibility: "supporting"},
		}
	}
	query := "which order changes can a member make after packing starts"

	overlap := Build(search.IntentSearch, query, search.TargetStatusNone, nil, inputs(0.4))
	require.NotEmpty(t, overlap.MustRead)
	require.Equal(t, "operations/order-corrections.md", overlap.MustRead[0].Path, "one shared title token must not outrank the engine score")

	decisive := Build(search.IntentSearch, query, search.TargetStatusNone, nil, inputs(0.8))
	require.NotEmpty(t, decisive.MustRead)
	require.Equal(t, "operations/volunteer-shifts.md", decisive.MustRead[0].Path, "a real identity match still wins")
}

func TestAssessedMustReadLeadsWithIdentityMatch(t *testing.T) {
	inputs := []Input{
		{Type: "note", Path: "docs/specs/experience/ontology-browser-navigation-model.md", Title: "Ontology browser navigation model", Role: "doc", Score: 2.4, Relevance: "strong", IdentityMatch: true, SupportedRoles: []string{"documentation"}},
		{Type: "code", Path: "pkg/ontology/node_catalog.go", Role: "impl", Score: 2.6, Relevance: "strong", SupportedRoles: []string{"implementation"}},
		{Type: "note", Path: "docs/specs/product/ontology-browser-workspace.md", Role: "doc", Score: 1.1, Relevance: "useful", SupportedRoles: []string{"documentation"}},
	}
	query := "Ontology browser navigation model"

	packet := Build(search.IntentSearch, query, search.TargetStatusNone, nil, inputs)
	require.NotEmpty(t, packet.MustRead)
	require.Equal(t, "docs/specs/experience/ontology-browser-navigation-model.md", packet.MustRead[0].Path, "the exact-identity hit leads must-read")

	// An intent with required roles is where role coverage used to take the lead.
	roleCovered := Build(search.IntentSubsystemOverview, query, search.TargetStatusNone, nil, inputs)
	require.NotEmpty(t, roleCovered.MustRead)
	require.Equal(t, "docs/specs/experience/ontology-browser-navigation-model.md", roleCovered.MustRead[0].Path, "identity leads before role coverage")
	paths := make([]string, 0, len(roleCovered.MustRead))
	for _, item := range roleCovered.MustRead {
		paths = append(paths, item.Path)
	}
	require.Contains(t, paths, "pkg/ontology/node_catalog.go", "role coverage still fills the packet")
}

func TestAssessedMustReadIdentityLeaderSkipsPrecisionIntents(t *testing.T) {
	inputs := []Input{
		{Type: "note", Path: "docs/specs/experience/ontology-browser-navigation-model.md", Title: "Ontology browser navigation model", Role: "doc", Score: 2.4, Relevance: "strong", IdentityMatch: true, SupportedRoles: []string{"documentation"}},
		{Type: "code", Path: "pkg/ontology/node_catalog.go", Role: "impl", Score: 2.6, Relevance: "strong", Eligibility: "primary", Relationship: "definition", SupportedRoles: []string{"implementation"}},
		{Type: "note", Path: "docs/specs/product/ontology-browser-workspace.md", Role: "doc", Score: 1.1, Relevance: "useful", SupportedRoles: []string{"documentation"}},
	}

	packet := Build(search.IntentGoToDef, "Ontology browser navigation model", search.TargetStatusNone, nil, inputs)

	require.NotEmpty(t, packet.MustRead)
	require.Equal(t, "pkg/ontology/node_catalog.go", packet.MustRead[0].Path, "precision selection is unchanged")
}
