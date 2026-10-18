package obsidian

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNotePathCacheTitlePreservesDottedNamesAcrossNoteFormats(t *testing.T) {
	for _, path := range []string{"notes/release.v2.md", "notes/release.v2.html"} {
		cache := BuildNotePathCacheWithAliases([]string{path}, nil)
		title, found := cache.Title(path)
		require.True(t, found)
		require.Equal(t, "release.v2", title)
	}
}
