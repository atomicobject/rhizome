package retrieval

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestCallEdgesRetriever_ReturnsCallersAndCallees(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "calls.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	callee := codeanchor.IntelAnchor{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/dep.go",
		Symbol:      "Target",
		FQN:         "pkg.dep.Target",
		Fingerprint: "fp-b1",
	}
	caller := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/one.go",
		Symbol:      "Caller",
		FQN:         "pkg.one.Caller",
		Fingerprint: "fp-a1",
	}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/dep.go", []codeanchor.IntelAnchor{callee}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/one.go", []codeanchor.IntelAnchor{caller}, []codeanchor.IntelEdge{
		{SrcID: "a1", DstID: "b1", Kind: "calls"},
	}, nil))

	callers := &CallEdgesRetriever{Store: store, Mode: CallEdgesCallers, Limit: 5}
	callerResults, err := callers.Retrieve(ctx, search.QuerySpec{Text: "pkg.dep.Target"})
	require.NoError(t, err)
	require.NotEmpty(t, callerResults)

	foundCaller := false
	for _, c := range callerResults {
		if c.AnchorID == "a1" {
			foundCaller = true
			break
		}
	}
	require.True(t, foundCaller, "expected caller anchor")

	callees := &CallEdgesRetriever{Store: store, Mode: CallEdgesCallees, Limit: 5}
	calleeResults, err := callees.Retrieve(ctx, search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.FileHandle("pkg/one.go")},
	})
	require.NoError(t, err)
	require.NotEmpty(t, calleeResults)

	foundCallee := false
	for _, c := range calleeResults {
		if c.AnchorID == "b1" {
			foundCallee = true
			break
		}
	}
	require.True(t, foundCallee, "expected callee anchor")

	calleesBySymbol := &CallEdgesRetriever{Store: store, Mode: CallEdgesCallees, Limit: 5}
	calleeSymbolResults, err := calleesBySymbol.Retrieve(ctx, search.QuerySpec{Text: "Caller"})
	require.NoError(t, err)
	require.NotEmpty(t, calleeSymbolResults)

	foundSymbolCallee := false
	for _, c := range calleeSymbolResults {
		if c.AnchorID == "b1" {
			foundSymbolCallee = true
			break
		}
	}
	require.True(t, foundSymbolCallee, "expected callee anchor from symbol query")
}

func TestCallEdgesRetriever_CalleesRestrictedToCallerAnchor(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "callees-restrict.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	callee1 := codeanchor.IntelAnchor{
		AnchorID:    "b1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/dep.go",
		Symbol:      "Target",
		FQN:         "pkg.dep.Target",
		Fingerprint: "fp-b1",
	}
	callee2 := codeanchor.IntelAnchor{
		AnchorID:    "c1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/dep2.go",
		Symbol:      "Other",
		FQN:         "pkg.dep.Other",
		Fingerprint: "fp-c1",
	}
	caller1 := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/one.go",
		Symbol:      "Caller",
		FQN:         "pkg.one.Caller",
		Fingerprint: "fp-a1",
	}
	caller2 := codeanchor.IntelAnchor{
		AnchorID:    "a2",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "pkg/one.go",
		Symbol:      "OtherCaller",
		FQN:         "pkg.one.OtherCaller",
		Fingerprint: "fp-a2",
	}

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/dep.go", []codeanchor.IntelAnchor{callee1}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/dep2.go", []codeanchor.IntelAnchor{callee2}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/one.go", []codeanchor.IntelAnchor{caller1, caller2}, []codeanchor.IntelEdge{
		{SrcID: "a1", DstID: "b1", Kind: "calls"},
		{SrcID: "a2", DstID: "c1", Kind: "calls"},
	}, nil))

	callees := &CallEdgesRetriever{Store: store, Mode: CallEdgesCallees, Limit: 5}
	results, err := callees.Retrieve(ctx, search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.AnchorHandle("a1")},
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)

	for _, c := range results {
		require.Equal(t, "b1", c.AnchorID, "expected only callees for seeded caller")
	}
}

func TestCallEdgesRetriever_ResolvedFQNDoesNotBroadenToSiblingSymbols(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "resolved-target.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	drain := codeanchor.IntelAnchor{AnchorID: "drain", Lang: codeanchor.LangGo, Kind: "method", Path: "dispatch/queue.go", Symbol: "Drain", FQN: "dispatch.Queue.Drain", Fingerprint: "fp-drain"}
	quarantine := codeanchor.IntelAnchor{AnchorID: "quarantine", Lang: codeanchor.LangGo, Kind: "method", Path: "dispatch/queue.go", Symbol: "Quarantine", FQN: "dispatch.Queue.Quarantine", Fingerprint: "fp-quarantine"}
	other := codeanchor.IntelAnchor{AnchorID: "other", Lang: codeanchor.LangGo, Kind: "method", Path: "dispatch/queue.go", Symbol: "Other", FQN: "dispatch.Queue.Other", Fingerprint: "fp-other"}
	worker := codeanchor.IntelAnchor{AnchorID: "worker", Lang: codeanchor.LangGo, Kind: "method", Path: "dispatch/worker.go", Symbol: "SyncTechnician", FQN: "dispatch.Worker.SyncTechnician", Fingerprint: "fp-worker"}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "dispatch/queue.go", []codeanchor.IntelAnchor{drain, quarantine, other}, []codeanchor.IntelEdge{
		{SrcID: "drain", DstID: "quarantine", Kind: "calls"},
		{SrcID: "quarantine", DstID: "other", Kind: "calls"},
	}, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "dispatch/worker.go", []codeanchor.IntelAnchor{worker}, []codeanchor.IntelEdge{
		{SrcID: "worker", DstID: "drain", Kind: "calls"},
	}, nil))

	spec := search.QuerySpec{
		Text: "callers of dispatch.Queue.Drain compared with dispatch.Queue.Quarantine",
		Seeds: []knowledge.Handle{
			knowledge.AnchorHandle("drain"),
			knowledge.FileHandle("dispatch/queue.go"),
		},
		ResolvedTarget: &search.TargetCandidate{Path: drain.Path, Symbol: drain.Symbol, FQN: drain.FQN},
	}
	callers, err := (&CallEdgesRetriever{Store: store, Mode: CallEdgesCallers, Limit: 10}).Retrieve(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, []string{"worker"}, candidateAnchorIDs(callers), "a sibling callee must not make Drain look self-recursive")

	callees, err := (&CallEdgesRetriever{Store: store, Mode: CallEdgesCallees, Limit: 10}).Retrieve(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, []string{"quarantine"}, candidateAnchorIDs(callees), "sibling functions must not widen exact callee retrieval")
}

func TestCallEdgesRetriever_ResolvedFQNRetainsGenuineRecursion(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "resolved-recursion.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	drain := codeanchor.IntelAnchor{AnchorID: "drain", Lang: codeanchor.LangGo, Kind: "method", Path: "dispatch/queue.go", Symbol: "Drain", FQN: "dispatch.Queue.Drain", Fingerprint: "fp-drain"}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, drain.Path, []codeanchor.IntelAnchor{drain}, []codeanchor.IntelEdge{
		{SrcID: drain.AnchorID, DstID: drain.AnchorID, Kind: "calls"},
	}, nil))

	spec := search.QuerySpec{
		Text:           "callers of Queue.Drain",
		Seeds:          []knowledge.Handle{knowledge.FileHandle(drain.Path)},
		ResolvedTarget: &search.TargetCandidate{Path: drain.Path, Symbol: drain.Symbol, FQN: drain.FQN},
	}
	callers, err := (&CallEdgesRetriever{Store: store, Mode: CallEdgesCallers, Limit: 10}).Retrieve(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, []string{"drain"}, candidateAnchorIDs(callers))
}

func candidateAnchorIDs(candidates []search.Candidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.AnchorID)
	}
	return ids
}
