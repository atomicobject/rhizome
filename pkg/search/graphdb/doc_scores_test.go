package graphdb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestComputeDocScores_BuildsConnectedCommunities(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	root := t.TempDir()
	n1 := filepath.ToSlash(filepath.Join("notes", "n1.md"))
	n2 := filepath.ToSlash(filepath.Join("notes", "n2.md"))
	codeRel := filepath.ToSlash(filepath.Join("pkg", "foo.go"))
	codeAbs := filepath.ToSlash(filepath.Join(root, filepath.FromSlash(codeRel)))

	anchor := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        "go",
		Kind:        "function",
		Path:        codeRel,
		Symbol:      "Item",
		Fingerprint: "fp",
		StartByte:   0,
		EndByte:     1,
		StartLine:   1,
		EndLine:     1,
		UpdatedAt:   time.Now().Unix(),
		DocComment:  "",
		Signature:   "",
		FQN:         "",
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, codeRel, []codeanchor.IntelAnchor{anchor}, nil, nil))

	// Mixed-case Markdown-looking suffixes must not decide ownership: the note
	// and code endpoints keep their persisted kinds and their mention edge.
	decisionNote := "notes/Decision.mD"
	decisionCode := "web/Decision.mD"
	decisionAnchor := codeanchor.IntelAnchor{AnchorID: "a2", Lang: "typescript", Kind: "function", Path: decisionCode, Symbol: "Decide", Fingerprint: "fp", StartByte: 0, EndByte: 1, StartLine: 1, EndLine: 1, UpdatedAt: time.Now().Unix()}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, decisionCode, []codeanchor.IntelAnchor{decisionAnchor}, nil, nil))
	decisionSection := codeanchor.IntelDocSection{SectionID: "s3", Path: decisionNote, Title: "Decision", Level: 1, StartByte: 0, EndByte: 8, Content: "@Decide", Fingerprint: "fp", UpdatedAt: time.Now().Unix()}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, decisionNote, []codeanchor.IntelDocSection{decisionSection}, []codeanchor.IntelEdge{{SrcID: "s3", DstID: "a2", Kind: "mentions"}}, nil))

	sec1 := codeanchor.IntelDocSection{
		SectionID:   "s1",
		Path:        n1,
		Title:       "N1",
		Level:       1,
		StartByte:   0,
		EndByte:     10,
		Content:     "See [[n2]] and mention @Item",
		Fingerprint: "fp",
		UpdatedAt:   time.Now().Unix(),
	}
	sec2 := codeanchor.IntelDocSection{
		SectionID:   "s2",
		Path:        n2,
		Title:       "N2",
		Level:       1,
		StartByte:   0,
		EndByte:     5,
		Content:     "Hello",
		Fingerprint: "fp",
		UpdatedAt:   time.Now().Unix(),
	}
	mentions := []codeanchor.IntelEdge{{SrcID: sec1.SectionID, DstID: anchor.AnchorID, Kind: "mentions"}}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, n1, []codeanchor.IntelDocSection{sec1}, mentions, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, n2, []codeanchor.IntelDocSection{sec2}, nil, nil))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", RawNotesHash: "raw-notes", LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{Path: n1, ContentHash: "n1-hash", Mtime: 1, Size: 1, FormatID: "markdown", Projection: semdb.NoteProjectionState{ProviderVersion: "markdown-provider-v1", ProjectionVersion: "markdown-projection-v3", SourceContentHash: "n1-hash", Status: semdb.NoteProjectionStatusCurrent, UpdatedAt: 1}},
			{Path: n2, ContentHash: "n2-hash", Mtime: 1, Size: 1, FormatID: "markdown", Projection: semdb.NoteProjectionState{ProviderVersion: "markdown-provider-v1", ProjectionVersion: "markdown-projection-v3", SourceContentHash: "n2-hash", Status: semdb.NoteProjectionStatusCurrent, UpdatedAt: 1}},
			{Path: decisionNote, ContentHash: "decision-hash", Mtime: 1, Size: 1, FormatID: "markdown", Projection: semdb.NoteProjectionState{ProviderVersion: "markdown-provider-v1", ProjectionVersion: "markdown-projection-v3", SourceContentHash: "decision-hash", Status: semdb.NoteProjectionStatusCurrent, UpdatedAt: 1}},
		},
		WikilinkEdges: []semdb.GraphDocEdgeRow{{SrcPath: n1, DstPath: n2, Kind: semdb.GraphDocEdgeKindWikilink}},
	}))

	require.NoError(t, store.ReplaceDocLinksForPath(ctx, codeAbs, []codeanchor.DocLink{{
		SrcType:   "code",
		SrcPath:   codeAbs,
		DstKind:   "note",
		DstPath:   n2,
		UpdatedAt: time.Now().Unix(),
	}}))

	edgeCount, err := store.GraphDocNoteEdgeCount(ctx)
	require.NoError(t, err)
	require.Greater(t, edgeCount, 0)

	scores, err := ComputeDocScores(ctx, store, DocScoresOptions{
		WikilinkOptions: obsidian.DefaultWikilinkOptions,
		VaultRoot:       root,
	})
	require.NoError(t, err)
	require.NotEmpty(t, scores)

	require.NoError(t, store.ReplaceGraphDocScores(ctx, scores))
	lookup, err := store.GraphDocScoresByPaths(ctx, []string{n1, n2, codeRel, decisionNote, decisionCode})
	require.NoError(t, err)

	require.Contains(t, lookup, string(paths.NormalizeNote(n1)))
	require.Contains(t, lookup, string(paths.NormalizeNote(n2)))
	require.Contains(t, lookup, string(paths.NormalizeCode(codeRel)))

	c1 := lookup[string(paths.NormalizeNote(n1))].Community
	c2 := lookup[string(paths.NormalizeNote(n2))].Community
	cc := lookup[string(paths.NormalizeCode(codeRel))].Community
	require.NotEmpty(t, c1)
	require.NotEmpty(t, c2)
	require.NotEmpty(t, cc)
	require.True(t, cc == c1 || cc == c2)

	require.Equal(t, "note", lookup[string(paths.NormalizeNote(n1))].DocType)
	require.Equal(t, "note", lookup[string(paths.NormalizeNote(n2))].DocType)
	require.Equal(t, "code", lookup[string(paths.NormalizeCode(codeRel))].DocType)

	require.Contains(t, lookup, decisionNote)
	require.Contains(t, lookup, decisionCode)
	require.Equal(t, "note", lookup[decisionNote].DocType)
	require.Equal(t, "code", lookup[decisionCode].DocType)
	require.Equal(t, 1, lookup[decisionNote].Outbound)
	require.Equal(t, 1, lookup[decisionCode].Inbound)
	require.Equal(t, lookup[decisionNote].Community, lookup[decisionCode].Community)
}

// TestComputeDocScores_HITSExcludesCodeEdges verifies that HITS hub/authority scores
// are computed only from doc-domain edges (wikilinks, mentions, coderefs), not code→code edges.
func TestComputeDocScores_HITSExcludesCodeEdges(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	root := t.TempDir()

	// Create two code files with a call edge between them (code→code).
	code1 := filepath.ToSlash(filepath.Join("pkg", "caller.go"))
	code2 := filepath.ToSlash(filepath.Join("pkg", "callee.go"))

	anchor1 := codeanchor.IntelAnchor{
		AnchorID:    "caller-fn",
		Lang:        "go",
		Kind:        "function",
		Path:        code1,
		Symbol:      "Caller",
		Fingerprint: "fp1",
		StartLine:   1, EndLine: 10,
		UpdatedAt: time.Now().Unix(),
	}
	anchor2 := codeanchor.IntelAnchor{
		AnchorID:    "callee-fn",
		Lang:        "go",
		Kind:        "function",
		Path:        code2,
		Symbol:      "Callee",
		Fingerprint: "fp2",
		StartLine:   1, EndLine: 10,
		UpdatedAt: time.Now().Unix(),
	}

	// caller.go calls callee.go (code→code edge)
	callEdges := []codeanchor.IntelEdge{{SrcID: anchor1.AnchorID, DstID: anchor2.AnchorID, Kind: "calls"}}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, code2, []codeanchor.IntelAnchor{anchor2}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, code1, []codeanchor.IntelAnchor{anchor1}, callEdges, nil))

	// Compute scores - the code files should have zero HITS scores because
	// there are no doc-domain edges, only code→code edges.
	scores, err := ComputeDocScores(ctx, store, DocScoresOptions{
		WikilinkOptions: obsidian.DefaultWikilinkOptions,
		VaultRoot:       root,
	})
	require.NoError(t, err)

	// Find the scores for our code files
	var code1Score, code2Score *semdb.GraphDocScore
	for i := range scores {
		if scores[i].DocPath == string(paths.NormalizeCode(code1)) {
			code1Score = &scores[i]
		}
		if scores[i].DocPath == string(paths.NormalizeCode(code2)) {
			code2Score = &scores[i]
		}
	}

	require.NotNil(t, code1Score, "code1 should be in scores")
	require.NotNil(t, code2Score, "code2 should be in scores")

	// HITS scores should be zero because code→code edges are excluded from HITS.
	// (HITS only considers doc-domain edges: wikilinks, mentions, coderefs)
	require.Equal(t, 0.0, code1Score.Hub, "code file with only code edges should have zero hub score")
	require.Equal(t, 0.0, code1Score.Authority, "code file with only code edges should have zero authority score")
	require.Equal(t, 0.0, code2Score.Hub, "code file with only code edges should have zero hub score")
	require.Equal(t, 0.0, code2Score.Authority, "code file with only code edges should have zero authority score")

	// But they should still be in the same community (label propagation uses all edges)
	require.Equal(t, code1Score.Community, code2Score.Community, "code files connected by calls should be in same community")
}

func TestComputeDocScores_IncludesOntologyRelations(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	root := t.TempDir()
	effort := "docs/effort.md"
	spec := "docs/spec.md"
	now := time.Now().Unix()
	require.NoError(t, store.ReplaceIntelDocSections(ctx, effort, []codeanchor.IntelDocSection{{
		SectionID:   "effort",
		Path:        effort,
		Title:       "Effort",
		Level:       1,
		Content:     "Frozen scope",
		Fingerprint: "fp-effort",
		UpdatedAt:   now,
	}}, nil, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, spec, []codeanchor.IntelDocSection{{
		SectionID:   "spec",
		Path:        spec,
		Title:       "Spec",
		Level:       1,
		Content:     "Specification",
		Fingerprint: "fp-spec",
		UpdatedAt:   now,
	}}, nil, nil))
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", RawNotesHash: "raw-notes", LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{Path: effort, ContentHash: "effort-hash", Mtime: 1, Size: 1, FormatID: "markdown", Projection: semdb.NoteProjectionState{ProviderVersion: "markdown-provider-v1", ProjectionVersion: "markdown-projection-v3", SourceContentHash: "effort-hash", Status: semdb.NoteProjectionStatusCurrent, UpdatedAt: 1}},
			{Path: spec, ContentHash: "spec-hash", Mtime: 1, Size: 1, FormatID: "markdown", Projection: semdb.NoteProjectionState{ProviderVersion: "markdown-provider-v1", ProjectionVersion: "markdown-projection-v3", SourceContentHash: "spec-hash", Status: semdb.NoteProjectionStatusCurrent, UpdatedAt: 1}},
		},
	}))
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{
		Edges: []semdb.OntologyEdgeRow{{
			SrcPath:      effort,
			RelationName: "frozenSpecs",
			DstPath:      spec,
			DstType:      "TechnicalSpec",
			Provenance:   "field",
			Structural:   true,
		}},
		SchemaState: semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", Ready: true},
	}))

	scores, err := ComputeDocScores(ctx, store, DocScoresOptions{
		WikilinkOptions: obsidian.DefaultWikilinkOptions,
		VaultRoot:       root,
	})
	require.NoError(t, err)
	require.NoError(t, store.ReplaceGraphDocScores(ctx, scores))

	lookup, err := store.GraphDocScoresByPaths(ctx, []string{effort, spec})
	require.NoError(t, err)
	require.Greater(t, lookup[spec].Authority, 0.0)
	require.Equal(t, 1, lookup[effort].Outbound)
	require.Equal(t, 1, lookup[spec].Inbound)
}
