package sqlite

import (
	"context"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

func TestIndexingWritesExposeNamedOps(t *testing.T) {
	t.Parallel()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	store, err := Open(currentSchemaTestDBPath(t, "labels.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.UpsertNoteMetadataBatch(ctx, map[string]codeanchor.NoteIndexMeta{
		"notes/a.md": {ContentHash: "h1", IndexerVersion: "v1", Mtime: 1},
	}))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "notes/a.md", []codeanchor.DocLink{{
		SrcType: "note", SrcPath: "notes/a.md", DstKind: "note", DstPath: "notes/b.md", UpdatedAt: time.Now().Unix(),
	}}))
	require.NoError(t, store.DeleteDocLinksByPath(ctx, "notes/a.md"))
	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"src/a.go": {{OwnerFQN: "pkg.Foo", RefKind: codeanchor.RefKindCalls, DstLang: codeanchor.LangGo, DstName: "Bar"}},
	}))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", []codeanchor.IntelAnchor{{
		AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "func", Path: "src/a.go", Symbol: "Foo", FQN: "pkg.Foo", Fingerprint: "fp-a1",
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/a.md", []codeanchor.IntelDocSection{{
		SectionID: "s1", Path: "notes/a.md", Title: "A", Level: 1, Content: "body", Fingerprint: "fp",
	}}, nil, nil))
	require.NoError(t, store.UpsertNoteMeta(ctx, "notes/a.md", "h2", "v2", 2))

	summary := collector.RenderSummary()
	require.Contains(t, summary, "intel.upsert_note_metadata_batch")
	require.Contains(t, summary, "intel.replace_doc_links")
	require.Contains(t, summary, "intel.delete_doc_links")
	require.Contains(t, summary, "intel.replace_code_file")
	require.Contains(t, summary, "intel.replace_doc_sections")
	require.Contains(t, summary, "intel.replace_symbol_refs")
	require.Contains(t, summary, "intel.upsert_note_meta")
	require.NotContains(t, summary, "unknown")
}
