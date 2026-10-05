package codeanchor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

// Expose scalar artifact capabilities while hiding SQLite's unified batch owner.
type scalarPublicationArtifacts interface {
	ReplaceIntelSymbolRefsForPathsBatch(context.Context, map[string][]codeanchor.SymbolRefRow) error
	ReplaceIntelImportRefsForPathsBatch(context.Context, map[string][]codeanchor.ImportRefRow) error
	ReplaceIntelModuleDefsForPathsBatch(context.Context, map[string][]codeanchor.ModuleDefRow) error
	SymbolRefsByPaths(context.Context, []string) (map[string][]codeanchor.SymbolRefRow, error)
	ImportRefsByPaths(context.Context, []string) (map[string][]codeanchor.ImportRefRow, error)
	ModuleDefsByPaths(context.Context, []string) (map[string][]codeanchor.ModuleDefRow, error)
	ReferrerPathsBySymbolRefs(context.Context, []codeanchor.SymbolRef) (map[string][]string, error)
	ReferrerPathsByModules(context.Context, []string) (map[string][]string, error)
	ReplaceExternalEvidenceForPathsBatch(context.Context, map[string]codeanchor.ExternalEvidenceBatch) error
	ReplaceIntelCodeFile(context.Context, string, []codeanchor.IntelAnchor, []codeanchor.IntelEdge, []codeanchor.IntelFTSRow) error
	ReplaceIntelDocSections(context.Context, string, []codeanchor.IntelDocSection, []codeanchor.IntelEdge, []codeanchor.IntelFTSRow) error
	DeleteIntelByPath(context.Context, string) error
	UpsertIntelCallEdgesForPath(context.Context, string, []codeanchor.IntelEdge) error
	ReplaceRationaleForPath(context.Context, string, []codeanchor.Rationale) error
}

type scalarPublicationStore struct {
	codeanchor.Store
	scalarPublicationArtifacts
}

type scalarPublicationLinker struct{}

func (scalarPublicationLinker) LinksForCode(path string, content []byte, _ codeanchor.FileContext) []codeanchor.DocLink {
	return []codeanchor.DocLink{{SrcType: "code", SrcPath: path, DstKind: "note", DstPath: "note.md", Snippet: string(content)}}
}
func (scalarPublicationLinker) LinksForNote(codeanchor.Note) []codeanchor.DocLink { return nil }

type scalarPublicationTimeoutIndexer struct{}

func (scalarPublicationTimeoutIndexer) Lang() codeanchor.Lang { return codeanchor.LangTS }
func (scalarPublicationTimeoutIndexer) IndexFile(_ []byte, ref paths.CodePathRef) (codeanchor.FileSummary, error) {
	return codeanchor.FileSummary{FilePath: ref.Rel.String(), Lang: codeanchor.LangTS, ParseStatus: codeanchor.ParseTimeout}, nil
}

func TestIndexCodeFileGenericFailureKeepsFreshnessRetryable(t *testing.T) {
	for _, table := range []string{"intel_symbol_refs", "intel_import_refs", "intel_module_defs", "intel_external_symbol_evidence", "intel_code_anchors", "intel_rationale", "doc_links", "files"} {
		t.Run(table, func(t *testing.T) {
			ctx, root, db, _ := codePublicationFixture(t, codeanchor.LangTS)
			store := &scalarPublicationStore{Store: db, scalarPublicationArtifacts: db}
			idx := &codePublicationCountingIndexer{LanguageIndexer: codeanchor.NewTSIndexerWithRoot(root)}
			svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{idx}, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache(), codeanchor.WithCodeDocLinker(scalarPublicationLinker{}))
			helper := []byte("export function local() { return 1; }\n")
			require.NoError(t, os.WriteFile(filepath.Join(root, "helper.ts"), helper, 0o644))
			require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, "helper.ts", helper))
			before := []byte("import { local } from './helper'; import { useState } from 'react';\n// NOTE: before invariant\nexport function Before() { return useState(local()); }\n")
			after := []byte("import { local } from './helper'; import { useEffect } from 'react';\n// NOTE: after invariant\nexport function After() { useEffect(() => local()); }\n")
			require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, "source.ts", before))
			oldHash := codePublicationHash(t, ctx, db, "source.ts")
			require.Contains(t, codePublicationRefNames(t, ctx, db, "source.ts"), "react.useState")
			imports, err := db.ImportRefsByPaths(ctx, []string{"source.ts"})
			require.NoError(t, err)
			require.NotEmpty(t, imports["source.ts"])
			modules, err := db.ModuleDefsByPaths(ctx, []string{"source.ts"})
			require.NoError(t, err)
			require.NotEmpty(t, modules["source.ts"])
			_, err = db.DB().Exec(fmt.Sprintf("CREATE TRIGGER scalar_failure BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT,'scalar family failed'); END", table))
			require.NoError(t, err)
			publisher := svc
			if table == "files" {
				publisher = codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{scalarPublicationTimeoutIndexer{}}, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache(), codeanchor.WithCodeDocLinker(scalarPublicationLinker{}))
			}
			err = publisher.IndexCodeFile(ctx, codeanchor.LangTS, "source.ts", after)
			require.ErrorContains(t, err, "scalar family failed")
			require.Equal(t, oldHash, codePublicationHash(t, ctx, db, "source.ts"), "required family failure must not advance trusted freshness")
			_, version, status, _, err := db.FileHash(ctx, "source.ts")
			require.NoError(t, err)
			require.Equal(t, codeanchor.IndexerVersion, version)
			require.Equal(t, codeanchor.ParseOK, status)
			_, err = db.DB().Exec("DROP TRIGGER scalar_failure")
			require.NoError(t, err)
			require.NoError(t, publisher.IndexCodeFile(ctx, codeanchor.LangTS, "source.ts", after))
			if table == "files" {
				_, _, status, _, err := db.FileHash(ctx, "source.ts")
				require.NoError(t, err)
				require.Equal(t, codeanchor.ParseTimeout, status)
				require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, "source.ts", after))
			}
			refs := codePublicationRefNames(t, ctx, db, "source.ts")
			require.Contains(t, refs, "react.useEffect")
			require.NotContains(t, refs, "react.useState")
			anchors, err := db.IntelAnchorsByPath(ctx, "source.ts")
			require.NoError(t, err)
			var names []string
			for _, anchor := range anchors {
				names = append(names, anchor.Symbol)
			}
			require.Contains(t, names, "After")
			require.NotContains(t, names, "Before")
			rationale, err := db.RationaleForPath(ctx, "source.ts")
			require.NoError(t, err)
			require.Len(t, rationale, 1)
			require.Contains(t, rationale[0].Content, "after invariant")
			links, err := db.DocLinksFromCodePath(ctx, "source.ts", 10)
			require.NoError(t, err)
			require.Len(t, links, 1)
			require.Contains(t, links[0].Snippet, "function After")
			evidence, err := db.ExternalImportEvidenceForPath(ctx, "source.ts")
			require.NoError(t, err)
			require.Len(t, evidence, 1)
			require.Equal(t, "useEffect", evidence[0].Evidence.ImportedName)
			calls := idx.calls.Load()
			require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, "source.ts", after))
			require.Equal(t, calls, idx.calls.Load(), "successful retry restores unchanged admission")
		})
	}
}
