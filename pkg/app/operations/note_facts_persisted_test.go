package operations

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestPersistedNoteFactsRetainNestedMetadataAndAuthoredBody(t *testing.T) {
	source := "---\nsummary: source\n---\n# Body"
	facts, err := NewPersistedNoteFacts(semdb.DurableNoteFacts{
		MetadataRows:       map[string]semdb.NoteMetadataRow{"note.md": {Path: "note.md", FormatID: "markdown"}},
		ProjectionPayloads: map[string]string{"note.md": `{"Frontmatter":{"summary":{"text":"Keep me","count":3},"tags":["planning"]},"InlineProps":{"owner":["Drew"]},"Tags":["planning"]}`},
		Tags:               []semdb.NoteTagRow{{NotePath: "note.md", TagNorm: "planning"}},
		SearchRegions:      []semdb.NoteSearchRegionRow{{NotePath: "note.md", Origin: "authored", Kind: "visible", Text: source, MediaType: "text/markdown", RangePresent: true, EndByte: len(source)}},
	})
	require.NoError(t, err)
	fact, ok := facts.LookupFact("note.md")
	require.True(t, ok)
	require.Equal(t, map[string]any{"text": "Keep me", "count": 3}, fact.Frontmatter["summary"])
	require.Equal(t, []string{"Drew"}, fact.InlineProps["owner"])
	require.Equal(t, source, fact.Content)
	require.Equal(t, []string{"planning"}, fact.Tags)
}

func TestIndexedLinkCacheUsesSharedFoldedResolutionWithoutDroppingClaimants(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "links.sqlite"))
	require.NoError(t, err)
	defer store.Close()
	sources := map[string]string{"a/Foo.md": "---\naliases: [Alpha]\n---\nA", "b/foo.md": "---\naliases: [alpha]\n---\nB", "notes/Cafe\u0301.md": "Café"}
	reader := &indexedLinkTestReader{sources: sources}
	_, err = testkit.NoteMetadataIndexer(t).EnsureIndexed(ctx, obsidian.VaultDefinition{Path: t.TempDir()}, reader, store)
	require.NoError(t, err)
	cache, _, err := extendIndexedNotePathCache(ctx, store, nil, []string{"[[Foo]] [[FOO]] [[Alpha]] [[ALPHA]] [[CAFÉ]]"})
	require.NoError(t, err)
	for input, want := range map[string]string{"Foo": "a/Foo.md", "foo": "b/foo.md", "Alpha": "a/Foo.md", "alpha": "b/foo.md", "CAFÉ": "notes/Cafe\u0301.md"} {
		got, ok := cache.ResolveNote(input)
		require.True(t, ok, input)
		require.Equal(t, want, got)
	}
	_, ok := cache.ResolveNote("FOO")
	require.False(t, ok)
	_, ok = cache.ResolveNote("ALPHA")
	require.False(t, ok)
}

type indexedLinkTestReader struct {
	obsidian.Note
	sources map[string]string
}

func (r *indexedLinkTestReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	var out []string
	for path := range r.sources {
		out = append(out, path)
	}
	return out, nil
}
func (r *indexedLinkTestReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	return r.sources[path], nil
}
func (r *indexedLinkTestReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Unix(1, 0), nil
}
