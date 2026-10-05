//go:build cgo

package codeanchor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRewriteCollectionExpressions_PreservesLineCount(t *testing.T) {
	content := []byte("class A {\n  List<int> Items { get; init; } = [1,\n    2];\n}\n")
	spans := []byteSpan{{start: strings.Index(string(content), "[1,"), end: strings.Index(string(content), "2]") + 2}}
	rewritten, mapper, changed := rewriteCollectionExpressions(content, spans)
	require.True(t, changed)
	require.Equal(t, strings.Count(string(content), "\n"), strings.Count(string(rewritten), "\n"))
	require.Equal(t, int64(4), mapper.OriginalLine(4))
}

func TestRewriteCollectionExpressions_OnlyTouchesErrorRegions(t *testing.T) {
	content := []byte("class A {\n  void M() {\n    var near = [];\n" + strings.Repeat(" ", collectionExprErrorPadding*3) + "var far = [];\n  }\n}\n")
	start := strings.Index(string(content), "var near = []") + len("var near = ")
	rewritten, _, changed := rewriteCollectionExpressions(content, []byteSpan{{start: start, end: start + 2}})
	require.True(t, changed)
	require.Contains(t, string(rewritten), "var near = new object()")
	require.Contains(t, string(rewritten), "var far = []")
}

func TestRewriteCollectionExpressions_HandlesArgumentAndSpreadForms(t *testing.T) {
	content := []byte("class A {\n  object M(object filters) {\n    return Merge(filters, [], [.. Defaults()]);\n  }\n}\n")
	start := strings.Index(string(content), "[]")
	end := strings.LastIndex(string(content), "]") + 1
	rewritten, _, changed := rewriteCollectionExpressions(content, []byteSpan{{start: start, end: end}})
	require.True(t, changed)
	require.Contains(t, string(rewritten), "Merge(filters, new object(), new object())")
}

func TestRewriteCollectionExpressions_DoesNotRewriteIndexers(t *testing.T) {
	content := []byte("class A {\n  void M() {\n    filters.Or[0].And.Add(value);\n  }\n}\n")
	start := strings.Index(string(content), "[0]")
	rewritten, _, changed := rewriteCollectionExpressions(content, []byteSpan{{start: start, end: start + len("[0]")}})
	require.False(t, changed)
	require.Equal(t, string(content), string(rewritten))
}

func TestRewriteCollectionExpressions_HandlesZeroWidthErrorSpans(t *testing.T) {
	content := []byte("class A {\n  object M() {\n    return [];\n  }\n}\n")
	start := strings.Index(string(content), "[]")
	spans := []byteSpan{{start: start, end: start}}
	rewritten, _, changed := rewriteCollectionExpressions(content, spans)
	require.True(t, changed)
	require.Contains(t, string(rewritten), "return new object();")
}

func TestMergeCSharpSummaries_PrefersPrimaryAndSupplementsRecovered(t *testing.T) {
	primary := FileSummary{
		FilePath:   "a.cs",
		Lang:       LangCs,
		Imports:    []ImportEdge{{Module: "N.Core"}},
		Symbols:    []Symbol{{Lang: LangCs, Kind: SymClass, File: "a.cs", Pkg: "N", Name: "Worker", StartLine: 1, EndLine: 10, DocComment: "primary"}},
		TypeRefs:   []TypeRef{{File: "a.cs", OwnerFQN: "N.Worker.Build", TypeSym: SymbolRef{Lang: LangCs, Pkg: "N", Name: "Task"}}},
		MemberRefs: []MemberRef{{File: "a.cs", OwnerFQN: "N.Worker.Build", Sym: SymbolRef{Lang: LangCs, Pkg: "N.Constants", Name: "Existing"}}},
		Calls:      []CallSite{{File: "a.cs", OwnerFQN: "N.Worker.Build", CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "N.Reporter", Name: "Start"}}},
	}
	recovered := FileSummary{
		FilePath: "a.cs",
		Lang:     LangCs,
		Imports: []ImportEdge{
			{Module: "N.Core"},
			{Module: "N.Helpers"},
		},
		Symbols: []Symbol{
			{Lang: LangCs, Kind: SymClass, File: "a.cs", Pkg: "N", Name: "Worker", StartLine: 1, EndLine: 10, DocComment: "recovered"},
			{Lang: LangCs, Kind: SymMethod, File: "a.cs", Pkg: "N.Worker", Name: "Build", StartLine: 3, EndLine: 6},
		},
		TypeRefs: []TypeRef{
			{File: "a.cs", OwnerFQN: "N.Worker.Build", TypeSym: SymbolRef{Lang: LangCs, Pkg: "N", Name: "Task"}},
			{File: "a.cs", OwnerFQN: "N.Worker.Build", TypeSym: SymbolRef{Lang: LangCs, Pkg: "N.Helpers", Name: "Widget"}},
		},
		MemberRefs: []MemberRef{
			{File: "a.cs", OwnerFQN: "N.Worker.Build", Sym: SymbolRef{Lang: LangCs, Pkg: "N.Constants", Name: "Existing"}},
			{File: "a.cs", OwnerFQN: "N.Worker.Build", Sym: SymbolRef{Lang: LangCs, Pkg: "N.Constants", Name: "Recovered"}},
		},
		Calls: []CallSite{
			{File: "a.cs", OwnerFQN: "N.Worker.Build", CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "N.Reporter", Name: "Start"}},
			{File: "a.cs", OwnerFQN: "N.Worker.Build", CalleeSymbol: SymbolRef{Lang: LangCs, Pkg: "N.Trace", Name: "Stop"}},
		},
	}

	merged := mergeCSharpSummaries(primary, recovered, newIdentitySourceMap())
	require.Len(t, merged.Imports, 2)
	require.Len(t, merged.Symbols, 2)
	require.Equal(t, "primary", merged.Symbols[0].DocComment)
	require.Len(t, merged.TypeRefs, 2)
	require.Len(t, merged.MemberRefs, 2)
	require.Len(t, merged.Calls, 2)
	require.Contains(t, merged.Imports, ImportEdge{Module: "N.Helpers"})
	require.Contains(t, merged.Symbols, recovered.Symbols[1])
	require.Contains(t, merged.TypeRefs, recovered.TypeRefs[1])
	require.Contains(t, merged.MemberRefs, recovered.MemberRefs[1])
	require.Contains(t, merged.Calls, recovered.Calls[1])
}
