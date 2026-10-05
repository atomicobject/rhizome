package codeanchor_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func codePublicationFixture(t *testing.T, lang codeanchor.Lang) (context.Context, string, *semdb.Store, *codeanchor.Service) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/audit\n\ngo 1.24\n"), 0644))
	store, err := semdb.Open(filepath.Join(root, "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	var idx codeanchor.LanguageIndexer = codeanchor.NewGoIndexer()
	if lang == codeanchor.LangTS {
		idx = codeanchor.NewTSIndexerWithRoot(root)
	}
	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{idx}, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache(), codeanchor.WithWriteAccess())
	return ctx, root, store, svc
}

func codePublicationRefNames(t *testing.T, ctx context.Context, store *semdb.Store, path string) []string {
	t.Helper()
	refs, err := store.SymbolRefsByPaths(ctx, []string{path})
	require.NoError(t, err)
	names := []string{}
	for _, r := range refs[path] {
		names = append(names, r.DstFQN)
	}
	return names
}
func codePublicationHash(t *testing.T, ctx context.Context, store *semdb.Store, path string) string {
	t.Helper()
	hash, _, _, ok, err := store.FileHash(ctx, path)
	require.NoError(t, err)
	require.True(t, ok)
	return hash
}
func publishCodeFile(t *testing.T, ctx context.Context, svc *codeanchor.Service, lang codeanchor.Lang, path string, content []byte, batch bool) error {
	t.Helper()
	if !batch {
		return svc.IndexCodeFile(ctx, lang, path, content)
	}
	work, err := svc.BuildCodeIndexWork(ctx, lang, path, content)
	if err != nil || work == nil {
		return err
	}
	return svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{*work})
}
func TestIndexCodeFileLateWriteFailureRemainsRetryable(t *testing.T) {
	for _, table := range []string{"intel_symbol_refs", "intel_code_anchors"} {
		for _, batch := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/batch=%v", table, batch), func(t *testing.T) {
				ctx, root, store, svc := codePublicationFixture(t, codeanchor.LangGo)
				path := "source.go"
				abs := filepath.Join(root, path)
				old := []byte("package audit\nfunc OldTarget() {}\nfunc NewTarget() {}\nfunc Caller(){ OldTarget() }\n")
				updated := []byte(strings.ReplaceAll(string(old), "Caller(){ OldTarget() }", "Caller(){ NewTarget() }"))
				require.NoError(t, os.WriteFile(abs, old, 0644))
				require.NoError(t, publishCodeFile(t, ctx, svc, codeanchor.LangGo, abs, old, batch))
				before := codePublicationHash(t, ctx, store, path)
				oldRefs := codePublicationRefNames(t, ctx, store, path)
				require.Contains(t, oldRefs, "example.com/audit.OldTarget")
				_, err := store.DB().Exec("CREATE TRIGGER audit_fail BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'late code write'); END")
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(abs, updated, 0644))
				writeErr := publishCodeFile(t, ctx, svc, codeanchor.LangGo, abs, updated, batch)
				after := codePublicationHash(t, ctx, store, path)
				refs := codePublicationRefNames(t, ctx, store, path)
				t.Logf("initial hash=%s refs=%v failure=%v hashAdvanced=%v refs=%v", before[:12], oldRefs, writeErr, after != before, refs)
				_, err = store.DB().Exec("DROP TRIGGER audit_fail")
				require.NoError(t, err)
				retryErr := publishCodeFile(t, ctx, svc, codeanchor.LangGo, abs, updated, batch)
				require.NoError(t, retryErr)
				retryRefs := codePublicationRefNames(t, ctx, store, path)
				anchors, err := store.IntelAnchorsByPath(ctx, path)
				require.NoError(t, err)
				var moduleHash string
				for _, a := range anchors {
					if a.Kind == "module" {
						moduleHash = a.Fingerprint
					}
				}
				hashNow := codePublicationHash(t, ctx, store, path)
				work, err := svc.BuildCodeIndexWork(ctx, codeanchor.LangGo, abs, updated)
				require.NoError(t, err)
				t.Logf("retry refs=%v moduleMatchesSource=%v nextWorkNil=%v", retryRefs, moduleHash == fmt.Sprintf("%x", sha256.Sum256(updated)), work == nil)
				require.Error(t, writeErr, "failure must be reported")
				require.Equal(t, before, after, "failed publication must not advance freshness")
				require.Contains(t, retryRefs, "example.com/audit.NewTarget")
				require.NotContains(t, retryRefs, "example.com/audit.OldTarget")
				require.Equal(t, hashNow, moduleHash, "live retry must repair Intel rows")
			})
		}
	}
}
func TestIndexCodeFilePersistsExternalEvidence(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%v", batch), func(t *testing.T) {
			ctx, root, store, svc := codePublicationFixture(t, codeanchor.LangTS)
			path := "source.ts"
			abs := filepath.Join(root, path)
			content := []byte("import { useState } from 'react'; export function App(){ return useState(0); }\n")
			require.NoError(t, os.WriteFile(abs, content, 0644))
			require.NoError(t, publishCodeFile(t, ctx, svc, codeanchor.LangTS, abs, content, batch))
			refs := codePublicationRefNames(t, ctx, store, path)
			require.Contains(t, refs, "react.useState")
			classes, err := store.ExternalReferenceClassificationsForPath(ctx, path)
			require.NoError(t, err)
			work, err := svc.BuildCodeIndexWork(ctx, codeanchor.LangTS, abs, content)
			require.NoError(t, err)
			var imports int
			require.NoError(t, store.DB().QueryRow("SELECT COUNT(*) FROM intel_external_import_evidence WHERE src_path=?", path).Scan(&imports))
			t.Logf("refs=%v externalClasses=%d externalImports=%d nextWorkNil=%v", refs, len(classes), imports, work == nil)
			require.NotEmpty(t, classes, "real parser emitted external symbol evidence must survive live publication")
			require.Positive(t, imports, "real parser emitted external import evidence must survive live publication")
		})
	}
}

func TestIndexCodeFileExternalEvidenceReplacesBatchState(t *testing.T) {
	ctx, root, store, svc := codePublicationFixture(t, codeanchor.LangTS)
	path := filepath.Join(root, "source.ts")
	initial := []byte("import { useState } from 'react'; export function App(){ return useState(0); }\n")
	require.NoError(t, publishCodeFile(t, codeanchor.WithBatchIndexing(ctx), svc, codeanchor.LangTS, path, initial, true))
	for _, step := range []struct {
		name, source, target string
		imports              int
	}{
		{"changed binding", "import { useEffect } from 'react'; export function App(){ useEffect(() => {}); }\n", "react.useEffect", 1},
		{"import only", "import 'react';\n", "", 1},
		{"cleared", "export const value = 1;\n", "", 0},
	} {
		t.Run(step.name, func(t *testing.T) {
			require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangTS, path, []byte(step.source)))
			refs := codePublicationRefNames(t, ctx, store, "source.ts")
			require.NotContains(t, refs, "react.useState")
			classes, err := store.ExternalReferenceClassificationsForPath(ctx, "source.ts")
			require.NoError(t, err)
			if step.target != "" {
				require.Contains(t, refs, step.target)
				require.Len(t, classes, 1)
			} else {
				require.Empty(t, classes)
			}
			imports, err := store.ExternalImportEvidenceForPath(ctx, "source.ts")
			require.NoError(t, err)
			require.Len(t, imports, step.imports)
		})
	}
}

func TestIndexCodeFileRepairsHistoricalPoisonedFreshness(t *testing.T) {
	ctx, root, store, _ := codePublicationFixture(t, codeanchor.LangGo)
	idx := &codePublicationCountingIndexer{LanguageIndexer: codeanchor.NewGoIndexer()}
	svc := codeanchor.NewServiceWithOptions(store, []codeanchor.LanguageIndexer{idx}, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	path := filepath.Join(root, "source.go")
	old := []byte("package audit\nfunc OldTarget() {}\nfunc NewTarget() {}\nfunc Caller(){ OldTarget() }\n")
	updated := []byte(strings.ReplaceAll(string(old), "Caller(){ OldTarget() }", "Caller(){ NewTarget() }"))
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, path, old))
	// Older live publication advanced file freshness before the reverse-index
	// write failed, leaving the previous artifact rows behind.
	newHash := fmt.Sprintf("%x", sha256.Sum256(updated))
	_, err := store.DB().Exec("UPDATE files SET hash=?, indexer_version='v1.15.0' WHERE path='source.go'", newHash)
	require.NoError(t, err)
	require.Equal(t, newHash, codePublicationHash(t, ctx, store, "source.go"))
	require.Contains(t, codePublicationRefNames(t, ctx, store, "source.go"), "example.com/audit.OldTarget")
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, path, updated))
	require.EqualValues(t, 2, idx.calls.Load())
	require.Contains(t, codePublicationRefNames(t, ctx, store, "source.go"), "example.com/audit.NewTarget")
	require.NotContains(t, codePublicationRefNames(t, ctx, store, "source.go"), "example.com/audit.OldTarget")
	_, version, _, _, err := store.FileHash(ctx, "source.go")
	require.NoError(t, err)
	require.Equal(t, codeanchor.IndexerVersion, version)
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, path, updated))
	require.EqualValues(t, 2, idx.calls.Load(), "repaired unchanged source must skip parsing")
}
