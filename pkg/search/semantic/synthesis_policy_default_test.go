package semantic

import (
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestMeaningfulStringLiterals_BoundsCandidateScan(t *testing.T) {
	span := strings.Repeat(`"x" `, namedSignalsMaxCount) + `"TARGET_SIGNAL"`

	require.Empty(t, meaningfulStringLiterals(span), "named signals are best-effort and must not scan unbounded module text")
}

func TestDefaultSynthesisPolicy_FunctionShortNoDocIncludesBody(t *testing.T) {
	policy := DefaultSynthesisPolicy(PolicyOptions{
		Budget:          ChunkBudget{TargetChars: 1500, MaxChars: 2800, Overlap: 300},
		MaxPrimaryChars: 2000,
		ShortBodyLines:  50,
	})

	content := []byte("package x\n\nfunc DoThing() string {\n\treturn \"ok\"\n}\n")
	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "x.go",
		Symbol:      "DoThing",
		FQN:         "x.DoThing",
		Signature:   "func DoThing() string",
		StartByte:   11,
		EndByte:     int64(len(content)),
		StartLine:   3,
		EndLine:     5,
		Fingerprint: "fp",
	}

	chunks := policy.BuildAnchorChunks(anchor, content, nil, SymbolChunkContext{})
	require.Len(t, chunks, 1)
	require.Equal(t, 0, chunks[0].Input.Index)
	require.Equal(t, "signature_body", chunks[0].Input.Granularity)
	require.Contains(t, chunks[0].Text, "return \"ok\"")
}

func TestDefaultSynthesisPolicy_DocumentedFunctionOmitsBody(t *testing.T) {
	policy := DefaultSynthesisPolicy(PolicyOptions{
		Budget:          ChunkBudget{TargetChars: 1500, MaxChars: 2800, Overlap: 300},
		MaxPrimaryChars: 2000,
		ShortBodyLines:  50,
	})

	content := []byte("package x\n\nfunc DoThing() string {\n\treturn \"ok\"\n}\n")
	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "x.go",
		Symbol:      "DoThing",
		FQN:         "x.DoThing",
		Signature:   "func DoThing() string",
		DocComment:  "This is a long-ish doc comment that should suppress including the body in the primary chunk.",
		Calls:       []string{"fmt.Printf", "net/http.Get"},
		StartByte:   11,
		EndByte:     int64(len(content)),
		StartLine:   3,
		EndLine:     5,
		Fingerprint: "fp",
	}

	chunks := policy.BuildAnchorChunks(anchor, content, nil, SymbolChunkContext{})
	require.Len(t, chunks, 1)
	require.Equal(t, "signature_doc", chunks[0].Input.Granularity)
	require.NotContains(t, chunks[0].Text, "return \"ok\"")
	require.NotContains(t, chunks[0].Text, "Calls:")
	require.NotContains(t, chunks[0].Text, "fmt.Printf")
	require.NotContains(t, chunks[0].Text, "Related calls")
}

func TestDefaultSynthesisPolicy_DocumentedFunctionIncludesImplementationSignals(t *testing.T) {
	policy := DefaultSynthesisPolicy(PolicyOptions{
		Budget:          ChunkBudget{TargetChars: 1500, MaxChars: 2800, Overlap: 300},
		MaxPrimaryChars: 2000,
		ShortBodyLines:  50,
	})

	content := []byte("package x\n\nfunc Load() error {\n\tmode := os.Getenv(\"RHIZOME_TEST_MODE\")\n\tif mode == \"strict-mode\" {\n\t\treturn fmt.Errorf(\"config.load_failed\")\n\t}\n\treturn nil\n}\n")
	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "x.go",
		Symbol:      "Load",
		FQN:         "x.Load",
		Signature:   "func Load() error",
		DocComment:  "Loads runtime configuration while hiding implementation details from the primary embedding body.",
		StartByte:   11,
		EndByte:     int64(len(content)),
		StartLine:   3,
		EndLine:     9,
		Fingerprint: "fp",
	}

	chunks := policy.BuildAnchorChunks(anchor, content, nil, SymbolChunkContext{})
	require.Len(t, chunks, 1)
	require.Equal(t, "signature_doc", chunks[0].Input.Granularity)
	require.Contains(t, chunks[0].Text, "Named signals:")
	require.Contains(t, chunks[0].Text, "RHIZOME_TEST_MODE")
	require.NotContains(t, chunks[0].Text, "branching/control flow")
	require.NotContains(t, chunks[0].Text, "error handling")
	require.NotContains(t, chunks[0].Text, "mode := os.Getenv")
}

func TestDefaultSynthesisPolicy_UsesAnchorRelatedDocsWhenTitlesOmitted(t *testing.T) {
	policy := DefaultSynthesisPolicy(PolicyOptions{
		Budget: ChunkBudget{TargetChars: 1500, MaxChars: 2800, Overlap: 300},
	})

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "x.go",
		Symbol:      "ResolveStory",
		FQN:         "x.ResolveStory",
		Signature:   "func ResolveStory()",
		DocComment:  "Resolves a user story into structured answer context.",
		RelatedDocs: []string{"User Story Retrieval Spec (docs/specs/user-story.md)"},
		Fingerprint: "fp",
	}

	chunks := policy.BuildAnchorChunks(anchor, nil, nil, SymbolChunkContext{})
	require.Len(t, chunks, 1)
	require.Contains(t, chunks[0].Text, "Related docs:")
	require.Contains(t, chunks[0].Text, "User Story Retrieval Spec")
}

func TestDefaultSynthesisPolicy_AddsBoundedDeterministicSurfaceLabels(t *testing.T) {
	policy := DefaultSynthesisPolicy(PolicyOptions{Budget: DefaultChunkBudget()})
	anchor := codeanchor.IntelAnchor{
		AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "handler",
		Path: "pkg/app/mcp/config/handler_test.go", Symbol: "handleConfig", FQN: "mcp.handleConfig",
		Signature: "func handleConfig()", Fingerprint: "fp",
	}

	chunks := policy.BuildAnchorChunks(anchor, nil, nil, SymbolChunkContext{})
	require.Len(t, chunks, 1)
	require.Contains(t, chunks[0].Text, "Surface: test, agent-tool, configuration")
	require.NotContains(t, chunks[0].Text, "boundary-handler", "surface labels are capped at three")
}

func TestDefaultSynthesisPolicy_DropsEnrichmentBeforeAuthoredContentAtBudget(t *testing.T) {
	policy := DefaultSynthesisPolicy(PolicyOptions{
		Budget: ChunkBudget{TargetChars: 180, MaxChars: 220},
	})
	content := []byte("func Load() { use(\"CONFIGURATION_SIGNAL\") }")
	anchor := codeanchor.IntelAnchor{
		AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/config/load.go",
		Symbol: "Load", FQN: "config.Load", Signature: "func Load()",
		DocComment: "Authoritative behavior description that must survive before optional retrieval enrichment. " +
			"This authored explanation is intentionally long enough to consume the remaining primary chunk budget.",
		StartByte: 0, EndByte: int64(len(content)), Fingerprint: "fp",
	}

	chunks := policy.BuildAnchorChunks(anchor, content, nil, SymbolChunkContext{})
	require.Len(t, chunks, 1)
	require.Contains(t, chunks[0].Text, "Authoritative behavior description")
	require.NotContains(t, chunks[0].Text, "CONFIGURATION_SIGNAL")
	require.LessOrEqual(t, RuneLen(chunks[0].Text), 220)
}

func TestBuildAnchorChunksIncludesModuleAndRationaleProse(t *testing.T) {
	policy := DefaultSynthesisPolicy(PolicyOptions{
		Budget:          ChunkBudget{TargetChars: 1500, MaxChars: 2800, Overlap: 300},
		MaxPrimaryChars: 2000,
		ShortBodyLines:  50,
	})
	content := []byte("func Foo() {\n\treturn\n}\n")
	anchor := codeanchor.IntelAnchor{FQN: "pkg.Foo", Symbol: "Foo", Signature: "func Foo()", Kind: "function", Path: "x.go", Lang: codeanchor.LangGo, StartLine: 1, EndLine: 3, EndByte: int64(len(content))}
	context := SymbolChunkContext{
		ModuleDoc: "Package pkg owns retrieval.",
		Rationale: []codeanchor.Rationale{{Kind: "why", Content: "Foo exists because  callers\nneed it."}},
	}
	chunks := policy.BuildAnchorChunks(anchor, content, nil, context)
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	text := chunks[0].Text
	for _, want := range []string{"Module: Package pkg owns retrieval.", "Rationale:\n- why: Foo exists because callers need it."} {
		if !strings.Contains(text, want) {
			t.Fatalf("chunk text missing %q:\n%s", want, text)
		}
	}
}
