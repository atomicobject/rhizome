//go:build cgo

package codeanchor_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

type referrerLookupProgress struct{ events []string }

func (p *referrerLookupProgress) Start(total int) {
	p.events = append(p.events, fmt.Sprintf("start:%d", total))
}

func (p *referrerLookupProgress) Advance(delta int) {
	p.events = append(p.events, fmt.Sprintf("advance:%d", delta))
}

func TestService_ReferrerBatchesPreserveProgressAndErrors(t *testing.T) {
	for _, kind := range []string{"symbol", "module"} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fail=%t", kind, fail), func(t *testing.T) {
				inner, err := sqlite.Open(filepath.Join(t.TempDir(), "referrers.db"))
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, inner.Close()) })
				store := &countingRefStore{Store: inner}
				count := 129
				if fail {
					count = 257
					store.failLookupAt = 2
					store.lookupError = errors.New("second lookup failed")
				}
				refs := make(map[string][]codeanchor.SymbolRefRow)
				imports := make(map[string][]codeanchor.ImportRefRow)
				var deltas codeanchor.DefDeltas
				var expectedPaths []string
				for i := 0; i < count; i++ {
					caller := fmt.Sprintf("src/caller_%03d.py", i)
					name := fmt.Sprintf("Func%03d", i)
					module := fmt.Sprintf("app.module%03d", i)
					expectedPaths = append(expectedPaths, caller)
					if kind == "symbol" {
						refs[caller] = []codeanchor.SymbolRefRow{{SrcPath: caller, RefKind: codeanchor.RefKindCalls, DstLang: codeanchor.LangPy, DstPkg: "app.core", DstName: name, DstFQN: "app.core." + name}}
						deltas.AddedSymbols = append(deltas.AddedSymbols, codeanchor.SymbolRef{Lang: codeanchor.LangPy, Pkg: "app.core", Name: name})
					} else {
						imports[caller] = []codeanchor.ImportRefRow{{SrcPath: caller, Module: module}}
						deltas.AddedModules = append(deltas.AddedModules, module)
					}
				}
				if kind == "symbol" {
					deltas.AddedSymbols = append(deltas.AddedSymbols, deltas.AddedSymbols[0], codeanchor.SymbolRef{})
				} else {
					deltas.AddedModules = append(deltas.AddedModules, " "+deltas.AddedModules[0]+" ", "")
				}
				ctx := context.Background()
				require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, refs))
				require.NoError(t, store.ReplaceIntelImportRefsForPathsBatch(ctx, imports))
				progress := &referrerLookupProgress{}
				ctx = codeanchor.WithCallEdgePlanProgress(ctx, progress)
				svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithWriteAccess())
				plan, err := svc.PlanCallEdgeRebuildIncremental(ctx, nil, deltas, codeanchor.CallEdgeResidual{}, true)
				if fail {
					require.ErrorIs(t, err, store.lookupError)
					require.Empty(t, plan.Paths, "a partial lookup must not publish a partial rebuild plan")
					require.Equal(t, []string{"start:3", "advance:1"}, progress.events)
				} else {
					require.NoError(t, err)
					require.Equal(t, expectedPaths, plan.Paths)
					require.Equal(t, count, plan.Summary.ImpactedCallers)
					require.Equal(t, []string{"start:2", "advance:1", "advance:1"}, progress.events)
				}
				if kind == "symbol" {
					require.EqualValues(t, 2, store.symbolCalls.Load())
					require.Zero(t, store.moduleCalls.Load())
				} else {
					require.EqualValues(t, 2, store.moduleCalls.Load())
					require.Zero(t, store.symbolCalls.Load())
				}
			})
		}
	}
}
