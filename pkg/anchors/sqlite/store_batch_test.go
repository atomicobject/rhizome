package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func TestReplaceFileSummariesBatch_ReplacesTargetFilesOnly(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "replace-batch.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	aPath := "src/a.py"
	bPath := "src/b.py"

	aOld := codeanchor.FileSummary{
		FilePath: aPath,
		Lang:     codeanchor.LangPy,
		Hash:     "a-old",
		Symbols: []codeanchor.Symbol{
			pySymbol(aPath, "pkg", "A"),
		},
		Annotations: []codeanchor.AnnotationUse{
			{
				OwnerFQN: "pkg.A",
				AnnSymbol: codeanchor.SymbolRef{
					Lang: codeanchor.LangPy,
					Pkg:  "decorators",
					Name: "track",
				},
				Args: map[string]string{"name": "a"},
			},
		},
	}
	bOld := codeanchor.FileSummary{
		FilePath: bPath,
		Lang:     codeanchor.LangPy,
		Hash:     "b-old",
		Symbols: []codeanchor.Symbol{
			pySymbol(bPath, "pkg", "B"),
		},
		Supers: []codeanchor.SuperEdge{
			{ChildFQN: "pkg.B", ParentFQN: "pkg.A"},
		},
	}
	require.NoError(t, store.ReplaceFileSummariesBatch(ctx, []codeanchor.FileSummary{aOld, bOld}))

	aNew := codeanchor.FileSummary{
		FilePath: aPath,
		Lang:     codeanchor.LangPy,
		Hash:     "a-new",
		Symbols: []codeanchor.Symbol{
			pySymbol(aPath, "pkg", "A2"),
		},
	}
	require.NoError(t, store.ReplaceFileSummariesBatch(ctx, []codeanchor.FileSummary{aNew}))

	symbols := selectStrings(t, store, ctx, `SELECT fqn FROM symbols ORDER BY fqn`)
	require.Equal(t, []string{"pkg.A2", "pkg.B"}, symbols)

	aFileSymbols := selectStrings(t, store, ctx, `SELECT fqn FROM symbols WHERE file = ? ORDER BY fqn`, aPath)
	require.Equal(t, []string{"pkg.A2"}, aFileSymbols)

	bFileSymbols := selectStrings(t, store, ctx, `SELECT fqn FROM symbols WHERE file = ? ORDER BY fqn`, bPath)
	require.Equal(t, []string{"pkg.B"}, bFileSymbols)

	var superCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM super_edges`).Scan(&superCount))
	require.Equal(t, 0, superCount)

	var annotationCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM annotations`).Scan(&annotationCount))
	require.Equal(t, 0, annotationCount)
}

func TestReplaceFileSummariesBatch_DuplicatePathUsesLastSummary(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "replace-batch-dedup.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	path := "src/dup.py"
	first := codeanchor.FileSummary{
		FilePath: path,
		Lang:     codeanchor.LangPy,
		Hash:     "first",
		Symbols: []codeanchor.Symbol{
			pySymbol(path, "pkg", "First"),
		},
	}
	last := codeanchor.FileSummary{
		FilePath: path,
		Lang:     codeanchor.LangPy,
		Hash:     "last",
		Symbols: []codeanchor.Symbol{
			pySymbol(path, "pkg", "Last"),
		},
	}
	require.NoError(t, store.ReplaceFileSummariesBatch(ctx, []codeanchor.FileSummary{first, last}))

	symbols := selectStrings(t, store, ctx, `SELECT fqn FROM symbols ORDER BY fqn`)
	require.Equal(t, []string{"pkg.Last"}, symbols)

	fileSymbols := selectStrings(t, store, ctx, `SELECT fqn FROM symbols WHERE file = ? ORDER BY fqn`, path)
	require.Equal(t, []string{"pkg.Last"}, fileSymbols)
}

func TestApplyCodePersistenceBatch_PersistsAllCodeArtifacts(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "code-persist-batch.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	targetSummary := codeanchor.FileSummary{
		FilePath: "src/target.cs",
		Lang:     codeanchor.LangCs,
		Hash:     "target-hash",
		Symbols: []codeanchor.Symbol{
			{
				Lang: codeanchor.LangCs,
				Kind: codeanchor.SymClass,
				File: "src/target.cs",
				Pkg:  "pkg",
				Name: "Target",
				FQN:  "pkg.Target",
			},
		},
	}
	sourceSummary := codeanchor.FileSummary{
		FilePath: "src/source.cs",
		Lang:     codeanchor.LangCs,
		Hash:     "source-hash",
		Symbols: []codeanchor.Symbol{
			{
				Lang: codeanchor.LangCs,
				Kind: codeanchor.SymClass,
				File: "src/source.cs",
				Pkg:  "pkg",
				Name: "Source",
				FQN:  "pkg.Source",
			},
		},
	}

	err = store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{
		Summaries: []codeanchor.FileSummary{targetSummary, sourceSummary},
		Metas: []codeanchor.FileMeta{
			{
				Path:        "src/errored.cs",
				Lang:        codeanchor.LangCs,
				Hash:        "errored-hash",
				ParseStatus: codeanchor.ParseErrored,
			},
		},
		SymbolRefBatches: map[string][]codeanchor.SymbolRefRow{
			"src/source.cs": {
				{
					OwnerFQN: "pkg.Source",
					RefKind:  codeanchor.RefKindTypeRef,
					DstLang:  codeanchor.LangCs,
					DstPkg:   "pkg",
					DstName:  "Target",
					DstFQN:   "pkg.Target",
				},
			},
		},
		ImportRefBatches: map[string][]codeanchor.ImportRefRow{
			"src/source.cs": {{Module: "pkg"}},
		},
		ModuleDefBatches: map[string][]codeanchor.ModuleDefRow{
			"src/source.cs": {{Lang: codeanchor.LangCs, Module: "pkg"}},
		},
		IntelReps: []codeanchor.IntelCodeFileReplace{
			{
				Path: "src/source.cs",
				Anchors: []codeanchor.IntelAnchor{
					{
						AnchorID:    "anchor:pkg.Source",
						Lang:        codeanchor.LangCs,
						Kind:        "symbol",
						Path:        "src/source.cs",
						Symbol:      "Source",
						FQN:         "pkg.Source",
						StartLine:   1,
						EndLine:     3,
						Fingerprint: "fp-source",
					},
				},
			},
		},
		RationaleBatches: []codeanchor.RationaleBatch{
			{
				Path: "src/errored.cs",
				Rationales: []codeanchor.Rationale{
					{
						ID:          "todo-1",
						Path:        "src/errored.cs",
						Kind:        codeanchor.RationaleTodo,
						Content:     "keep this rationale",
						StartLine:   1,
						EndLine:     1,
						Fingerprint: "todo-fp",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	symbols := selectStrings(t, store, ctx, `SELECT fqn FROM symbols ORDER BY fqn`)
	require.Equal(t, []string{"pkg.Source", "pkg.Target"}, symbols)

	var parseStatus string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT parse_status FROM files WHERE path = 'src/errored.cs'`).Scan(&parseStatus))
	require.Equal(t, string(codeanchor.ParseErrored), parseStatus)

	var symbolRefCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_symbol_refs`).Scan(&symbolRefCount))
	require.Equal(t, 1, symbolRefCount)

	var importRefCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_import_refs`).Scan(&importRefCount))
	require.Equal(t, 1, importRefCount)

	var moduleDefCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_module_defs`).Scan(&moduleDefCount))
	require.Equal(t, 1, moduleDefCount)

	var anchorCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_code_anchors`).Scan(&anchorCount))
	require.Equal(t, 1, anchorCount)

	var rationaleCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_rationale`).Scan(&rationaleCount))
	require.Equal(t, 1, rationaleCount)
}

func TestApplyCodePersistenceBatch_PreservesIntelEdgesFTSAndReadOnlyCallLookup(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "code-persist-intel-batch.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	err = store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{
		Summaries: []codeanchor.FileSummary{
			{
				FilePath: "src/callee.py",
				Lang:     codeanchor.LangPy,
				Hash:     "callee-hash",
				Symbols: []codeanchor.Symbol{
					{
						Lang: codeanchor.LangPy,
						Kind: codeanchor.SymFunc,
						File: "src/callee.py",
						Pkg:  "pkg.callee",
						Name: "run",
						FQN:  "pkg.callee.run",
					},
				},
			},
			{
				FilePath: "src/caller.py",
				Lang:     codeanchor.LangPy,
				Hash:     "caller-hash",
				Symbols: []codeanchor.Symbol{
					{
						Lang: codeanchor.LangPy,
						Kind: codeanchor.SymFunc,
						File: "src/caller.py",
						Pkg:  "pkg.caller",
						Name: "invoke",
						FQN:  "pkg.caller.invoke",
					},
				},
			},
		},
		IntelReps: []codeanchor.IntelCodeFileReplace{
			{
				Path: "src/callee.py",
				Anchors: []codeanchor.IntelAnchor{{
					AnchorID:    "anchor:pkg.callee.run",
					Lang:        codeanchor.LangPy,
					Kind:        "symbol",
					Path:        "src/callee.py",
					Symbol:      "run",
					FQN:         "pkg.callee.run",
					Fingerprint: "fp-callee",
				}},
				FTSRows: []codeanchor.IntelFTSRow{{
					ItemType: "anchor",
					ItemID:   "anchor:pkg.callee.run",
					Path:     "src/callee.py",
					Title:    "run",
					Body:     "def run(): pass",
				}},
			},
			{
				Path: "src/caller.py",
				Anchors: []codeanchor.IntelAnchor{{
					AnchorID:    "anchor:pkg.caller.invoke",
					Lang:        codeanchor.LangPy,
					Kind:        "symbol",
					Path:        "src/caller.py",
					Symbol:      "invoke",
					FQN:         "pkg.caller.invoke",
					Fingerprint: "fp-caller",
				}},
				Edges: []codeanchor.IntelEdge{{
					SrcID: "anchor:pkg.caller.invoke",
					DstID: "anchor:pkg.callee.run",
					Kind:  "calls",
				}},
				FTSRows: []codeanchor.IntelFTSRow{{
					ItemType: "anchor",
					ItemID:   "anchor:pkg.caller.invoke",
					Path:     "src/caller.py",
					Title:    "invoke",
					Body:     "def invoke(): return run()",
				}},
			},
		},
	})
	require.NoError(t, err)

	var edgeCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_edges WHERE kind = 'calls'`).Scan(&edgeCount))
	require.Equal(t, 1, edgeCount)

	var ftsCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts WHERE item_type = 'anchor'`).Scan(&ftsCount))
	require.Equal(t, 2, ftsCount)

	var ftsRowIDCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_fts_rowid`).Scan(&ftsRowIDCount))
	require.Equal(t, 2, ftsRowIDCount)

	pathsByRef, err := store.CallFilesByCallees(ctx, []codeanchor.SymbolRef{{
		Lang: codeanchor.LangPy,
		Pkg:  "pkg.callee",
		Name: "run",
	}})
	require.NoError(t, err)
	require.Equal(t, []string{"src/caller.py"}, pathsByRef["py|pkg.callee|run"])
}

func TestApplyCodePersistenceBatch_LargeBatchPersistsEveryPath(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	store, err := Open(currentSchemaTestDBPath(t, "code-persist-chunks.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	intelReps := make([]codeanchor.IntelCodeFileReplace, 0, 130)
	rationaleBatches := make([]codeanchor.RationaleBatch, 0, 130)
	for i := 0; i < 130; i++ {
		path := filepath.ToSlash(filepath.Join("src", "pkg", fmt.Sprintf("file_%03d.py", i)))
		anchorID := fmt.Sprintf("anchor:file_%03d", i)
		intelReps = append(intelReps, codeanchor.IntelCodeFileReplace{
			Path: path,
			Anchors: []codeanchor.IntelAnchor{{
				AnchorID:    anchorID,
				Lang:        codeanchor.LangPy,
				Kind:        "symbol",
				Path:        path,
				Symbol:      fmt.Sprintf("run_%03d", i),
				FQN:         fmt.Sprintf("pkg.file_%03d.run", i),
				Fingerprint: fmt.Sprintf("fp-%03d", i),
			}},
		})
		rationaleBatches = append(rationaleBatches, codeanchor.RationaleBatch{
			Path: path,
			Rationales: []codeanchor.Rationale{{
				ID:          fmt.Sprintf("todo-%03d", i),
				Path:        path,
				Kind:        codeanchor.RationaleTodo,
				Content:     strings.Repeat("chunk me ", 32),
				StartLine:   1,
				EndLine:     1,
				Fingerprint: fmt.Sprintf("todo-fp-%03d", i),
			}},
		})
	}

	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{
		IntelReps:        intelReps,
		RationaleBatches: rationaleBatches,
	}))

	var anchorCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_code_anchors`).Scan(&anchorCount))
	require.Equal(t, 130, anchorCount)

	var rationaleCount int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_rationale`).Scan(&rationaleCount))
	require.Equal(t, 130, rationaleCount)

	summary := collector.RenderSummary()
	require.Contains(t, summary, "intel.replace_code_file")
}

func TestApplyCodePersistenceBatch_ResolvesExternalEvidenceAfterDurableRefs(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	store, err := Open(currentSchemaTestDBPath(t, "code-persist-external.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{
		Ecosystem: codeanchor.ExternalEcosystemNPM, Module: "react", SymbolPath: "useState", Kind: codeanchor.ExternalTargetSymbol,
	})
	require.NoError(t, err)
	path := "src/component.tsx"
	row := codeanchor.SymbolRefRow{
		SrcPath: path, OwnerFQN: "src.component.Counter", RefKind: codeanchor.RefKindCalls,
		DstLang: codeanchor.LangTS, DstPkg: "react", DstName: "useState", DstFQN: "react.useState",
	}
	typeRow := row
	typeRow.RefKind = codeanchor.RefKindTypeRef
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{
		SymbolRefBatches: map[string][]codeanchor.SymbolRefRow{path: {row, typeRow}},
		ExternalEvidenceBatches: map[string]codeanchor.ExternalEvidenceBatch{path: {
			Symbols: []codeanchor.ExternalSymbolEvidenceInput{
				{
					OwnerFQN: row.OwnerFQN, RefKind: row.RefKind,
					Raw:      codeanchor.RawSymbolTargetKey{DstLang: row.DstLang, DstPkg: row.DstPkg, DstName: row.DstName, DstFQN: row.DstFQN},
					Target:   target,
					Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh, ImportedName: "useState", LocalName: "useState"},
				},
				{
					OwnerFQN: typeRow.OwnerFQN, RefKind: typeRow.RefKind,
					Raw:      codeanchor.RawSymbolTargetKey{DstLang: typeRow.DstLang, DstPkg: typeRow.DstPkg, DstName: typeRow.DstName, DstFQN: typeRow.DstFQN},
					Target:   target,
					Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh, ImportedName: "useState", LocalName: "useState"},
				},
			},
		}},
	}))

	classes, err := store.ExternalReferenceClassificationsForPath(ctx, path)
	require.NoError(t, err)
	require.Len(t, classes, 2)
	for _, class := range classes {
		require.Equal(t, codeanchor.ExternalClassExternalClassified, class.Class)
		require.Equal(t, target.Handle, class.Target.Handle)
	}

	// An empty replacement is a durable statement that the path no longer has
	// qualifying evidence; it clears associations and prunes the shared catalog.
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{
		SymbolRefBatches:        map[string][]codeanchor.SymbolRefRow{path: {row, typeRow}},
		ExternalEvidenceBatches: map[string]codeanchor.ExternalEvidenceBatch{path: {}},
	}))
	classes, err = store.ExternalReferenceClassificationsForPath(ctx, path)
	require.NoError(t, err)
	require.Empty(t, classes)
	var catalog int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_external_targets`).Scan(&catalog))
	require.Zero(t, catalog)
}

func TestApplyCodePersistenceBatch_RollsBackRefsWhenExternalEvidenceIsUnmatched(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "code-persist-external-rollback.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{
		Ecosystem: codeanchor.ExternalEcosystemNPM, Module: "react", SymbolPath: "useState", Kind: codeanchor.ExternalTargetSymbol,
	})
	require.NoError(t, err)

	err = store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{
		SymbolRefBatches: map[string][]codeanchor.SymbolRefRow{
			"src/component.tsx": {{SrcPath: "src/component.tsx", OwnerFQN: "src.component.Counter", RefKind: codeanchor.RefKindCalls, DstLang: codeanchor.LangTS, DstPkg: "react", DstName: "useState", DstFQN: "react.useState"}},
		},
		ExternalEvidenceBatches: map[string]codeanchor.ExternalEvidenceBatch{
			"src/component.tsx": {Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
				OwnerFQN: "src.component.Other", RefKind: codeanchor.RefKindCalls,
				Raw:    codeanchor.RawSymbolTargetKey{DstLang: codeanchor.LangTS, DstPkg: "react", DstName: "useState", DstFQN: "react.useState"},
				Target: target, Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMNamed, Confidence: codeanchor.ExternalConfidenceHigh},
			}}},
		},
	})
	require.ErrorContains(t, err, "unmatched durable symbol-reference associations")
	var refs int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_symbol_refs`).Scan(&refs))
	require.Zero(t, refs)
}

func TestExternalEvidenceFullRebuildAndIncrementalReplacementConverge(t *testing.T) {
	ctx := context.Background()
	open := func(path string) *Store {
		store, err := Open(path)
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
		return store
	}
	incremental := open(filepath.Join(t.TempDir(), "incremental.db"))
	rebuiltPath := filepath.Join(t.TempDir(), "rebuilt.db")
	rebuilt := open(rebuiltPath)

	reactModule, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{
		Ecosystem: codeanchor.ExternalEcosystemNPM, Module: "react", Kind: codeanchor.ExternalTargetModule,
	})
	require.NoError(t, err)
	useState := mustExternalTarget(t, "react", "useState")
	legacy := mustExternalTarget(t, "legacy-package", "oldAPI")
	deleted := mustExternalTarget(t, "deleted-package", "gone")

	batchFor := func(path, owner string, target codeanchor.ExternalTarget, evidenceKind codeanchor.ExternalEvidenceKind) codeanchor.CodePersistenceBatch {
		name := target.SymbolPath
		row := codeanchor.SymbolRefRow{
			SrcPath: path, OwnerFQN: owner, RefKind: codeanchor.RefKindCalls,
			DstLang: codeanchor.LangTS, DstPkg: target.Module, DstName: name, DstFQN: target.Module + "." + name,
		}
		return codeanchor.CodePersistenceBatch{
			SymbolRefBatches: map[string][]codeanchor.SymbolRefRow{path: {row}},
			ExternalEvidenceBatches: map[string]codeanchor.ExternalEvidenceBatch{
				path: {Symbols: []codeanchor.ExternalSymbolEvidenceInput{{
					OwnerFQN: owner, RefKind: row.RefKind,
					Raw:    codeanchor.RawSymbolTargetKey{DstLang: row.DstLang, DstPkg: row.DstPkg, DstName: row.DstName, DstFQN: row.DstFQN},
					Target: target, Evidence: codeanchor.ExternalEvidence{Kind: evidenceKind, Confidence: codeanchor.ExternalConfidenceHigh},
				}}},
			},
		}
	}
	finalBatch := func() codeanchor.CodePersistenceBatch {
		batch := batchFor("src/app.tsx", "src.app.App", useState, codeanchor.ExternalEvidenceESMNamed)
		batch.ExternalEvidenceBatches["src/app.tsx"] = codeanchor.ExternalEvidenceBatch{
			Symbols: batch.ExternalEvidenceBatches["src/app.tsx"].Symbols,
			Imports: []codeanchor.ExternalImportEvidenceInput{{
				Module: "react", BindingOrdinal: 0, Target: reactModule,
				Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMSideEffect, Confidence: codeanchor.ExternalConfidenceHigh},
			}},
		}
		return batch
	}
	seedStale := func(store *Store) {
		require.NoError(t, store.ApplyCodePersistenceBatch(ctx, batchFor("src/app.tsx", "src.app.App", legacy, codeanchor.ExternalEvidenceESMNamed)))
		require.NoError(t, store.ApplyCodePersistenceBatch(ctx, batchFor("src/deleted.ts", "", deleted, codeanchor.ExternalEvidenceESMNamed)))
	}
	seedStale(incremental)
	seedStale(rebuilt)

	// Incremental indexing replaces the surviving source and deletes the absent
	// source. Full rebuild clears the domain and publishes the same final source.
	require.NoError(t, incremental.ApplyCodePersistenceBatch(ctx, finalBatch()))
	require.NoError(t, incremental.DeleteFile(ctx, "src/deleted.ts"))
	require.NoError(t, rebuilt.ResetDomain(ctx))
	require.NoError(t, rebuilt.Close())
	rebuilt = open(rebuiltPath)
	require.NoError(t, rebuilt.ApplyCodePersistenceBatch(ctx, finalBatch()))

	snapshot := func(store *Store) []string {
		var rows []string
		rows = append(rows, selectStrings(t, store, ctx, `SELECT 'target|' || handle FROM intel_external_targets ORDER BY handle`)...)
		rows = append(rows, selectStrings(t, store, ctx, `SELECT 'map|' || r.dst_lang || '|' || r.dst_pkg || '|' || r.dst_name || '|' || r.dst_fqn || '|' || e.handle
			FROM intel_external_target_map m JOIN intel_symbol_ref_targets r ON r.target_id=m.raw_target_id
			JOIN intel_external_targets e ON e.external_id=m.external_id ORDER BY 1`)...)
		rows = append(rows, selectStrings(t, store, ctx, `SELECT 'symbol|' || f.path || '|' || s.owner_fqn || '|' || s.ref_kind || '|' || r.dst_fqn || '|' || e.handle || '|' || s.evidence_kind
			FROM intel_external_symbol_evidence s JOIN intel_symbol_ref_files f ON f.file_id=s.src_file_id
			JOIN intel_symbol_ref_targets r ON r.target_id=s.raw_target_id JOIN intel_external_targets e ON e.external_id=s.external_id ORDER BY 1`)...)
		rows = append(rows, selectStrings(t, store, ctx, `SELECT 'import|' || i.src_path || '|' || i.module || '|' || i.binding_ordinal || '|' || e.handle || '|' || i.evidence_kind
			FROM intel_external_import_evidence i JOIN intel_external_targets e ON e.external_id=i.external_id ORDER BY 1`)...)
		return rows
	}
	incrementalRows := snapshot(incremental)
	require.Equal(t, snapshot(rebuilt), incrementalRows)
	require.Len(t, incrementalRows, 5, "two targets, one raw mapping, one symbol association, and one import association")
	for _, row := range incrementalRows {
		require.NotContains(t, row, "legacy-package")
		require.NotContains(t, row, "deleted-package")
	}
}

func pySymbol(file, pkg, name string) codeanchor.Symbol {
	return codeanchor.Symbol{
		Lang: codeanchor.LangPy,
		Kind: codeanchor.SymClass,
		File: file,
		Pkg:  pkg,
		Name: name,
		FQN:  pkg + "." + name,
	}
}

func selectStrings(t *testing.T, store *Store, ctx context.Context, query string, args ...any) []string {
	t.Helper()

	rows, err := store.db.QueryContext(ctx, query, args...)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var value string
		require.NoError(t, rows.Scan(&value))
		out = append(out, value)
	}
	require.NoError(t, rows.Err())
	return out
}
