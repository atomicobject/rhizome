package retrieval

import (
	"context"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestBM25ToSimilarity_IsBoundedAndMonotonic(t *testing.T) {
	// Smaller (more negative) bm25 scores should map to higher similarity.
	sNeg4 := bm25ToSimilarity(-4)
	sNeg1 := bm25ToSimilarity(-1)
	sZero := bm25ToSimilarity(0)
	sPos1 := bm25ToSimilarity(1)
	sPos4 := bm25ToSimilarity(4)

	require.Greater(t, sNeg4, sNeg1)
	require.Greater(t, sNeg1, sZero)
	require.Greater(t, sZero, sPos1)
	require.Greater(t, sPos1, sPos4)

	for _, v := range []float64{sNeg4, sNeg1, sZero, sPos1, sPos4} {
		require.GreaterOrEqual(t, v, 0.0)
		require.LessOrEqual(t, v, 1.0)
		require.False(t, math.IsNaN(v))
	}

	// A neutral bm25 (0) should be around the midpoint.
	require.InDelta(t, 0.5, sZero, 1e-9)
}

func TestIntelLexicalRetriever_CollapsesPathOnlyAnchorsToModule(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	path := "pkg/chunker_test.go"
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, path,
		[]codeanchor.IntelAnchor{
			{AnchorID: "m1", Lang: codeanchor.LangGo, Kind: "module", Path: path, Symbol: "chunker_test.go", Fingerprint: "fp", StartLine: 1, EndLine: 1},
			{AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: "TestSummarizeFrontmatterPriority", FQN: "example.com/mod/pkg.TestSummarizeFrontmatterPriority", Fingerprint: "fp", StartLine: 1, EndLine: 1},
		},
		nil,
		[]codeanchor.IntelFTSRow{
			{ItemType: "anchor", ItemID: "m1", Path: path, Title: "chunker_test.go", Body: path},
			{ItemType: "anchor", ItemID: "a1", Path: path, Title: "TestSummarizeFrontmatterPriority", Body: "example.com/mod/pkg.TestSummarizeFrontmatterPriority"},
		},
	))

	r := &IntelLexicalRetriever{Store: store}
	cands, err := r.Retrieve(ctx, search.QuerySpec{Text: "chunker", Limits: search.Limits{Total: 25}})
	require.NoError(t, err)
	require.NotEmpty(t, cands)
	found, foundOriginal := false, false
	for _, c := range cands {
		require.NotEqual(t, "a1", c.AnchorID)
		if c.AnchorID == "m1" {
			found = true
			require.Equal(t, path, c.Path)
			require.Equal(t, "module", c.Kind)
			requireEvidenceType(t, c.Evidence, "intel_fts_match")
			for _, ev := range c.Evidence {
				if ev.Type == "intel_fts_match" && ev.Details["original"] == "a1" && ev.Details["pathOnly"] == "true" {
					foundOriginal = true
				}
			}
		}
	}
	require.True(t, found, "path-only anchor hit should resolve to module m1")
	require.True(t, foundOriginal, "module must retain the original path-only hit")
}

func TestIntelLexicalRetriever_DocSectionsUseNoteOwner(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	notePath := "notes/search.md"
	sectionID := "section-search"
	content := "# Search Ranking\n\nranking budget fairness"
	require.NoError(t, store.ReplaceIntelDocSections(ctx, notePath,
		[]codeanchor.IntelDocSection{{
			SectionID:   sectionID,
			Path:        notePath,
			Title:       "Search Ranking",
			Level:       1,
			StartByte:   0,
			EndByte:     int64(len(content)),
			Content:     content,
			Fingerprint: "fp",
		}},
		nil,
		[]codeanchor.IntelFTSRow{{
			ItemType: "doc_section",
			ItemID:   sectionID,
			Path:     notePath,
			Title:    "Search Ranking",
			Body:     "ranking budget fairness",
		}},
	))

	r := &IntelLexicalRetriever{Store: store}
	cands, err := r.Retrieve(ctx, search.QuerySpec{Text: "ranking", Limits: search.Limits{Total: 25}})
	require.NoError(t, err)
	require.NotEmpty(t, cands)

	cand := cands[0]
	require.Equal(t, "doc_section", cand.Type)
	require.Equal(t, knowledge.NoteHandle(notePath), cand.Owner)
	require.Equal(t, knowledge.FileHandle(notePath).String()+"#doc#"+sectionID, cand.Handle.String())
	require.Equal(t, notePath+"#search-ranking-0", cand.NodeID)
	require.Equal(t, notePath+"#search-ranking-0", cand.SourceLocator)
	require.Equal(t, "SECTION", cand.NodeKind)
	require.NotNil(t, cand.NodeRef)
	require.Equal(t, ontology.NodeKindSection, cand.NodeRef.Kind)
	require.Equal(t, notePath+"#search-ranking-0", cand.NodeRef.NodeID)
	require.Equal(t, "search-ranking-0", cand.NodeRef.Fragment)
	require.Equal(t, 0, cand.ChunkIndex)
}

func TestIntelLexicalRetriever_DuplicateHeadingsResolveDistinctCanonicalSections(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	notePath := "notes/duplicates.md"
	content := "# Document\n\n## Decision\nfirst target\n\n## Decision\nsecond target\n"
	firstStart := strings.Index(content, "## Decision")
	secondStart := strings.LastIndex(content, "## Decision")
	require.GreaterOrEqual(t, firstStart, 0)
	require.Greater(t, secondStart, firstStart)
	sections := []codeanchor.IntelDocSection{
		{
			SectionID:   "intel-first",
			Path:        notePath,
			Title:       "Decision",
			Level:       2,
			StartByte:   int64(firstStart),
			EndByte:     int64(secondStart),
			Content:     content[firstStart:secondStart],
			Fingerprint: "first",
		},
		{
			SectionID:   "intel-second",
			Path:        notePath,
			Title:       "Decision",
			Level:       2,
			StartByte:   int64(secondStart),
			EndByte:     int64(len(content)),
			Content:     content[secondStart:],
			Fingerprint: "second",
		},
	}
	ftsRows := []codeanchor.IntelFTSRow{
		{ItemType: "doc_section", ItemID: "intel-first", Path: notePath, Title: "Decision", Body: sections[0].Content},
		{ItemType: "doc_section", ItemID: "intel-second", Path: notePath, Title: "Decision", Body: sections[1].Content},
	}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, notePath, sections, nil, ftsRows))

	r := &IntelLexicalRetriever{Store: store}
	cands, err := r.Retrieve(ctx, search.QuerySpec{Text: "Decision", Limits: search.Limits{Total: 25}})
	require.NoError(t, err)
	require.Len(t, cands, 2)

	byHandle := make(map[string]search.Candidate, len(cands))
	for _, cand := range cands {
		require.Equal(t, "doc_section", cand.Type)
		require.NotEqual(t, cand.NodeID, "intel-first")
		require.NotEqual(t, cand.NodeID, "intel-second")
		byHandle[cand.Handle.String()] = cand
	}

	first := byHandle[knowledge.FileHandle(notePath).String()+"#doc#intel-first"]
	require.Equal(t, notePath+"#decision-"+strconv.Itoa(firstStart), first.NodeID)
	require.Equal(t, first.NodeID, first.SourceLocator)
	require.NotNil(t, first.NodeRef)
	require.Equal(t, "decision-"+strconv.Itoa(firstStart), first.NodeRef.Fragment)

	second := byHandle[knowledge.FileHandle(notePath).String()+"#doc#intel-second"]
	require.Equal(t, notePath+"#decision-"+strconv.Itoa(secondStart), second.NodeID)
	require.Equal(t, second.NodeID, second.SourceLocator)
	require.NotNil(t, second.NodeRef)
	require.Equal(t, "decision-"+strconv.Itoa(secondStart), second.NodeRef.Fragment)
	require.NotEqual(t, first.NodeID, second.NodeID)
}

func TestIntelLexicalRetriever_HeadinglessDocSectionResolvesToNoteRoot(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	notePath := "notes/plain.md"
	content := "A plain note with searchable ranking content.\n"
	section := codeanchor.IntelDocSection{
		SectionID:   "intel-whole-note",
		Path:        notePath,
		Title:       "plain.md",
		Level:       1,
		StartByte:   0,
		EndByte:     int64(len(content)),
		Content:     content,
		Fingerprint: "whole-note",
	}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, notePath, []codeanchor.IntelDocSection{section}, nil, []codeanchor.IntelFTSRow{{
		ItemType: "doc_section",
		ItemID:   section.SectionID,
		Path:     notePath,
		Title:    section.Title,
		Body:     content,
	}}))

	r := &IntelLexicalRetriever{Store: store}
	cands, err := r.Retrieve(ctx, search.QuerySpec{Text: "ranking", Limits: search.Limits{Total: 25}})
	require.NoError(t, err)
	require.Len(t, cands, 1)

	cand := cands[0]
	require.Equal(t, "doc_section", cand.Type)
	require.Empty(t, cand.NodeID)
	require.Equal(t, notePath, cand.SourceLocator)
	require.Equal(t, string(ontology.NodeKindNote), cand.NodeKind)
	require.NotNil(t, cand.NodeRef)
	require.Equal(t, ontology.NodeKindNote, cand.NodeRef.Kind)
	require.Empty(t, cand.NodeRef.Fragment)
}

func TestIntelLexicalRetriever_AuthoredBlockIDPreservesCanonicalSectionIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	notePath := "notes/anchored.md"
	fullContent := "# Document\n\n## Decision\n^decision\n\nanchored ranking content\n"
	sectionStart := strings.Index(fullContent, "## Decision")
	sectionContent := fullContent[sectionStart:]
	section := codeanchor.IntelDocSection{
		SectionID:   "intel-anchored",
		Path:        notePath,
		Title:       "Decision",
		Level:       2,
		StartByte:   int64(sectionStart),
		EndByte:     int64(len(fullContent)),
		Content:     sectionContent,
		Fingerprint: "anchored",
	}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, notePath, []codeanchor.IntelDocSection{section}, nil, []codeanchor.IntelFTSRow{{
		ItemType: "doc_section",
		ItemID:   section.SectionID,
		Path:     notePath,
		Title:    section.Title,
		Body:     sectionContent,
	}}))

	r := &IntelLexicalRetriever{Store: store}
	cands, err := r.Retrieve(ctx, search.QuerySpec{Text: "ranking", Limits: search.Limits{Total: 25}})
	require.NoError(t, err)
	require.Len(t, cands, 1)

	cand := cands[0]
	require.Equal(t, notePath+"#^decision", cand.NodeID)
	require.Equal(t, notePath+"#^decision", cand.SourceLocator)
	require.Equal(t, "^decision", cand.NodeRef.Fragment)
	require.Equal(t, notePath+"#^decision", cand.NodeRef.NodeID)
}

func TestIntelLexicalRetriever_ProviderRegionsUseCanonicalNoteOwner(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	notePath := "reports/prototype.html"
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "hash", RawNotesHash: "raw", LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{{Path: notePath, Title: "Prototype", FormatID: "html", ContentHash: "hash"}},
		SearchRegions: []semdb.NoteSearchRegionRow{{
			NotePath: notePath, Ordinal: 0, Origin: "derived", Kind: "visible",
			Text: "quarterly frobnication results", MediaType: "text/plain",
		}},
	}))

	r := &IntelLexicalRetriever{Store: store}
	cands, err := r.Retrieve(ctx, search.QuerySpec{Text: "frobnication", Limits: search.Limits{Total: 25}})
	require.NoError(t, err)
	require.Len(t, cands, 1)
	require.Equal(t, "note", cands[0].Type)
	require.Equal(t, knowledge.NoteHandle(notePath), cands[0].Handle)
	require.Equal(t, knowledge.NoteHandle(notePath), cands[0].Owner)
	require.Equal(t, "visible", cands[0].Evidence[0].Details["regionKind"])
}
