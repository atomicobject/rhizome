package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnrichSemanticLinkTargetsAddsEmbeddedNodeMetadata(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Story implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type Stories implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "one.md"), []byte(`---
type: Spec
summary: One
---

# One

## Stories

### Linkable
status:: ready
^linkable
`), 0o644))

	matches := []SemanticMatchPayload{{
		Type: "note",
		Path: "specs/one.md",
		NodeRef: &ontology.NodeRef{
			NotePath: "specs/one.md",
			Fragment: "^linkable",
			Kind:     ontology.NodeKindEmbedded,
		},
	}, {
		Type:        "note",
		Path:        "specs/one.md",
		NodeRefJSON: `{"notePath":"specs/one.md","fragment":"^linkable","kind":"EMBEDDED"}`,
	}}
	enrichSemanticLinkTargets(context.Background(), Config{
		VaultPath: root,
		VaultDef:  obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
	}, nil, matches)
	require.NotNil(t, matches[0].LinkTarget)
	assert.Equal(t, "[[one#^linkable]]", matches[0].LinkTarget.Wikilink)
	assert.True(t, matches[0].LinkTarget.Exists)
	assert.Equal(t, "[[one#^linkable]]", matches[0].ReferenceHint)
	require.NotNil(t, matches[1].LinkTarget)
	assert.Equal(t, "[[one#^linkable]]", matches[1].LinkTarget.Wikilink)
	assert.Equal(t, "[[one#^linkable]]", matches[1].ReferenceHint)
	assert.Contains(t, renderSemanticPiece(0, matches[0], 0.9, "body"), "link: [[one#^linkable]]")
}

func TestAttachSymbolHitsAddsLinesAndBestSymbol(t *testing.T) {
	page := []semanticQueryGroup{{
		match: SemanticMatchPayload{Type: "code", Path: "pkg/search/service.go", Title: "service.go", Symbol: "service.go"},
		hits: []search.RankedResult{{
			Candidate:  search.Candidate{AnchorID: "a1"},
			FinalScore: 0.91,
		}},
	}}
	anchors := map[string]codeanchor.IntelAnchor{
		"a1": {
			AnchorID:  "a1",
			Kind:      "method",
			Path:      "pkg/search/service.go",
			Symbol:    "Service.Search",
			FQN:       "github.com/atomicobject/rhizome/pkg/search.Service.Search",
			Signature: "func (s *Service) Search(ctx context.Context, spec QuerySpec) (Response, error)",
			StartLine: 61,
			EndLine:   121,
		},
	}

	attachSymbolHits(page, anchors)

	match := page[0].match
	require.Equal(t, "Service.Search", match.Title)
	require.Equal(t, "Service.Search", match.Symbol)
	require.Equal(t, 61, match.StartLine)
	require.Equal(t, 121, match.EndLine)
	require.Len(t, match.Symbols, 1)
	require.Equal(t, "method", match.Symbols[0].Kind)
	require.Equal(t, 0.91, match.Symbols[0].Score)
	require.Contains(t, semanticHeader(0, match, 0.91), "pkg/search/service.go:61")
}

func TestAttachModuleOutlinesAddsExportsForModuleHits(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	module := codeanchor.IntelAnchor{
		AnchorID:    "module-a",
		Lang:        codeanchor.LangGo,
		Kind:        "module",
		Path:        "pkg/search/service.go",
		Symbol:      "service.go",
		Fingerprint: "module-fp",
	}
	searchFn := codeanchor.IntelAnchor{
		AnchorID:    "fn-a",
		Lang:        codeanchor.LangGo,
		Kind:        "method",
		Path:        "pkg/search/service.go",
		Symbol:      "Service.Search",
		FQN:         "github.com/atomicobject/rhizome/pkg/search.Service.Search",
		Signature:   "func (s *Service) Search(ctx context.Context, spec QuerySpec) (Response, error)",
		StartLine:   61,
		EndLine:     121,
		Fingerprint: "fn-fp",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, module.Path, []codeanchor.IntelAnchor{module, searchFn}, nil, nil))

	page := []semanticQueryGroup{{
		match: SemanticMatchPayload{Type: "code", Path: module.Path, Title: "service.go", Symbol: "service.go"},
		hits: []search.RankedResult{{
			Candidate: search.Candidate{AnchorID: module.AnchorID},
		}},
	}}
	attachModuleOutlines(ctx, store, page, map[string]codeanchor.IntelAnchor{module.AnchorID: module})

	require.Len(t, page[0].match.Symbols, 1)
	require.Equal(t, "Service.Search", page[0].match.Symbols[0].Symbol)
	require.Equal(t, 61, page[0].match.StartLine)
	require.Equal(t, 121, page[0].match.EndLine)
}

func TestBuildSemanticOptions_PrefersFullWhenCheap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.go")
	require.NoError(t, os.WriteFile(path, []byte("package main\n\nfunc Foo() {}\n"), 0o644))

	group := semanticQueryGroup{
		match: SemanticMatchPayload{
			Type: "code",
			Path: filepath.Base(path),
		},
		score: 0.9,
	}
	opts := buildSemanticOptions(group, map[string]codeanchor.IntelAnchor{}, dir, 0, 3, 8000, semanticQueryIntent{}, docMatcher{})
	require.NotEmpty(t, opts)
	assert.Equal(t, planFull, opts[0].plan)
}

func TestBuildSemanticOptions_OffersOutlineForCodeAnchors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.go")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("func Noise() {}\n", 200)), 0o644))

	group := semanticQueryGroup{
		match: SemanticMatchPayload{Type: "code", Path: filepath.Base(path)},
		score: 0.9,
		hits: []search.RankedResult{
			{Candidate: search.Candidate{AnchorID: "a1"}},
			{Candidate: search.Candidate{AnchorID: "a2"}},
		},
	}
	anchors := map[string]codeanchor.IntelAnchor{
		"a1": {AnchorID: "a1", Signature: "func Search(ctx context.Context, q string) []Result", StartLine: 10},
		"a2": {AnchorID: "a2", Signature: "type Searcher struct", StartLine: 30},
	}

	opts := buildSemanticOptions(group, anchors, dir, 0, 3, 1200, semanticQueryIntent{}, docMatcher{})
	plans := make([]contentPlan, 0, len(opts))
	for _, opt := range opts {
		plans = append(plans, opt.plan)
	}
	assert.Contains(t, plans, planOutline)
	assert.Less(t, indexOfPlan(plans, planOutline), indexOfPlan(plans, planStub))
}

func indexOfPlan(plans []contentPlan, want contentPlan) int {
	for i, plan := range plans {
		if plan == want {
			return i
		}
	}
	return -1
}

func TestRenderSemanticBody_CodeExcerptUsesAnchorSpan(t *testing.T) {
	dir := t.TempDir()
	code := "package main\n\nfunc Alpha() {}\n\nfunc Beta() {}\n"
	full := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(full, []byte(code), 0o644))

	anchor := codeanchor.IntelAnchor{
		AnchorID:  "a1",
		Path:      "main.go",
		StartByte: int64(strings.Index(code, "func Alpha")),
		EndByte:   int64(strings.Index(code, "func Alpha")) + int64(len("func Alpha() {}")),
	}
	group := semanticQueryGroup{
		match: SemanticMatchPayload{
			Type: "code",
			Path: "main.go",
		},
		hits: []search.RankedResult{
			{Candidate: search.Candidate{AnchorID: "a1"}},
		},
	}
	body, _, _ := renderSemanticBody(context.Background(), Config{VaultPath: dir}, nil, group, map[string]codeanchor.IntelAnchor{"a1": anchor}, planExcerpt, 2000)
	require.Equal(t, "func Alpha() {}", body)
	require.NotContains(t, body, "Beta")
}

func TestRenderSemanticBody_CodeOutlineUsesTopAnchors(t *testing.T) {
	group := semanticQueryGroup{
		match: SemanticMatchPayload{Type: "code", Path: "main.go"},
		hits: []search.RankedResult{
			{Candidate: search.Candidate{AnchorID: "a1"}},
			{Candidate: search.Candidate{AnchorID: "a2"}},
			{Candidate: search.Candidate{AnchorID: "a3"}},
		},
	}
	anchors := map[string]codeanchor.IntelAnchor{
		"a1": {
			AnchorID:   "a1",
			Path:       "main.go",
			StartLine:  10,
			Signature:  "func Search(ctx context.Context, q string) []Result",
			DocComment: "// Search runs the query pipeline.",
		},
		"a2": {
			AnchorID:  "a2",
			Path:      "main.go",
			StartLine: 30,
			Signature: "type Searcher struct",
		},
		"a3": {
			AnchorID:  "a3",
			Path:      "main.go",
			StartLine: 50,
			Signature: "package main",
		},
	}

	body, _, _ := renderSemanticBody(context.Background(), Config{}, nil, group, anchors, planOutline, 2000)

	assert.Contains(t, body, "L10 func Search(ctx context.Context, q string) []Result")
	assert.Contains(t, body, "Search runs the query pipeline.")
	assert.Contains(t, body, "L30 type Searcher struct")
	assert.NotContains(t, body, "package main")
}

func TestBuildSemanticQueryIntent_SubsystemOverviewPrefersDocs(t *testing.T) {
	intent := buildSemanticQueryIntent(search.IntentSubsystemOverview)
	assert.True(t, intent.Overview)
	assert.True(t, intent.PreferDocs)
	assert.False(t, intent.PreferCode)
}

func TestMarshalSemanticQueryResponseTrimsAnswerPreviewsBeforeText(t *testing.T) {
	longPreview := strings.Repeat("preview ", 80)
	longText := strings.Repeat("body ", 220)
	resp := semanticQueryResponse{
		Query: "q",
		Count: 1,
		Text:  longText,
		MustRead: []answer.Item{
			{Type: "code", Path: "pkg/a.go", Role: "implementation", Preview: longPreview},
		},
		Supporting: []answer.Item{
			{Type: "note", Path: "Notes/A.md", Role: "documentation", Preview: longPreview},
		},
	}

	const budget = 1900
	untrimmed, err := json.Marshal(resp)
	require.NoError(t, err)
	require.Greater(t, len(untrimmed), budget, "fixture must exceed the budget before preview trimming")

	encoded, err := marshalSemanticQueryResponse(resp, budget)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), budget)

	var out semanticQueryResponse
	require.NoError(t, json.Unmarshal(encoded, &out))
	require.Equal(t, longText, out.Text, "trimming previews must suffice before Text is shortened")
	require.Len(t, out.MustRead, 1)
	require.Len(t, out.Supporting, 1)
	require.NotEmpty(t, out.MustRead[0].Preview)
	require.LessOrEqual(t, len(out.MustRead[0].Preview), 180)
	require.NotEmpty(t, out.Supporting[0].Preview)
	require.LessOrEqual(t, len(out.Supporting[0].Preview), 180)
}

func TestMarshalSemanticQueryResponseDropsFieldsUntilUnderBudget(t *testing.T) {
	longPreview := strings.Repeat("preview ", 120)
	longText := strings.Repeat("body ", 260)
	resp := semanticQueryResponse{
		Query:      "q",
		Count:      2,
		Text:       longText,
		Warnings:   []search.Warning{{Code: "warning", Message: strings.Repeat("warning ", 20)}},
		TypeCounts: map[string]int{"code": 2},
		MustRead: []answer.Item{
			{Type: "code", Path: "pkg/a.go", Role: "implementation", Preview: longPreview},
		},
		Supporting: []answer.Item{
			{Type: "note", Path: "Notes/A.md", Role: "documentation", Preview: longPreview},
		},
		Matches: []SemanticMatchPayload{
			{Type: "code", Path: "pkg/a.go", Preview: longPreview},
			{Type: "note", Path: "Notes/A.md", Preview: longPreview},
		},
	}

	encoded, err := marshalSemanticQueryResponse(resp, 360)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), 360)
}

func TestMarshalSemanticQueryResponsePreservesSkeletalMatchesOverProse(t *testing.T) {
	resp := semanticQueryResponse{
		Query:       "how does search run?",
		ModeApplied: string(search.IntentOverview),
		Count:       1,
		Text:        strings.Repeat("packed prose ", 120),
		Summary:     strings.Repeat("summary ", 40),
		MustRead: []answer.Item{{
			Type:    "code",
			Path:    "pkg/search/service.go",
			Role:    "implementation",
			Symbol:  "Service.Search",
			Line:    61,
			Preview: strings.Repeat("preview ", 80),
		}},
		Supporting: []answer.Item{{
			Type:    "note",
			Path:    "docs/hubs/Search (Hub).md",
			Role:    "overview",
			Preview: strings.Repeat("preview ", 80),
		}},
		Warnings: []search.Warning{{
			Code:    "retriever_degraded",
			Message: strings.Repeat("warning ", 20),
		}},
		Matches: []SemanticMatchPayload{{
			Type:           "code",
			Path:           "pkg/search/service.go",
			Title:          "Service.Search",
			Symbol:         "Service.Search",
			FQN:            "github.com/atomicobject/rhizome/pkg/search.Service.Search",
			StartLine:      61,
			EndLine:        121,
			Specificity:    0.9,
			Score:          0.8,
			Preview:        strings.Repeat("preview ", 80),
			Included:       true,
			Kind:           "method",
			AnchorID:       "a1",
			ExcerptContent: "large excerpt should be removed",
			Symbols:        []SymbolHit{{Symbol: "Service.Search", StartLine: 61, EndLine: 121, Signature: "func (s *Service) Search(...)"}},
			LinkTarget:     &ontology.NodeLinkTarget{Wikilink: "[[service]]"},
		}},
	}
	decode := func(t *testing.T, budget int) SemanticMatchPayload {
		t.Helper()
		encoded, err := marshalSemanticQueryResponse(resp, budget)
		require.NoError(t, err)
		require.LessOrEqual(t, len(encoded), budget)
		var out semanticQueryResponse
		require.NoError(t, json.Unmarshal(encoded, &out))
		require.Empty(t, out.Text)
		require.Empty(t, out.MustRead)
		require.Len(t, out.Matches, 1)
		match := out.Matches[0]
		require.Equal(t, "pkg/search/service.go", match.Path)
		require.Equal(t, "Service.Search", match.Symbol)
		require.Equal(t, 61, match.StartLine)
		require.Equal(t, 121, match.EndLine)
		require.Empty(t, match.Preview)
		require.Empty(t, match.ExcerptContent)
		return match
	}

	t.Run("minimal actionable identity", func(t *testing.T) {
		decode(t, 290)
	})
	t.Run("skeletal code identity", func(t *testing.T) {
		match := decode(t, 600)
		require.Equal(t, "Service.Search", match.Title)
		require.Equal(t, "github.com/atomicobject/rhizome/pkg/search.Service.Search", match.FQN)
		require.Equal(t, "method", match.Kind)
		require.Equal(t, "a1", match.AnchorID)
		require.Equal(t, []SymbolHit{{Symbol: "Service.Search", StartLine: 61, EndLine: 121, Signature: "func (s *Service) Search(...)"}}, match.Symbols)
		require.NotNil(t, match.LinkTarget)
		require.Equal(t, "[[service]]", match.LinkTarget.Wikilink)
	})
}

func TestNotePreviewFromContentUsesProjectableHTML(t *testing.T) {
	preview := notePreviewFromContent("<p>Visible HTML body.</p>", "notes/preview.html", previewTestConfig(t))
	assert.Equal(t, "<p>Visible HTML body.</p>", preview)
}

func previewTestConfig(t *testing.T) Config {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return Config{NoteMetadata: indexer}
}

func TestAdjustOptionRatio_PenalizesSimilarity(t *testing.T) {
	group := semanticQueryGroup{
		match: SemanticMatchPayload{Type: "note", Path: "docs/Overview.md"},
		score: 0.9,
	}
	opt := semanticContentOption{plan: planExcerpt, cost: 500, utility: 1.0}
	ctx := semanticSelectionContext{
		selectedSummaries: []string{"overview semantic search pipeline"},
		selectedGroups:    map[string]int{},
	}
	ratio := adjustOptionRatio(opt, group, "overview semantic search pipeline", "note:docs/notes", ctx, semanticQueryIntent{}, docMatcher{})
	assert.Less(t, ratio, opt.utility/float64(opt.cost))
}

// TestSemanticGroupsFromPageMapsWireMetadata owns the application-page to
// wire-match mapping: identity, node metadata, section targets, and explain
// visibility must survive exactly as the page supplied them.
func TestSemanticGroupsFromPageMapsWireMetadata(t *testing.T) {
	noteRef := &ontology.NodeRef{NotePath: "docs/spec.md", Kind: ontology.NodeKindNote}
	sectionRef := &ontology.NodeRef{NotePath: "docs/spec.md", Fragment: "Search Ranking", NodeID: "section-search", Kind: ontology.NodeKindSection}
	vector := []search.Evidence{{Type: "note_vector_similarity", RawScore: 0.72}}
	storyJSON := `{"notePath":"docs/spec.md","nodeId":"story","typeName":"UserStory","kind":"EMBEDDED"}`
	sectionJSON := `{"notePath":"docs/spec.md","fragment":"Search Ranking","nodeId":"section-search","kind":"SECTION"}`

	tests := []struct {
		name    string
		source  search.RankedResult
		display *search.RankedResult
		explain bool
		want    SemanticMatchPayload
		role    string
	}{
		{
			name:   "mixed-case note path is preserved",
			source: search.RankedResult{Candidate: search.Candidate{Handle: knowledge.NoteHandle("Notes/Decision.MD"), Type: "note", Path: "Notes/Decision.MD", NoteID: "Notes/Decision.MD"}, FinalScore: 1},
			want:   SemanticMatchPayload{Type: "note", Path: "Notes/Decision.MD", Title: "Decision", ChunkIndex: -1, Score: 1},
		},
		{
			name:    "note node ref and evidence with explain",
			source:  search.RankedResult{Candidate: search.Candidate{Type: "note", Path: "docs/spec.md", Granularity: "node_body", Evidence: vector, NodeRef: noteRef}, FinalScore: 0.9},
			explain: true,
			want:    SemanticMatchPayload{Type: "note", Path: "docs/spec.md", Title: "spec", Granularity: "node_body", ChunkIndex: -1, Score: 0.9, NodeRef: noteRef, Evidence: map[string]float64{"vector": 0.72}},
			role:    "doc",
		},
		{
			name:   "explain false omits evidence",
			source: search.RankedResult{Candidate: search.Candidate{Type: "note", Path: "docs/spec.md", Granularity: "node_body", Evidence: vector, NodeRef: noteRef}, FinalScore: 0.9},
			want:   SemanticMatchPayload{Type: "note", Path: "docs/spec.md", Title: "spec", Granularity: "node_body", ChunkIndex: -1, Score: 0.9, NodeRef: noteRef},
		},
		{
			name:   "display doc section target overrides the ranked source",
			source: search.RankedResult{Candidate: search.Candidate{Type: "note", Path: "docs/spec.md", NoteID: "docs/spec.md"}, FinalScore: 0.9},
			display: &search.RankedResult{Candidate: search.Candidate{
				Type: "doc_section", Path: "docs/spec.md", NoteID: "docs/spec.md", Title: "Search Ranking", ChunkIndex: 3,
				NodeID: sectionRef.NodeID, NodeRefJSON: sectionJSON, SourceLocator: sectionRef.String(), NodeKind: string(ontology.NodeKindSection), NodeRef: sectionRef,
			}, FinalScore: 0.9},
			want: SemanticMatchPayload{
				Type: "note", Path: "docs/spec.md", Title: "Search Ranking", Heading: "Search Ranking", ChunkIndex: 3, Score: 0.9,
				NodeRef: sectionRef, NodeID: "section-search", NodeRefJSON: sectionJSON, SourceLocator: sectionRef.String(), NodeKind: "SECTION",
			},
		},
		{
			name: "embedded ontology node granularity",
			source: search.RankedResult{Candidate: search.Candidate{
				Type: "note", Path: "docs/spec.md", NoteID: "docs/spec.md", Title: "Faster checkout", Granularity: "node_body",
				NodeID: "node:story", NodeRefJSON: storyJSON, NodeKind: "EMBEDDED", NodeType: "UserStory",
			}, FinalScore: 0.9},
			want: SemanticMatchPayload{
				Type: "note", Path: "docs/spec.md", Title: "Faster checkout", Heading: "Faster checkout", Granularity: "node_body", ChunkIndex: -1, Score: 0.9,
				NodeID: "node:story", NodeRefJSON: storyJSON, NodeKind: "EMBEDDED", NodeType: "UserStory",
			},
		},
		{
			name: "code identity",
			source: search.RankedResult{Candidate: search.Candidate{
				Type: "code", Path: "pkg/search/service.go", Symbol: "Service.Search", FQN: "pkg/search.Service.Search", Kind: "method", AnchorID: "a1",
			}, FinalScore: 0.8},
			want: SemanticMatchPayload{
				Type: "code", Path: "pkg/search/service.go", Title: "Service.Search", Symbol: "Service.Search", FQN: "pkg/search.Service.Search",
				Kind: "method", AnchorID: "a1", ChunkIndex: -1, Score: 0.8,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unifiedsearch.ApplicationResult{Sources: []unifiedsearch.SourceAssessment{{Result: tt.source}}}
			if tt.display != nil {
				result.Display = []unifiedsearch.DisplayItem{{Primary: *tt.display}}
			}
			page := semanticGroupsFromPage(result, tt.explain)
			require.Len(t, page, 1)
			require.Equal(t, tt.want, page[0].match)
			if tt.role != "" {
				assignSemanticRoles(page, packGroups(page, semanticQueryIntent{}, docMatcher{}), semanticQueryIntent{}, docMatcher{})
				require.Equal(t, tt.role, page[0].match.Role)
			}
		})
	}
}

// TestPreviewFromBodyRendersUsefulPreview owns the emitted match preview:
// note summaries and prose, chunk fallbacks, and code declarations that skip
// package boilerplate.
func TestPreviewFromBodyRendersUsefulPreview(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Note.md"), []byte("# Title\n\nThe introduction paragraph is the note-level fallback preview text.\n\n## Details\n\nThis is a focused paragraph about embeddings.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package mcp\n\nfunc DoThing() {}\n"), 0o644))
	cfg := previewTestConfig(t)
	cfg.VaultPath = dir
	hits := func(ids ...string) []search.RankedResult {
		out := make([]search.RankedResult, 0, len(ids))
		for _, id := range ids {
			out = append(out, search.RankedResult{Candidate: search.Candidate{AnchorID: id}})
		}
		return out
	}

	tests := []struct {
		name     string
		body     string
		match    SemanticMatchPayload
		group    semanticQueryGroup
		anchors  map[string]codeanchor.IntelAnchor
		want     string
		contains []string
		excludes []string
	}{
		{
			name:  "note frontmatter summary wins",
			body:  "---\nsummary: This is the summary line.\n---\n\n# Title\n\nBody paragraph one.",
			match: SemanticMatchPayload{Type: "note", Path: "notes/preview.md"},
			want:  "This is the summary line.",
		},
		{
			name:  "note without summary uses first prose paragraph",
			body:  "# Title\n\nThis is the first paragraph of the note.\nIt should be used for preview.\n\nSecond paragraph.",
			match: SemanticMatchPayload{Type: "note", Path: "notes/preview.md"},
			want:  "This is the first paragraph of the note. It should be used for preview.",
		},
		{
			name:  "unselected note uses ranked chunk text",
			match: SemanticMatchPayload{Type: "note", Path: "Note.md"},
			group: semanticQueryGroup{hits: []search.RankedResult{{Candidate: search.Candidate{Path: "Note.md", ChunkIndex: 1}}}},
			want:  "This is a focused paragraph about embeddings.",
		},
		{
			name:     "code body keeps declarations and doc comments",
			body:     "package main\n\n# Search runs the query.\ndef search(q):\n    return None\n\nclass Searcher:\n    pass",
			match:    SemanticMatchPayload{Type: "code", Path: "search.py"},
			contains: []string{"Search runs the query", "def search(q):", "class Searcher:"},
			excludes: []string{"package main", "return None"},
		},
		{
			name:    "anchor previews skip package signatures",
			match:   SemanticMatchPayload{Type: "code", Path: "missing.go"},
			group:   semanticQueryGroup{hits: hits("pkg", "fn")},
			anchors: map[string]codeanchor.IntelAnchor{"pkg": {AnchorID: "pkg", Signature: "package mcp"}, "fn": {AnchorID: "fn", Signature: "func DoThing()"}},
			want:    "func DoThing()",
		},
		{
			name:  "low-signal package body falls back to file declaration",
			body:  "package mcp",
			match: SemanticMatchPayload{Type: "code", Path: "main.go"},
			want:  "func DoThing() {}",
		},
		{
			name:    "package-only anchor span falls back to file declaration",
			match:   SemanticMatchPayload{Type: "code", Path: "main.go"},
			group:   semanticQueryGroup{hits: hits("a1")},
			anchors: map[string]codeanchor.IntelAnchor{"a1": {AnchorID: "a1", Path: "main.go", StartLine: 1, EndLine: 1}},
			want:    "func DoThing() {}",
		},
		{
			name:  "code anchors summarize top hits and remaining spread",
			match: SemanticMatchPayload{Type: "code", Path: "missing.go"},
			group: semanticQueryGroup{spread: 4, hits: hits("a1", "a2", "a3")},
			anchors: map[string]codeanchor.IntelAnchor{
				"a1": {AnchorID: "a1", Signature: "func Search(q string) error", DocComment: "Run the query."},
				"a2": {AnchorID: "a2", Signature: "type Searcher struct", DocComment: "Coordinates retrieval."},
				"a3": {AnchorID: "a3", Signature: "func (s *Searcher) Execute() error"},
			},
			want: "func Search(q string) error — Run the query.\ntype Searcher struct — Coordinates retrieval.\nfunc (s *Searcher) Execute() error\n…and 1 more matches",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := tt.group
			group.match = tt.match
			preview := previewFromBody(tt.body, tt.match, cfg, map[string]string{}, map[string]string{}, map[string][]codeanchor.IntelAnchor{}, nil, group, tt.anchors)
			if tt.want != "" {
				require.Equal(t, tt.want, preview)
			}
			for _, want := range tt.contains {
				require.Contains(t, preview, want)
			}
			for _, excluded := range tt.excludes {
				require.NotContains(t, preview, excluded)
			}
		})
	}
}

// TestRenderedSemanticContentKinds owns the renderer-to-match content fields:
// a rendered outline is emitted only as outline content, and a package-only
// file excerpt is never claimed as body content.
func TestRenderedSemanticContentKinds(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package mcp\n\nfunc DoThing() {}\n"), 0o644))
	group := semanticQueryGroup{
		match: SemanticMatchPayload{Type: "code", Path: "main.go"},
		hits:  []search.RankedResult{{Candidate: search.Candidate{AnchorID: "pkg"}}, {Candidate: search.Candidate{AnchorID: "fn"}}},
	}
	anchors := map[string]codeanchor.IntelAnchor{
		"pkg": {AnchorID: "pkg", Path: "main.go", StartLine: 1, EndLine: 1},
		"fn":  {AnchorID: "fn", Path: "main.go", StartLine: 10, Signature: "func Search(ctx context.Context, q string) []Result"},
	}
	cfg := Config{VaultPath: dir}

	outline, _, truncated := renderSemanticBody(context.Background(), cfg, nil, group, anchors, planOutline, 2000)
	require.Equal(t, "L10 func Search(ctx context.Context, q string) []Result", outline)
	outlined := SemanticMatchPayload{}
	assignContentField(&outlined, planOutline, outline, truncated)
	require.Equal(t, SemanticMatchPayload{ContentKind: "outline", OutlineContent: outline}, outlined)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "doc.go"), []byte("package mcp\n"), 0o644))
	excerptGroup := semanticQueryGroup{match: SemanticMatchPayload{Type: "code", Path: "doc.go"}, hits: group.hits[:1]}
	excerpt, _, truncated := renderSemanticBody(context.Background(), cfg, nil, excerptGroup, anchors, planExcerpt, 2000)
	require.Equal(t, "package mcp", strings.TrimSpace(excerpt))
	excerpted := SemanticMatchPayload{}
	assignContentField(&excerpted, planExcerpt, excerpt, truncated)
	require.Equal(t, SemanticMatchPayload{}, excerpted)
}
