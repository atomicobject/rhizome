package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRationaleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	path := "pkg/foo/foo.go"

	records := []codeanchor.Rationale{
		{
			ID:          "abc123def456abcd",
			Path:        path,
			SymbolFQN:   "pkg/foo.Foo",
			Kind:        codeanchor.RationaleNote,
			Content:     "NOTE: this is important",
			StartLine:   10,
			EndLine:     10,
			Fingerprint: "deadbeef01234567890abcdef01234567890abcdef01234567890abcdef012345",
		},
		{
			ID:          "1234567890abcdef",
			Path:        path,
			SymbolFQN:   "",
			Kind:        codeanchor.RationaleHack,
			Content:     "HACK: workaround",
			StartLine:   20,
			EndLine:     21,
			Fingerprint: "cafebabe01234567890abcdef01234567890abcdef01234567890abcdef012345",
		},
	}

	err = store.ReplaceRationaleForPath(ctx, path, records)
	require.NoError(t, err)

	got, err := store.RationaleForPath(ctx, path)
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, records[0].ID, got[0].ID)
	assert.Equal(t, records[0].Kind, got[0].Kind)
	assert.Equal(t, records[0].SymbolFQN, got[0].SymbolFQN)
	assert.Equal(t, records[0].Content, got[0].Content)
	assert.Equal(t, records[0].StartLine, got[0].StartLine)
	assert.Equal(t, records[0].EndLine, got[0].EndLine)

	assert.Equal(t, records[1].ID, got[1].ID)
	assert.Equal(t, "", got[1].SymbolFQN, "empty symbol FQN should round-trip as empty string")
}

func TestRationaleReplace(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	path := "pkg/bar/bar.go"

	first := []codeanchor.Rationale{
		{ID: "aaaa0001aaaa0001", Path: path, Kind: codeanchor.RationaleTodo, Content: "TODO: first", StartLine: 5, EndLine: 5, Fingerprint: "fp1"},
		{ID: "aaaa0002aaaa0002", Path: path, Kind: codeanchor.RationaleNote, Content: "NOTE: old", StartLine: 8, EndLine: 8, Fingerprint: "fp2"},
	}
	require.NoError(t, store.ReplaceRationaleForPath(ctx, path, first))

	// Replace with only one new record.
	second := []codeanchor.Rationale{
		{ID: "bbbb0001bbbb0001", Path: path, Kind: codeanchor.RationaleFixme, Content: "FIXME: new", StartLine: 12, EndLine: 12, Fingerprint: "fp3"},
	}
	require.NoError(t, store.ReplaceRationaleForPath(ctx, path, second))

	got, err := store.RationaleForPath(ctx, path)
	require.NoError(t, err)
	require.Len(t, got, 1, "replace should leave only new records")
	assert.Equal(t, codeanchor.RationaleFixme, got[0].Kind)
}

func TestRationaleForSymbol(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	fqn := "pkg/baz.MyFunc"

	records := []codeanchor.Rationale{
		{ID: "sym0001sym0001aa", Path: "pkg/baz/baz.go", SymbolFQN: fqn, Kind: codeanchor.RationaleNote, Content: "NOTE: in func", StartLine: 15, EndLine: 15, Fingerprint: "fp10"},
		{ID: "sym0002sym0002bb", Path: "pkg/baz/baz.go", SymbolFQN: "", Kind: codeanchor.RationaleHack, Content: "HACK: file level", StartLine: 1, EndLine: 1, Fingerprint: "fp11"},
	}
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "pkg/baz/baz.go", records))

	got, err := store.RationaleForSymbol(ctx, fqn)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, fqn, got[0].SymbolFQN)
	assert.Equal(t, codeanchor.RationaleNote, got[0].Kind)
}

func TestSearchRationaleFTS_ReturnsSnippetLineAndKind(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	path := "pkg/search/service.go"
	require.NoError(t, store.ReplaceRationaleForPath(ctx, path, []codeanchor.Rationale{{
		ID:          "fts0001fts0001",
		Path:        path,
		SymbolFQN:   "pkg/search.Service.Search",
		Kind:        codeanchor.RationaleWhy,
		Content:     "WHY: preserve mixed evidence so search does not overfit one retriever",
		StartLine:   42,
		EndLine:     43,
		Fingerprint: "fp-search",
	}}))

	rows, err := store.SearchRationaleFTS(ctx, "why preserve evidence?", []string{"why", "rationale", "important"}, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "fts0001fts0001", rows[0].ID)
	require.Equal(t, path, rows[0].Path)
	require.Equal(t, "pkg/search.Service.Search", rows[0].SymbolFQN)
	require.Equal(t, "why", rows[0].Kind)
	require.Equal(t, int64(42), rows[0].StartLine)
	require.Contains(t, rows[0].Snippet, "preserve")
}

func TestSearchRationaleFTS_ReplaceRemovesStaleRows(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	path := "pkg/search/service.go"
	require.NoError(t, store.ReplaceRationaleForPath(ctx, path, []codeanchor.Rationale{{
		ID:          "stale001stale01",
		Path:        path,
		Kind:        codeanchor.RationaleWhy,
		Content:     "WHY: stale rationale should disappear",
		StartLine:   10,
		EndLine:     10,
		Fingerprint: "fp-stale",
	}}))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, path, []codeanchor.Rationale{{
		ID:          "fresh01fresh01",
		Path:        path,
		Kind:        codeanchor.RationaleWhy,
		Content:     "WHY: fresh rationale remains",
		StartLine:   12,
		EndLine:     12,
		Fingerprint: "fp-fresh",
	}}))

	stale, err := store.SearchRationaleFTS(ctx, "stale", []string{"why"}, 10)
	require.NoError(t, err)
	require.Empty(t, stale)
	fresh, err := store.SearchRationaleFTS(ctx, "fresh", []string{"why"}, 10)
	require.NoError(t, err)
	require.Len(t, fresh, 1)
	require.Equal(t, "fresh01fresh01", fresh[0].ID)
}

func TestSearchRationaleFTS_BatchReplaceUpdatesFTS(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	path := "pkg/search/planner.go"
	require.NoError(t, store.ApplyCodePersistenceBatch(ctx, codeanchor.CodePersistenceBatch{
		RationaleBatches: []codeanchor.RationaleBatch{{
			Path: path,
			Rationales: []codeanchor.Rationale{{
				ID:          "batch01batch01",
				Path:        path,
				Kind:        codeanchor.RationaleImportant,
				Content:     "IMPORTANT: planner gates rationale retrieval",
				StartLine:   7,
				EndLine:     7,
				Fingerprint: "fp-batch",
			}},
		}},
	}))

	rows, err := store.SearchRationaleFTS(ctx, "planner gates", []string{"important"}, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "batch01batch01", rows[0].ID)
}

func TestRationaleByKind(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	records := []codeanchor.Rationale{
		{ID: "kind0001kind0001", Path: "a.go", Kind: codeanchor.RationaleTodo, Content: "TODO: one", StartLine: 1, EndLine: 1, Fingerprint: "fp1"},
		{ID: "kind0002kind0002", Path: "a.go", Kind: codeanchor.RationaleNote, Content: "NOTE: two", StartLine: 2, EndLine: 2, Fingerprint: "fp2"},
		{ID: "kind0003kind0003", Path: "b.go", Kind: codeanchor.RationaleTodo, Content: "TODO: three", StartLine: 3, EndLine: 3, Fingerprint: "fp3"},
	}
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "a.go", records[:2]))
	require.NoError(t, store.ReplaceRationaleForPath(ctx, "b.go", records[2:]))

	// All kinds.
	all, err := store.RationaleByKind(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// Filter by todo only.
	todos, err := store.RationaleByKind(ctx, []string{"todo"})
	require.NoError(t, err)
	require.Len(t, todos, 2)
	for _, r := range todos {
		assert.Equal(t, codeanchor.RationaleTodo, r.Kind)
	}
}
