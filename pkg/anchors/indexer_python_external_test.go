//go:build cgo

package codeanchor

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestPythonIndexer_ExtractsCuratedBuiltinExternalEvidence(t *testing.T) {
	summary, err := NewPythonIndexerWithRoots([]string{"src"}).IndexFile([]byte(`
def render(items):
    print(len(items))
    values = list(items)
    mystery(items)
`), paths.CodePathRef{Rel: "src/app.py"})
	require.NoError(t, err)

	require.Len(t, summary.ExternalEvidence.Symbols, 3)
	byName := make(map[string]ExternalSymbolEvidenceInput)
	for _, input := range summary.ExternalEvidence.Symbols {
		byName[input.Raw.DstName] = input
	}
	for _, name := range []string{"print", "len", "list"} {
		input, ok := byName[name]
		require.True(t, ok, name)
		require.Equal(t, "app.render", input.OwnerFQN)
		require.Equal(t, RefKindCalls, input.RefKind)
		require.Equal(t, RawSymbolTargetKey{DstLang: LangPy, DstPkg: "builtins", DstName: name, DstFQN: "builtins." + name}, input.Raw)
		require.Equal(t, ExternalTargetIdentity{Ecosystem: ExternalEcosystemPython, Module: "builtins", SymbolPath: name, Kind: ExternalTargetRuntimeBuiltin}, input.Target.ExternalTargetIdentity)
		require.Equal(t, ExternalEvidence{Kind: ExternalEvidenceRuntimeBuiltin, Confidence: ExternalConfidenceHigh, LocalName: name}, input.Evidence)
	}
	_, unknown := byName["mystery"]
	require.False(t, unknown)
}

func TestPythonIndexer_LocalAndImportBindingsWinOverBuiltins(t *testing.T) {
	summary, err := NewPythonIndexerWithRoots([]string{"src"}).IndexFile([]byte(`
from helpers import print

def len(value):
    return value

list = make_list()

def render(items):
    print(items)
    len(items)
    list(items)
    range(3)
`), paths.CodePathRef{Rel: "src/app.py"})
	require.NoError(t, err)

	require.Len(t, summary.ExternalEvidence.Symbols, 1)
	require.Equal(t, "range", summary.ExternalEvidence.Symbols[0].Raw.DstName)
}

func TestPythonIndexer_ParameterBindingWinsOverBuiltin(t *testing.T) {
	summary, err := NewPythonIndexerWithRoots([]string{"src"}).IndexFile([]byte(`
def f(print):
	print(x)
	len(x)
`), paths.CodePathRef{Rel: "src/app.py"})
	require.NoError(t, err)

	require.Len(t, summary.ExternalEvidence.Symbols, 1)
	require.Equal(t, "len", summary.ExternalEvidence.Symbols[0].Raw.DstName)
}

func TestPythonIndexer_LexicalBindingFormsSuppressBuiltinEvidence(t *testing.T) {
	summary, err := NewPythonIndexerWithRoots([]string{"src"}).IndexFile([]byte(`
def run(items, manager):
    callback = lambda print: print("local")
    values = [len(item) for len in items]
    for str in items:
        str()
    with manager() as list:
        list()
    try:
        callback()
    except Failure as type:
        type()
    return range(3)
`), paths.CodePathRef{Rel: "src/app.py"})
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, input := range summary.ExternalEvidence.Symbols {
		names[input.Raw.DstName] = true
	}
	for _, shadowed := range []string{"print", "len", "str", "list", "type"} {
		require.False(t, names[shadowed], shadowed)
	}
	require.True(t, names["range"])
}

func TestPythonBuiltinExternalEvidence_CoversEveryParserRefKind(t *testing.T) {
	summary := FileSummary{
		Lang:       LangPy,
		Calls:      []CallSite{{OwnerFQN: "app.f", CalleeSymbol: SymbolRef{Lang: LangPy, Pkg: "builtins", Name: "list"}}},
		TypeRefs:   []TypeRef{{OwnerFQN: "app.f", TypeSym: SymbolRef{Lang: LangPy, Pkg: "builtins", Name: "str"}}},
		MemberRefs: []MemberRef{{OwnerFQN: "app.f", Sym: SymbolRef{Lang: LangPy, Pkg: "builtins", Name: "len"}}},
	}

	batch := pythonBuiltinExternalEvidence(summary, nil)
	require.Len(t, batch.Symbols, 3)
	kinds := map[RefKind]bool{}
	for _, input := range batch.Symbols {
		kinds[input.RefKind] = true
	}
	require.Equal(t, map[RefKind]bool{RefKindCalls: true, RefKindTypeRef: true, RefKindMemberRef: true}, kinds)
}
