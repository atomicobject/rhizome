package codeanchor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

// intelStubStore makes call-edge rebuilds reach their worker paths.
type intelStubStore struct{ *stubStore }

func (intelStubStore) ReplaceIntelCodeFile(context.Context, string, []IntelAnchor, []IntelEdge, []IntelFTSRow) error {
	return nil
}
func (intelStubStore) ReplaceIntelDocSections(context.Context, string, []IntelDocSection, []IntelEdge, []IntelFTSRow) error {
	return nil
}
func (intelStubStore) DeleteIntelByPath(context.Context, string) error { return nil }
func (intelStubStore) UpsertIntelCallEdgesForPath(context.Context, string, []IntelEdge) error {
	return nil
}

type failingScopeStore struct{ *stubStore }

var errScopeWrite = errors.New("scope write failed")

func (failingScopeStore) SetAnchorScope(context.Context, int64, []string, []string) error {
	return errScopeWrite
}

func funcAnchorStub(caller string) *stubStore {
	ref := SymbolRef{Lang: LangPy, Pkg: "svc.invoice", Name: "charge"}
	return &stubStore{
		callFilesByCallee: map[string][]string{symbolRefKey(ref): {caller}},
		staleCallPaths:    []string{caller},
		anchors:           []Anchor{{ID: 1, Label: "charge", Kind: AnchorFunc, Lang: LangPy, BaseSym: &ref}},
		scopeByAnchor: map[int64]struct {
			syms  []string
			files []string
		}{},
	}
}

func TestRecomputeAnchorScopes_CancelledCallEdgeRebuildIsRetried(t *testing.T) {
	base := funcAnchorStub(string(paths.NormalizeCode("svc/caller.py")))
	svc := NewServiceWithOptions(intelStubStore{base}, nil, WithoutWarmCache(), WithWriteAccess())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = svc.RecomputeAnchorScopes(ctx)
	require.False(t, svc.callEdgesRebuilt, "a cancelled rebuild must not count as done")

	require.NoError(t, svc.RecomputeAnchorScopes(context.Background()))
	require.Equal(t, 2, base.staleCallCalls, "the next recompute retries the catch-up rebuild")
}

func TestRebuildCallEdgesForPaths_ParallelCancelledDoesNotReportSuccess(t *testing.T) {
	var files []string
	for i := 0; i < 8; i++ {
		files = append(files, string(paths.NormalizeCode(fmt.Sprintf("svc/caller%d.py", i))))
	}
	svc := NewServiceWithOptions(intelStubStore{funcAnchorStub(files[0])}, nil, WithoutWarmCache(), WithWriteAccess())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, svc.RebuildCallEdgesForPaths(ctx, files), context.Canceled)
	require.False(t, svc.callEdgesRebuilt)
}

func TestRecomputeAnchorScopes_FailureAfterOverflowKeepsFullRecomputePending(t *testing.T) {
	base := funcAnchorStub(string(paths.NormalizeCode("svc/caller.py")))
	svc := NewServiceWithOptions(failingScopeStore{base}, nil, WithoutWarmCache(), WithWriteAccess())
	svc.callEdgesRebuilt = true

	svc.dirtyMu.Lock()
	svc.dirtyOverflow = true
	svc.dirtyMu.Unlock()

	require.ErrorIs(t, svc.RecomputeAnchorScopes(context.Background()), errScopeWrite)
	require.True(t, svc.HasDirtyAnchors(), "a failed recompute must keep the full-recompute obligation")
	overflow, _ := svc.DirtyAnchorIDsSnapshot()
	require.True(t, overflow)
}

// goPhaseFailingStore lets the parser call-edge phase finish (empty
// reverse-index rows) and fails the Go package relationship phase after it.
type goPhaseFailingStore struct{ intelStubStore }

var errGoPhase = errors.New("go relationship phase failed")

func (goPhaseFailingStore) ReplaceIntelSymbolRefsForPathsBatch(context.Context, map[string][]SymbolRefRow) error {
	return nil
}
func (goPhaseFailingStore) ReplaceIntelImportRefsForPathsBatch(context.Context, map[string][]ImportRefRow) error {
	return nil
}
func (goPhaseFailingStore) ReplaceIntelModuleDefsForPathsBatch(context.Context, map[string][]ModuleDefRow) error {
	return nil
}
func (goPhaseFailingStore) SymbolRefsByPaths(context.Context, []string) (map[string][]SymbolRefRow, error) {
	return nil, nil
}
func (goPhaseFailingStore) ImportRefsByPaths(context.Context, []string) (map[string][]ImportRefRow, error) {
	return nil, nil
}
func (goPhaseFailingStore) ModuleDefsByPaths(context.Context, []string) (map[string][]ModuleDefRow, error) {
	return nil, nil
}
func (goPhaseFailingStore) ReferrerPathsBySymbolRefs(context.Context, []SymbolRef) (map[string][]string, error) {
	return nil, nil
}
func (goPhaseFailingStore) ReferrerPathsByModules(context.Context, []string) (map[string][]string, error) {
	return nil, nil
}
func (goPhaseFailingStore) IntelAnchorsByPath(context.Context, string) ([]IntelAnchor, error) {
	return nil, nil
}
func (goPhaseFailingStore) IntelAnchorsByPaths(context.Context, []string) (map[string][]IntelAnchor, error) {
	return nil, nil
}
func (goPhaseFailingStore) IntelAnchorsBySymbol(context.Context, string, int) ([]IntelAnchor, error) {
	return nil, nil
}
func (goPhaseFailingStore) GoPackageSnapshotsForPaths(context.Context, []string) ([]GoPackageSnapshot, error) {
	return nil, errGoPhase
}
func (goPhaseFailingStore) ReplaceGoPackageRelationships(context.Context, []GoPackageRelationshipReplacement) error {
	return nil
}

func TestRebuildCallEdges_FailedGoRelationshipPhaseIsNotDone(t *testing.T) {
	caller := string(paths.NormalizeCode("svc/caller.py"))
	svc := NewServiceWithOptions(goPhaseFailingStore{intelStubStore{funcAnchorStub(caller)}}, nil, WithoutWarmCache(), WithWriteAccess())

	require.ErrorIs(t, svc.RebuildCallEdgesForPaths(context.Background(), []string{caller}), errGoPhase)
	require.False(t, svc.callEdgesRebuilt, "catch-up is done only when both rebuild phases succeed")
	require.ErrorIs(t, svc.RebuildAllCallEdges(context.Background()), errGoPhase)
	require.False(t, svc.callEdgesRebuilt, "catch-up is done only when both rebuild phases succeed")
}
