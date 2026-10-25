package embedindex

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
)

func TestBuildModuleChunk_SortsExports(t *testing.T) {
	builder := ChunkBuilder{Budget: DefaultChunkBudget()}
	anchors := []codeanchor.IntelAnchor{
		{Symbol: "B"},
		{Symbol: "A"},
	}
	chunk := builder.BuildModuleChunk("pkg/foo.go", codeanchor.Lang("go"), anchors, []string{"Example context"})
	if chunk.Input.Granularity != "module" {
		t.Fatalf("unexpected granularity: %s", chunk.Input.Granularity)
	}
	if chunk.Text == "" || chunk.Input.Hash == "" {
		t.Fatalf("expected chunk text and hash")
	}
	if chunk.Input.Breadcrumb != "pkg/foo.go" {
		t.Fatalf("breadcrumb not normalized: %s", chunk.Input.Breadcrumb)
	}
	if chunk.Input.Heading != "foo.go" {
		t.Fatalf("unexpected heading: %s", chunk.Input.Heading)
	}
	if !strings.Contains(chunk.Text, "Exports: A, B") {
		t.Fatalf("exports not sorted: %s", chunk.Text)
	}
}

func TestBuildModuleChunk_IncludesCompactFieldSignals(t *testing.T) {
	builder := ChunkBuilder{Budget: DefaultChunkBudget()}
	anchors := []codeanchor.IntelAnchor{
		{Kind: "field", Symbol: "zeta"},
		{Kind: "field", Symbol: "Alpha"},
		{Kind: "function", Symbol: "Run", Signature: "func Run()"},
	}

	chunk := builder.BuildModuleChunk("pkg/foo.go", codeanchor.Lang("go"), anchors, nil)

	if !strings.Contains(chunk.Text, "Exports: Run") {
		t.Fatalf("non-field exports missing: %s", chunk.Text)
	}
	if !strings.Contains(chunk.Text, "Fields: Alpha, zeta") {
		t.Fatalf("field signal missing or unsorted: %s", chunk.Text)
	}
	if strings.Contains(chunk.Text, "Signatures:\n- field ") {
		t.Fatalf("field signatures should not flood module signatures: %s", chunk.Text)
	}
}

func TestBuildModuleChunk_TruncatesFieldSignals(t *testing.T) {
	builder := ChunkBuilder{Budget: DefaultChunkBudget()}
	anchors := make([]codeanchor.IntelAnchor, 0, 45)
	for i := 0; i < 45; i++ {
		anchors = append(anchors, codeanchor.IntelAnchor{Kind: "field", Symbol: fmt.Sprintf("field%02d", i)})
	}

	chunk := builder.BuildModuleChunk("pkg/foo.go", codeanchor.Lang("go"), anchors, nil)

	if !strings.Contains(chunk.Text, "+5 more fields") {
		t.Fatalf("expected field truncation marker: %s", chunk.Text)
	}
}

func TestBuildModuleChunk_DoesNotIncludeCallsInPrimaryChunk(t *testing.T) {
	builder := ChunkBuilder{Budget: DefaultChunkBudget()}
	anchors := []codeanchor.IntelAnchor{
		{Kind: "module", Path: "pkg/foo.go", Calls: []string{"service.Search"}},
		{Kind: "function", Symbol: "Run", Signature: "func Run()"},
	}

	chunk := builder.BuildModuleChunk("pkg/foo.go", codeanchor.Lang("go"), anchors, nil)

	if strings.Contains(chunk.Text, "Calls:") || strings.Contains(chunk.Text, "service.Search") {
		t.Fatalf("module primary chunk should stay call-insensitive: %s", chunk.Text)
	}
}

func TestBuildAnchorChunks_IncludesModuleDocAndInSpanRationale(t *testing.T) {
	policy := DefaultSynthesisPolicy(PolicyOptions{Budget: DefaultChunkBudget()})
	anchor := codeanchor.IntelAnchor{
		Kind: "function", Lang: codeanchor.Lang("go"), Symbol: "Merge", FQN: "merge.Merge",
		Path: "pkg/search/merge.go", Signature: "func Merge()", DocComment: "Merge combines lanes.",
		StartLine: 10, EndLine: 40,
	}
	context := SymbolChunkContext{
		ModuleDoc: "Package merge folds lane results into one ranking.",
		Rationale: RationaleInSpan(anchor, []codeanchor.Rationale{
			{Kind: codeanchor.RationaleWhy, Content: "duplicate evidence is not independent evidence", StartLine: 12, EndLine: 12},
			{Kind: codeanchor.RationaleNote, Content: "agreement is capped, not summed", StartLine: 20, EndLine: 21},
			{Kind: codeanchor.RationaleTodo, Content: "unrelated to this symbol", StartLine: 90, EndLine: 90},
		}),
	}

	chunk := policy.BuildAnchorChunks(anchor, nil, nil, context)[0]

	if !strings.Contains(chunk.Text, "Module: Package merge folds lane results into one ranking.") {
		t.Fatalf("module doc missing: %s", chunk.Text)
	}
	if !strings.Contains(chunk.Text, "Rationale:\n") {
		t.Fatalf("rationale section missing: %s", chunk.Text)
	}
	if got := strings.Count(chunk.Text, "\n- "); got != 2 {
		t.Fatalf("expected 2 in-span rationale lines, got %d: %s", got, chunk.Text)
	}
	if !strings.Contains(chunk.Text, "- why: duplicate evidence is not independent evidence") {
		t.Fatalf("rationale content missing: %s", chunk.Text)
	}
	if strings.Contains(chunk.Text, "unrelated to this symbol") {
		t.Fatalf("out-of-span rationale leaked: %s", chunk.Text)
	}
}
