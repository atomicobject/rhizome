package semantic

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
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

func TestBuildBodyChunks_SplitsAndHashes(t *testing.T) {
	builder := ChunkBuilder{Budget: ChunkBudget{TargetChars: 10, MaxChars: 30, Overlap: 5}}
	anchor := codeanchor.IntelAnchor{Kind: "func", Lang: codeanchor.Lang("go"), FQN: "f", Path: "pkg/foo.go", Symbol: "f"}
	body := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+TAIL"
	chunks := builder.BuildBodyChunks(anchor, body)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	var reconstructed strings.Builder
	for i, ch := range chunks {
		// Index 0 is reserved for the non-body "symbol" chunk; body chunks start at 1.
		if ch.Input.Index != i+1 {
			t.Fatalf("expected chunk index %d, got %d", i+1, ch.Input.Index)
		}
		if ch.Input.Hash != embeddings.HashText(ch.Text) || ch.Text == "" {
			t.Fatalf("chunk missing hash or text")
		}
		if !strings.Contains(ch.Text, "Chunk: ") {
			t.Fatalf("chunk header missing: %s", ch.Text)
		}
		parts := strings.SplitN(ch.Text, "\n\n", 2)
		if len(parts) != 2 || parts[1] == "" {
			t.Fatalf("empty body slice: %q", ch.Text)
		}
		if i == 0 {
			reconstructed.WriteString(parts[1])
		} else {
			if got := parts[1][:5]; got != reconstructed.String()[reconstructed.Len()-5:] {
				t.Fatalf("overlap mismatch: %q", got)
			}
			reconstructed.WriteString(parts[1][5:])
		}
	}
	if reconstructed.String() != body {
		t.Fatalf("body changed: got %q want %q", reconstructed.String(), body)
	}
	again := builder.BuildBodyChunks(anchor, body)
	for i := range chunks {
		if chunks[i].Input.Hash != again[i].Input.Hash {
			t.Fatalf("unstable hash at %d", i)
		}
	}
}

func TestExtractSpan_FallbackToLines(t *testing.T) {
	content := []byte("line1\nline2\nline3\n")
	anchor := codeanchor.IntelAnchor{StartLine: 2, EndLine: 3}
	span := ExtractSpan(content, anchor)
	if span == "" {
		t.Fatalf("expected non-empty span")
	}
	if span != "line2\nline3" {
		t.Fatalf("unexpected span: %q", span)
	}
}

func TestBuildSymbolChunk_IncludesModuleDocAndInSpanRationale(t *testing.T) {
	builder := ChunkBuilder{Budget: DefaultChunkBudget()}
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

	chunk := builder.BuildSymbolChunk(anchor, nil, context)

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

func TestBuildSymbolChunk_EmptyContextKeepsPreviousFormat(t *testing.T) {
	builder := ChunkBuilder{Budget: DefaultChunkBudget()}
	anchor := codeanchor.IntelAnchor{
		Kind: "function", Lang: codeanchor.Lang("go"), Symbol: "Merge", FQN: "merge.Merge",
		Path: "pkg/search/merge.go", Signature: "func Merge()", DocComment: "Merge combines lanes.",
	}

	chunk := builder.BuildSymbolChunk(anchor, nil, SymbolChunkContext{})

	want := "Kind: function\nLang: go\nSymbol: Merge\nFQN: merge.Merge\nPath: pkg/search/merge.go\nRep: symbol\n\n" +
		"Signature: func Merge()\n\nDoc:\nMerge combines lanes."
	if chunk.Text != want {
		t.Fatalf("empty-context chunk text changed:\ngot:  %q\nwant: %q", chunk.Text, want)
	}
}

func TestBuildBodyChunks_CarrySymbolSignatureAndDoc(t *testing.T) {
	builder := ChunkBuilder{Budget: ChunkBudget{TargetChars: 10, MaxChars: 30, Overlap: 5}}
	body := strings.Repeat("x", 80)

	withContext := codeanchor.IntelAnchor{
		Kind: "func", Lang: codeanchor.Lang("go"), FQN: "f", Path: "pkg/foo.go", Symbol: "f",
		Signature: "func f()", DocComment: "f does the thing.\nMore detail.",
	}
	withChunks := builder.BuildBodyChunks(withContext, body)
	if len(withChunks) == 0 {
		t.Fatal("expected body chunks with context")
	}
	for _, ch := range withChunks {
		if !strings.Contains(ch.Text, "Signature: func f()\n") {
			t.Fatalf("body chunk missing signature: %s", ch.Text)
		}
		if !strings.Contains(ch.Text, "Doc: f does the thing.\n") {
			t.Fatalf("body chunk missing doc summary: %s", ch.Text)
		}
	}

	bare := codeanchor.IntelAnchor{Kind: "func", Lang: codeanchor.Lang("go"), FQN: "f", Path: "pkg/foo.go", Symbol: "f"}
	bareChunks := builder.BuildBodyChunks(bare, body)
	if len(bareChunks) == 0 {
		t.Fatal("expected bare body chunks")
	}
	for _, ch := range bareChunks {
		if strings.Contains(ch.Text, "Signature:") || strings.Contains(ch.Text, "Doc:") {
			t.Fatalf("body chunk added empty context lines: %s", ch.Text)
		}
	}
}
