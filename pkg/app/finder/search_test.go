package finder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func paths(results []Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Path
	}
	return out
}

func TestSearchRanksExactThenPrefixTitlesAndFavorsTitles(t *testing.T) {
	t.Parallel()
	c := NewCatalog([]store.FinderNoteRow{
		{Path: "a/roadmap-archive.md", Title: "Roadmap archive"},
		{Path: "b/plan.md", Title: "Quarterly road map"},
		{Path: "c/roadmap.md", Title: "Roadmap", Type: "Effort"},
		{Path: "roadmap/notes.md", Title: "Notes"},
	}, []store.FinderCodeRow{{Path: "pkg/roadmap/roadmap.go", Lang: "go"}})
	got := c.Search(context.Background(), Query{Text: "roadmap"})
	// Exact title, then title prefixes (notes before code), then fuzzy matches
	// where a title match beats the same match in a path.
	require.Equal(t, []string{"c/roadmap.md", "a/roadmap-archive.md", "pkg/roadmap/roadmap.go", "b/plan.md", "roadmap/notes.md"}, paths(got))
	require.Equal(t, "Effort", got[0].Type)
	require.Equal(t, [][2]int{{0, 7}}, got[0].TitleMatches)
}

func TestSearchIsSmartCase(t *testing.T) {
	t.Parallel()
	c := NewCatalog([]store.FinderNoteRow{
		{Path: "guide.md", Title: "Schema Guide"},
		{Path: "notes/schema-notes.md", Title: "notes on the schema"},
	}, nil)
	require.ElementsMatch(t, []string{"guide.md", "notes/schema-notes.md"}, paths(c.Search(context.Background(), Query{Text: "schema"})))
	require.Equal(t, []string{"guide.md"}, paths(c.Search(context.Background(), Query{Text: "Schema"})))
}

func TestSearchToleratesSwappedAndDroppedCharacters(t *testing.T) {
	t.Parallel()
	c := NewCatalog([]store.FinderNoteRow{
		{Path: "docs/schema.md", Title: "Schema"},
		{Path: "docs/other.md", Title: "Something else"},
	}, nil)
	for _, query := range []string{"shcema", "schemma", "scehma"} {
		got := c.Search(context.Background(), Query{Text: query})
		require.NotEmpty(t, got, query)
		require.Equal(t, "docs/schema.md", got[0].Path, query)
		require.NotEmpty(t, got[0].TitleMatches, query)
	}
	require.Empty(t, c.Search(context.Background(), Query{Text: "zzzz"}))
}

func TestFrecencyBreaksCloseScoresOnly(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	c := NewCatalog([]store.FinderNoteRow{
		{Path: "a.md", Title: "Alpha notes"},
		{Path: "b.md", Title: "Alpha plans"},
		{Path: "weak.md", Title: "a long list of percolating heuristics"},
	}, nil)
	require.Equal(t, "a.md", c.Search(context.Background(), Query{Text: "alpha", Now: now})[0].Path)
	opens := map[string]Open{"b.md": {Count: 3, LastOpened: now.Add(-time.Hour)}}
	require.Equal(t, "b.md", c.Search(context.Background(), Query{Text: "alpha", Now: now, Opens: opens})[0].Path)

	// A heavily opened weak match cannot overtake a strong one.
	opens = map[string]Open{"weak.md": {Count: 500, LastOpened: now}}
	got := c.Search(context.Background(), Query{Text: "alp", Now: now, Opens: opens})
	require.Equal(t, "weak.md", got[len(got)-1].Path)
}

func TestEmptyQueryListsFrecentThenRecentlyChanged(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	c := NewCatalog([]store.FinderNoteRow{
		{Path: "old.md", Title: "Old", Mtime: 1},
		{Path: "new.md", Title: "New", Mtime: 3},
		{Path: "mid.md", Title: "Mid", Mtime: 2},
	}, []store.FinderCodeRow{{Path: "main.go", Lang: "go"}})
	opens := map[string]Open{
		"main.go": {Count: 1, LastOpened: now},
		"old.md":  {Count: 4, LastOpened: now},
		"gone.md": {Count: 9, LastOpened: now},
	}
	got := c.Search(context.Background(), Query{Now: now, Opens: opens, Limit: 4})
	require.Equal(t, []string{"old.md", "main.go", "new.md", "mid.md"}, paths(got))
	require.Equal(t, []string{"main.go"}, paths(c.Search(context.Background(), Query{Now: now, Opens: opens, Kind: KindCode})))
}

func TestMatchRangesUseUTF16Units(t *testing.T) {
	t.Parallel()
	c := NewCatalog([]store.FinderNoteRow{{Path: "x.md", Title: "🎉 café"}}, nil)
	got := c.Search(context.Background(), Query{Text: "cafe"})
	require.Len(t, got, 1)
	require.Equal(t, [][2]int{{3, 7}}, got[0].TitleMatches)
}

func TestTypoVariantsAreBounded(t *testing.T) {
	t.Parallel()
	require.Len(t, typoVariants([]rune("abc"), false), 2)
	require.LessOrEqual(t, len(typoVariants([]rune("abcdefgh"), false)), 2*8-1)
}

func TestLiveServesPreviousCatalogWhileRebuilding(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	builds := make(chan int, 4)
	n := 0
	live := NewLive(func(ctx context.Context) (*Catalog, error) {
		n++
		builds <- n
		if n > 1 {
			<-release
		}
		return NewCatalog([]store.FinderNoteRow{{Path: "v.md", Title: map[bool]string{true: "first", false: "second"}[n == 1]}}, nil), nil
	}, time.Millisecond)
	defer live.Close()
	ctx := context.Background()
	first, err := live.Catalog(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, <-builds)
	require.Equal(t, "first", first.Search(context.Background(), Query{Text: "v"})[0].Title)

	live.Invalidate()
	require.Equal(t, 2, <-builds)
	during, err := live.Catalog(ctx)
	require.NoError(t, err)
	require.Same(t, first, during, "a request never waits for a rebuild")
	close(release)
	require.Eventually(t, func() bool {
		c, _ := live.Catalog(ctx)
		return c != first && c.Search(context.Background(), Query{Text: "v"})[0].Title == "second"
	}, time.Second, time.Millisecond)
}

func TestLiveRetriesAFailedFirstBuild(t *testing.T) {
	t.Parallel()
	fail := true
	live := NewLive(func(context.Context) (*Catalog, error) {
		if fail {
			fail = false
			return nil, errors.New("index not ready")
		}
		return NewCatalog(nil, nil), nil
	}, time.Millisecond)
	defer live.Close()
	_, err := live.Catalog(context.Background())
	require.Error(t, err)
	c, err := live.Catalog(context.Background())
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestSearchStopsWhenCanceled(t *testing.T) {
	c := NewCatalog([]store.FinderNoteRow{{Path: "notes/roadmap.md", Title: "Roadmap"}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := c.Search(ctx, Query{Text: "roadmap"}); got != nil {
		t.Fatalf("canceled search returned %d results, want none", len(got))
	}
}
