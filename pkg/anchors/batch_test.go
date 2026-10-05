package codeanchor_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/stretchr/testify/require"
)

type failingIntelDocBatchStore struct {
	*semdb.Store
	err error
}

type countingCodeBatchStore struct {
	*semdb.Store
	summaryBatchCalls atomic.Int64
	fileMetaCalls     atomic.Int64
	persistenceCalls  atomic.Int64
}

type testNoteSource struct {
	path    string
	format  noteformat.FormatID
	content string
	hash    string
	mtime   int64
}

func (s testNoteSource) NotePathString() string { return s.path }
func (s testNoteSource) NoteFormatID() noteformat.FormatID {
	if s.format == "" {
		return noteformat.FormatID("markdown")
	}
	return s.format
}
func (s testNoteSource) NoteContentString() string     { return s.content }
func (s testNoteSource) NoteContentHashString() string { return s.hash }
func (s testNoteSource) NoteMtimeUnix() int64          { return s.mtime }

func (s *failingIntelDocBatchStore) ReplaceIntelDocSectionsBatch(ctx context.Context, reps []codeanchor.IntelDocReplace) error {
	if s.err != nil {
		return s.err
	}
	return s.Store.ReplaceIntelDocSectionsBatch(ctx, reps)
}

func (s *countingCodeBatchStore) ReplaceFileSummariesBatch(ctx context.Context, summaries []codeanchor.FileSummary) error {
	s.summaryBatchCalls.Add(1)
	return s.Store.ReplaceFileSummariesBatch(ctx, summaries)
}

func (s *countingCodeBatchStore) UpsertFileMeta(ctx context.Context, meta codeanchor.FileMeta) error {
	s.fileMetaCalls.Add(1)
	return s.Store.UpsertFileMeta(ctx, meta)
}

func (s *countingCodeBatchStore) ApplyCodePersistenceBatch(ctx context.Context, batch codeanchor.CodePersistenceBatch) error {
	s.persistenceCalls.Add(1)
	return s.Store.ApplyCodePersistenceBatch(ctx, batch)
}

func TestApplyNoteIndexBatch_DoesNotAdvanceMetadataOnIntelWriteFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := currentSchemaAnchorTestDBPath(t, filepath.Join(root, "batch-note-meta.db"))
	inner, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = inner.Close() })

	store := &failingIntelDocBatchStore{
		Store: inner,
		err:   errors.New("intel-doc-batch-failure"),
	}
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())

	notePath := filepath.Join(root, "note.md")
	content := "# Note\n\nbatch metadata ordering regression test\n"
	require.NoError(t, os.WriteFile(notePath, []byte(content), 0o644))

	work, err := svc.BuildNoteIndexWorkFromSource(ctx, testNoteSource{
		path:    notePath,
		content: content,
		hash:    fmt.Sprintf("%x", sha256.Sum256([]byte(content))),
		mtime:   123,
	})
	require.NoError(t, err)
	require.NotNil(t, work)
	work.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	work.Mtime = 123

	err = svc.ApplyNoteIndexBatch(ctx, []codeanchor.NoteIndexWork{*work})
	require.Error(t, err)
	require.Contains(t, err.Error(), "intel-doc-batch-failure")

	meta, err := inner.IntelNoteIndexMeta(ctx)
	require.NoError(t, err)
	if got, ok := meta["note.md"]; ok {
		require.Empty(t, got.ContentHash)
		require.Empty(t, got.IndexerVersion)
		require.Zero(t, got.Mtime)
	}
}

func TestBuildNoteIndexWorkUsesAnchorDeclarationExtraction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := currentSchemaAnchorTestDBPath(t, filepath.Join(root, "batch-note-source.db"))
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	notePath := filepath.Join(root, "architecture.md")
	content := `---
title: "Architecture"
code-anchors:
  go:
    - label: ontology-node-id
      symbol: github.com/atomicobject/rhizome/pkg/ontology.OntologyNodeID
---
# Architecture

The note body still produces intel sections.
`
	require.NoError(t, os.WriteFile(notePath, []byte(content), 0o644))

	work, err := svc.BuildNoteIndexWorkFromSource(ctx, testNoteSource{
		path:    notePath,
		content: content,
		hash:    fmt.Sprintf("%x", sha256.Sum256([]byte(content))),
		mtime:   123,
	})

	require.NoError(t, err)
	require.NotNil(t, work)
	require.Equal(t, "architecture.md", work.Path)
	require.Equal(t, []string{"ontology-node-id"}, work.KeepLabels)
	require.Len(t, work.Note.DefinedAnchors, 1)
	require.Equal(t, codeanchor.AnchorFunc, work.Note.DefinedAnchors[0].Kind)
	require.NotEmpty(t, work.IntelSections)
	require.NotEmpty(t, work.ContentHash)
}

func TestBuildNoteIndexWorkRejectsNonMarkdownSource(t *testing.T) {
	svc := codeanchor.NewServiceWithOptions(nil, nil, codeanchor.WithoutWarmCache())

	work, err := svc.BuildNoteIndexWorkFromSource(context.Background(), testNoteSource{
		path:    "notes/future.html",
		format:  noteformat.FormatID("future"),
		content: "<h1>Future</h1>",
	})

	require.Nil(t, work)
	require.ErrorContains(t, err, "requires Markdown source")
}

func TestBuildNoteIndexWorkFromSourceUsesCanonicalFreshness(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := currentSchemaAnchorTestDBPath(t, filepath.Join(root, "batch-note-source-freshness.db"))
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	source := testNoteSource{
		path:    "architecture.md",
		content: "# Architecture\n\nBody.",
		hash:    "canonical-hash",
		mtime:   456,
	}

	work, err := svc.BuildNoteIndexWorkFromSource(ctx, source)

	require.NoError(t, err)
	require.NotNil(t, work)
	require.Equal(t, "architecture.md", work.Path)
	require.Equal(t, "canonical-hash", work.ContentHash)
	require.Equal(t, int64(456), work.Mtime)
	require.NotEmpty(t, work.IntelSections)
	require.Equal(t, int64(456), work.IntelSections[0].UpdatedAt)

	require.NoError(t, svc.ApplyNoteIndexBatch(ctx, []codeanchor.NoteIndexWork{*work}))
	meta, err := store.IntelNoteIndexMeta(ctx)
	require.NoError(t, err)
	require.Equal(t, codeanchor.NoteIndexerVersion, meta["architecture.md"].IndexerVersion)

	second, err := svc.BuildNoteIndexWorkFromSource(ctx, source)
	require.NoError(t, err)
	require.Nil(t, second)
}

func TestNoteIndexBatchPreservesMixedCaseAuthoredPathAcrossAnchorMetadataAndIntel(t *testing.T) {
	t.Parallel()

	for _, authoredPath := range []string{"Decision.MD", "decision.md"} {
		authoredPath := authoredPath
		t.Run(authoredPath, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store, err := semdb.Open(currentSchemaAnchorTestDBPath(t, filepath.Join(root, "authored-path.db")))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })

			svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
			content := `---
code-anchors:
  go:
    - label: decision-docs
      symbol: example.com/project.Decision
---
# Decision

This decision must retain its authored filename casing.
`
			work, err := svc.BuildNoteIndexWorkFromSource(ctx, testNoteSource{
				path:    filepath.Join(root, authoredPath),
				content: content,
				hash:    "decision-hash",
				mtime:   456,
			})
			require.NoError(t, err)
			require.Equal(t, authoredPath, work.Path)
			require.Equal(t, authoredPath, work.Note.Path)

			require.NoError(t, svc.ApplyNoteIndexBatch(ctx, []codeanchor.NoteIndexWork{*work}))

			meta, err := store.IntelNoteIndexMeta(ctx)
			require.NoError(t, err)
			require.Contains(t, meta, authoredPath)
			anchorIDs, err := store.AnchorIDsForNotePath(ctx, authoredPath)
			require.NoError(t, err)
			require.Len(t, anchorIDs, 1)

			sections, err := store.IntelDocSectionsByPath(ctx, authoredPath)
			require.NoError(t, err)
			require.NotEmpty(t, sections)
			for _, section := range sections {
				require.Equal(t, authoredPath, section.Path)
			}
		})
	}
}

func TestIngestNoteSourcePreservesMixedCaseAuthoredPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := semdb.Open(currentSchemaAnchorTestDBPath(t, filepath.Join(root, "direct-ingest.db")))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	content := `---
code-anchors:
  go:
    - label: decision-docs-direct
      symbol: example.com/project.Decision
---
# Decision
`
	note, err := svc.IngestNoteSource(ctx, testNoteSource{
		path:    filepath.Join(root, "Decision.MD"),
		content: content,
		hash:    "decision-hash",
		mtime:   456,
	})
	require.NoError(t, err)
	require.Equal(t, "Decision.MD", note.Path)

	anchorIDs, err := store.AnchorIDsForNotePath(ctx, "Decision.MD")
	require.NoError(t, err)
	require.Len(t, anchorIDs, 1)
	sections, err := store.IntelDocSectionsByPath(ctx, "Decision.MD")
	require.NoError(t, err)
	require.NotEmpty(t, sections)
}

func TestApplyCodeIndexBatch_BatchStoreOwnsSummaryAndMetaWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := currentSchemaAnchorTestDBPath(t, filepath.Join(root, "batch-code-persistence.db"))
	inner, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = inner.Close() })

	store := &countingCodeBatchStore{Store: inner}
	svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())

	work := codeanchor.CodeIndexWork{
		Lang: codeanchor.LangGo,
		Path: "caller.go",
		Summary: codeanchor.FileSummary{
			FilePath:    "caller.go",
			Lang:        codeanchor.LangGo,
			Hash:        "hash-caller",
			ParseStatus: codeanchor.ParseOK,
			Symbols: []codeanchor.Symbol{
				{Lang: codeanchor.LangGo, Kind: codeanchor.SymFunc, File: "caller.go", Pkg: "pkg", Name: "Caller", FQN: "pkg.Caller"},
			},
		},
		ReplaceIndex: true,
	}

	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{work}))
	require.Equal(t, int64(0), store.summaryBatchCalls.Load())
	require.Equal(t, int64(0), store.fileMetaCalls.Load())
	require.Equal(t, int64(1), store.persistenceCalls.Load())
}

func TestApplyCodeIndexBatch_PersistsImportOnlyExternalEvidence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	inner, err := semdb.Open(currentSchemaAnchorTestDBPath(t, filepath.Join(root, "batch-import-only.db")))
	require.NoError(t, err)
	t.Cleanup(func() { _ = inner.Close() })
	svc := codeanchor.NewServiceWithOptions(inner, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	target, err := codeanchor.NewExternalTarget(codeanchor.ExternalTargetIdentity{
		Ecosystem: codeanchor.ExternalEcosystemNPM, Module: "react", Kind: codeanchor.ExternalTargetModule,
	})
	require.NoError(t, err)
	path := "side-effect.ts"
	require.NoError(t, inner.ReplaceIntelCodeFile(ctx, path,
		[]codeanchor.IntelAnchor{
			{AnchorID: "stale-a", Lang: codeanchor.LangTS, Kind: "symbol", Path: path, Symbol: "staleA", FQN: "stale.a", Fingerprint: "stale-a"},
			{AnchorID: "stale-b", Lang: codeanchor.LangTS, Kind: "symbol", Path: path, Symbol: "staleB", FQN: "stale.b", Fingerprint: "stale-b"},
		},
		[]codeanchor.IntelEdge{{SrcID: "stale-a", DstID: "stale-b", Kind: "calls"}},
		[]codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: "stale-a", Path: path, Title: "stale marker", Body: "stale marker"}},
	))

	work := codeanchor.CodeIndexWork{
		Lang: codeanchor.LangTS, Path: path, ReplaceIndex: true, ExternalEvidenceReady: true,
		Summary: codeanchor.FileSummary{FilePath: path, Lang: codeanchor.LangTS, Hash: "hash", ParseStatus: codeanchor.ParseOK},
		ExternalEvidence: codeanchor.ExternalEvidenceBatch{Imports: []codeanchor.ExternalImportEvidenceInput{{
			Module: "react", BindingOrdinal: 0, Target: target,
			Evidence: codeanchor.ExternalEvidence{Kind: codeanchor.ExternalEvidenceESMSideEffect, Confidence: codeanchor.ExternalConfidenceHigh},
		}},
		},
	}
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{work}))

	rows, err := inner.ExternalImportEvidenceForPath(ctx, path)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "react", rows[0].Association.Module)
	anchors, err := inner.IntelAnchorsByPath(ctx, path)
	require.NoError(t, err)
	require.Empty(t, anchors)
	edges, err := inner.IntelEdges(ctx)
	require.NoError(t, err)
	require.Empty(t, edges)
	fts, err := inner.SearchIntelFTS(ctx, "stale marker", 10)
	require.NoError(t, err)
	require.Empty(t, fts)

	// A language/indexer that did not classify external references must not turn
	// an absent DTO into destructive negative import evidence.
	work.ExternalEvidenceReady = false
	work.ExternalEvidence = codeanchor.ExternalEvidenceBatch{}
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{work}))
	rows, err = inner.ExternalImportEvidenceForPath(ctx, path)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	work.ExternalEvidenceReady = true
	require.NoError(t, svc.ApplyCodeIndexBatch(ctx, []codeanchor.CodeIndexWork{work}))
	rows, err = inner.ExternalImportEvidenceForPath(ctx, path)
	require.NoError(t, err)
	require.Empty(t, rows)
}
