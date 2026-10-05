package obsidian

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockNoteReader is a mock implementation of NoteReader for testing
type MockNoteReader struct {
	mock.Mock
}

func TestAliasListFromFrontmatterAcceptsStringerScalarsAndListItems(t *testing.T) {
	aliasTime := time.Date(2026, 8, 3, 9, 10, 11, 0, time.FixedZone("EDT", -4*60*60))
	got := AliasListFromFrontmatter(map[string]any{
		"aliases": []any{"  plain  ", aliasTime, "", 4},
	})
	want := []string{"plain", aliasTime.String()}
	if !assert.Equal(t, want, got) {
		t.Fatalf("aliases = %#v, want %#v", got, want)
	}
	if got := AliasListFromFrontmatter(map[string]any{"aliases": aliasTime}); !assert.Equal(t, []string{aliasTime.String()}, got) {
		t.Fatalf("scalar Stringer aliases = %#v", got)
	}
}

func (m *MockNoteReader) GetContents(def VaultDefinition, notePath string) (string, error) {
	args := m.Called(def, notePath)
	return args.String(0), args.Error(1)
}

func (m *MockNoteReader) GetNotesList(def VaultDefinition) ([]string, error) {
	args := m.Called(def)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockNoteReader) GetModTime(def VaultDefinition, notePath string) (time.Time, error) {
	args := m.Called(def, notePath)
	return args.Get(0).(time.Time), args.Error(1)
}

func (m *MockNoteReader) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}

type snapshotNoteReader struct {
	entries []NoteEntry
}

func (n *snapshotNoteReader) GetContents(VaultDefinition, string) (string, error) {
	return "", errors.New("unexpected content read")
}

func (n *snapshotNoteReader) GetNotesList(VaultDefinition) ([]string, error) {
	out := make([]string, 0, len(n.entries))
	for _, entry := range n.entries {
		out = append(out, entry.Path)
	}
	return out, nil
}

func (n *snapshotNoteReader) GetModTime(VaultDefinition, string) (time.Time, error) {
	return time.Time{}, nil
}

func (n *snapshotNoteReader) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}

func (n *snapshotNoteReader) NoteEntriesSnapshot(context.Context) ([]NoteEntry, error) {
	out := make([]NoteEntry, 0, len(n.entries))
	for _, entry := range n.entries {
		out = append(out, entry)
	}
	return out, nil
}

func TestExtractWikilinks(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "multiple wikilinks",
			content: "Link to [[Project]] and [[Todo List]]",
			want:    []string{"Project", "Todo List"},
		},
		{
			name:    "no wikilinks",
			content: "Just regular text",
			want:    []string{},
		},
		{
			name:    "wikilink with spaces",
			content: "Link to [[Project Notes]] and [[Todo List 2024]]",
			want:    []string{"Project Notes", "Todo List 2024"},
		},
		{
			name:    "wikilink with path",
			content: "Link to [[folder/Project]] and [[subfolder/Todo List]]",
			want:    []string{"folder/Project", "subfolder/Todo List"},
		},
		{
			name:    "wikilink with alias",
			content: "Link to [[Project|My Project]] and [[Todo List|Tasks]]",
			want:    []string{"Project", "Todo List"},
		},
		{
			name:    "wikilink with both path and alias",
			content: "Link to [[folder/Project|My Project]] and [[subfolder/Todo List|Tasks]]",
			want:    []string{"folder/Project", "subfolder/Todo List"},
		},
		{
			name:    "wikilinks in code block",
			content: "```\n[[Project]]\n```\nOutside [[Real Link]]",
			want:    []string{"Project", "Real Link"}, // Note: Currently extracts from code blocks too
		},
		{
			name:    "wikilinks with file extension",
			content: "Link to [[Project.md]] and [[Todo List.md]]",
			want:    []string{"Project.md", "Todo List.md"},
		},
		{
			name:    "wikilinks with heading",
			content: "Link to [[Project#section]] and [[Todo List#details]]",
			want:    []string{"Project#section", "Todo List#details"},
		},
		{
			name:    "wikilinks with special characters",
			content: "Link to [[Project-2023]] and [[Todo_List]]",
			want:    []string{"Project-2023", "Todo_List"},
		},
		{
			name:    "wikilinks in complex text",
			content: "# Header\n\nParagraph with [[Link1]] and [[Link2]]\n\n> Quote with [[Link3]]\n\n- List item with [[Link4]]\n",
			want:    []string{"Link1", "Link2", "Link3", "Link4"},
		},
		{
			name:    "nested wikilinks don't exist in Obsidian",
			content: "Link to [[Outer [[Inner]]]]",
			want:    []string{"Outer [[Inner"}, // This is expected behavior in Obsidian
		},
		{
			name:    "adjacent wikilinks",
			content: "Link to [[Link1]][[Link2]]",
			want:    []string{"Link1", "Link2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractWikilinks(tt.content, DefaultWikilinkOptions)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestBuildNotePathCache(t *testing.T) {
	tests := []struct {
		name         string
		notes        []string
		expectedPath string
		lookupKey    string
	}{
		{
			name:         "basic path resolution",
			notes:        []string{"folder/note1.md", "folder/note2.md"},
			lookupKey:    "note1",
			expectedPath: "folder/note1.md",
		},
		{
			name:         "resolve with full path",
			notes:        []string{"folder/note1.md", "subfolder/note1.md"},
			lookupKey:    "folder/note1",
			expectedPath: "folder/note1.md",
		},
		{
			name:         "note with spaces",
			notes:        []string{"folder/My Note.md", "folder/Other Note.md"},
			lookupKey:    "My Note",
			expectedPath: "folder/My Note.md",
		},
		{name: "wrong folder falls back to unique basename", notes: []string{"folder/note1.md"}, lookupKey: "subfolder/note1", expectedPath: "folder/note1.md"},
		{
			name:      "missing note",
			notes:     []string{"folder/note1.md"},
			lookupKey: "note2",
		},
		{
			name:         "lookup with extension",
			notes:        []string{"folder/note1.md", "folder/note2.md"},
			lookupKey:    "note1.md",
			expectedPath: "folder/note1.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := BuildNotePathCache(tt.notes)

			// Verify specific path resolution
			path, exists := cache.ResolveNote(tt.lookupKey)
			assert.Equal(t, tt.expectedPath != "", exists, "lookup %q", tt.lookupKey)
			assert.Equal(t, tt.expectedPath, path)
		})
	}
}

func TestBuildNotePathCache_BasenameCollisionRetainsCandidatesAndRefusesAmbiguity(t *testing.T) {
	cache := BuildNotePathCache([]string{
		"folder/subfolder/note1.md",
		"folder/note1.md",
	})

	_, ok := cache.ResolveNote("note1")
	assert.False(t, ok)
	_, ok = cache.ResolveNoteTarget("note1#Details")
	assert.False(t, ok)
	assert.Equal(t, []ResolvedNoteTarget{
		{Path: "folder/note1.md", Fragment: "Details"},
		{Path: "folder/subfolder/note1.md", Fragment: "Details"},
	}, cache.ResolveNoteCandidates("note1#Details"))

	path, ok := cache.ResolveNote("folder/note1")
	assert.True(t, ok)
	assert.Equal(t, "folder/note1.md", path)
}

func TestBuildNotePathCacheWithAliasesResolvesIdentifierAliases(t *testing.T) {
	notes := []string{
		"docs/specs/product/001-indexed-search.md",
		"docs/specs/product/002-rename-backlinks.md",
		"notes/project.md",
	}
	aliases := map[string][]string{
		"docs/specs/product/001-indexed-search.md":   {"SPEC-001", "spec-indexed-search"},
		"docs/specs/product/002-rename-backlinks.md": {"SPEC-002"},
	}
	cache := BuildNotePathCacheWithAliases(notes, aliases)

	t.Run("alias metadata is retained", func(t *testing.T) {
		assert.Equal(t, []string{"docs/specs/product/001-indexed-search.md"}, cache.Aliases["SPEC-001"])
		assert.Equal(t, []string{"docs/specs/product/001-indexed-search.md"}, cache.Aliases["spec-indexed-search"])
	})

	t.Run("alias resolves as wikilink target", func(t *testing.T) {
		path, ok := cache.ResolveNote("SPEC-001")
		assert.True(t, ok)
		assert.Equal(t, "docs/specs/product/001-indexed-search.md", path)
	})

	t.Run("alias with heading anchor preserves fragment", func(t *testing.T) {
		resolved, ok := cache.ResolveNoteTarget("SPEC-002#Requirements")
		assert.True(t, ok)
		assert.Equal(t, "docs/specs/product/002-rename-backlinks.md", resolved.Path)
		assert.Equal(t, "Requirements", resolved.Fragment)
	})

	t.Run("unknown alias misses", func(t *testing.T) {
		_, ok := cache.ResolveNote("SPEC-404")
		assert.False(t, ok)
	})
}

func TestResolveNote_RealFilenameShadowsAlias(t *testing.T) {
	notes := []string{
		"Foo.md",
		"notes/bar.md",
	}
	aliases := map[string][]string{
		"notes/bar.md": {"Foo"},
	}
	cache := BuildNotePathCacheWithAliases(notes, aliases)

	path, ok := cache.ResolveNote("Foo")
	assert.True(t, ok)
	assert.Equal(t, "Foo.md", path)
}

func TestResolveNote_UsesFilenameFallbackNotAliases(t *testing.T) {
	// [[subfolder/SPEC-001]] has a slash and falls through to the filename-only
	// fallback before alias resolution.
	notes := []string{
		"docs/specs/product/001-long-title.md",
		"other/SPEC-001.md",
	}
	aliases := map[string][]string{
		"docs/specs/product/001-long-title.md": {"SPEC-001"},
	}
	cache := BuildNotePathCacheWithAliases(notes, aliases)

	path, ok := cache.ResolveNote("SPEC-001")
	assert.True(t, ok)
	assert.Equal(t, "other/SPEC-001.md", path)

	path, ok = cache.ResolveNote("subfolder/SPEC-001")
	assert.True(t, ok)
	assert.Equal(t, "other/SPEC-001.md", path)
}

func TestBuildNotePathCacheWithAliases_CollisionRetainsCandidatesAndRefusesAmbiguity(t *testing.T) {
	notes := []string{
		"specs/a.md",
		"specs/b.md",
	}
	aliases := map[string][]string{
		"specs/a.md": {"DUPE"},
		"specs/b.md": {"DUPE"},
	}
	cache := BuildNotePathCacheWithAliases(notes, aliases)

	assert.Equal(t, []string{"specs/a.md", "specs/b.md"}, cache.Aliases["DUPE"])
	_, ok := cache.ResolveNote("DUPE")
	assert.False(t, ok)
	_, ok = cache.ResolveNoteTarget("DUPE#Requirements")
	assert.False(t, ok)
	assert.Equal(t, []ResolvedNoteTarget{
		{Path: "specs/a.md", Fragment: "Requirements"},
		{Path: "specs/b.md", Fragment: "Requirements"},
	}, cache.ResolveNoteCandidates("DUPE#Requirements"))
}

func TestBuildNotePathCacheWithAliases_DottedAliasIsNotAFileExtension(t *testing.T) {
	cache := BuildNotePathCacheWithAliases(
		[]string{"specs/a.md"},
		map[string][]string{"specs/a.md": {"SPEC-0042.US1"}},
	)

	assert.Equal(t, []ResolvedNoteTarget{{
		Path:     "specs/a.md",
		Fragment: "^acceptance",
	}}, cache.ResolveNoteCandidates("SPEC-0042.US1#^acceptance"))
}

func TestBuildNotePathCacheWithAliases_IgnoresBlankAliases(t *testing.T) {
	notes := []string{"specs/a.md"}
	aliases := map[string][]string{
		"specs/a.md": {"", "  ", "A-1"},
	}
	cache := BuildNotePathCacheWithAliases(notes, aliases)

	_, emptyOK := cache.ResolveNote("")
	assert.False(t, emptyOK)
	path, ok := cache.ResolveNote("A-1")
	assert.True(t, ok)
	assert.Equal(t, "specs/a.md", path)
	assert.Equal(t, []string{"specs/a.md"}, cache.Aliases["A-1"])
}

func TestNotePathCacheIncrementalUpdates(t *testing.T) {
	cache := BuildNotePathCacheWithAliases(
		[]string{"folder/subfolder/note1.md", "folder/note1.md", "specs/a.md"},
		map[string][]string{"specs/a.md": {"SPEC-A"}},
	)

	_, ok := cache.ResolveNote("note1")
	assert.False(t, ok)

	cache.Remove("folder/note1.md")
	path, ok := cache.ResolveNote("note1")
	assert.True(t, ok)
	assert.Equal(t, "folder/subfolder/note1.md", path)

	cache.AddOrUpdate("specs/b.md", []string{"SPEC-B"})
	path, ok = cache.ResolveNote("SPEC-B")
	assert.True(t, ok)
	assert.Equal(t, "specs/b.md", path)
	assert.Equal(t, []string{"specs/b.md"}, cache.Aliases["SPEC-B"])

	cache.AddOrUpdate("specs/a.md", []string{"SPEC-A2"})
	_, ok = cache.ResolveNote("SPEC-A")
	assert.False(t, ok)
	path, ok = cache.ResolveNote("SPEC-A2")
	assert.True(t, ok)
	assert.Equal(t, "specs/a.md", path)
	assert.Equal(t, []string{"specs/a.md"}, cache.Aliases["SPEC-A2"])

	cache.Remove("specs/a.md")
	_, ok = cache.ResolveNote("SPEC-A2")
	assert.False(t, ok)
}

func TestNotePathCacheIncrementalAliasCollisionsRetainAndReleaseCandidates(t *testing.T) {
	cache := BuildNotePathCacheWithAliases(
		[]string{"specs/a.md"},
		map[string][]string{"specs/a.md": {"DUPE"}},
	)

	cache.AddOrUpdate("specs/b.md", []string{"DUPE"})
	assert.Equal(t, []string{"specs/a.md", "specs/b.md"}, cache.Aliases["DUPE"])
	assert.Equal(t, []ResolvedNoteTarget{
		{Path: "specs/a.md"},
		{Path: "specs/b.md"},
	}, cache.ResolveNoteCandidates("DUPE"))
	_, ok := cache.ResolveNote("DUPE")
	assert.False(t, ok)

	cache.AddOrUpdate("specs/a.md", []string{"SPEC-A"})
	path, ok := cache.ResolveNote("DUPE")
	assert.True(t, ok)
	assert.Equal(t, "specs/b.md", path)
	assert.Equal(t, []ResolvedNoteTarget{{Path: "specs/b.md"}}, cache.ResolveNoteCandidates("DUPE"))

	cache.AddOrUpdate("specs/a.md", []string{"DUPE"})
	_, ok = cache.ResolveNote("DUPE")
	assert.False(t, ok)

	cache.Remove("specs/b.md")
	path, ok = cache.ResolveNote("DUPE")
	assert.True(t, ok)
	assert.Equal(t, "specs/a.md", path)
	assert.Equal(t, []string{"specs/a.md"}, cache.Aliases["DUPE"])
}

func TestNotePathCacheIncrementalBasenameCollisionsRetainAndReleaseCandidates(t *testing.T) {
	cache := BuildNotePathCache([]string{"deep/item.md"})

	cache.AddOrUpdate("shallow/item.md", nil)
	_, ok := cache.ResolveNote("item")
	assert.False(t, ok)
	assert.Equal(t, []ResolvedNoteTarget{
		{Path: "deep/item.md"},
		{Path: "shallow/item.md"},
	}, cache.ResolveNoteCandidates("item"))

	cache.Remove("deep/item.md")
	path, ok := cache.ResolveNote("item")
	assert.True(t, ok)
	assert.Equal(t, "shallow/item.md", path)

	cache.AddOrUpdate("deep/item.md", nil)
	_, ok = cache.ResolveNote("item")
	assert.False(t, ok)

	cache.Remove("shallow/item.md")
	path, ok = cache.ResolveNote("item")
	assert.True(t, ok)
	assert.Equal(t, "deep/item.md", path)
}

func TestFollowWikilinks(t *testing.T) {
	// Create test mocks
	mockNote := &MockNoteReader{}

	// Setup vault definition
	vaultDef := VaultDefinition{Name: "test", Path: "/test/vault"}

	// Notes list mock - used for building the cache
	mockNote.On("GetNotesList", mock.Anything).Return([]string{
		"note1.md",
		"note2.md",
		"folder/note3.md",
		"folder/note4.md",
	}, nil)

	// Content mocks - different wikilink structures
	mockNote.On("GetContents", mock.Anything, "note1.md").Return("Content with link to [[note2]]", nil)
	mockNote.On("GetContents", mock.Anything, "note2.md").Return("Content with link to [[folder/note3]]", nil)
	mockNote.On("GetContents", mock.Anything, "folder/note3.md").Return("Content with link to [[note4]]", nil)
	mockNote.On("GetContents", mock.Anything, "folder/note4.md").Return("Content with link to [[note1]] and [[non-existent]]", nil)

	tests := []struct {
		name        string
		startFile   string
		maxDepth    int
		expected    []string
		expectedErr bool
	}{
		{
			name:        "follow one level",
			startFile:   "note1.md",
			maxDepth:    1,
			expected:    []string{"note1.md", "note2.md"},
			expectedErr: false,
		},
		{
			name:        "follow two levels",
			startFile:   "note1.md",
			maxDepth:    2,
			expected:    []string{"note1.md", "note2.md", "folder/note3.md"},
			expectedErr: false,
		},
		{
			name:        "follow all levels",
			startFile:   "note1.md",
			maxDepth:    3,
			expected:    []string{"note1.md", "note2.md", "folder/note3.md", "folder/note4.md"},
			expectedErr: false,
		},
		{
			name:        "start from middle",
			startFile:   "note2.md",
			maxDepth:    2,
			expected:    []string{"note2.md", "folder/note3.md", "folder/note4.md"},
			expectedErr: false,
		},
		{
			name:        "depth 0 returns only starting file",
			startFile:   "note1.md",
			maxDepth:    0,
			expected:    []string{"note1.md"},
			expectedErr: false,
		},
		{
			name:        "handle circular references",
			startFile:   "folder/note4.md",
			maxDepth:    3,
			expected:    []string{"folder/note4.md", "note1.md", "note2.md", "folder/note3.md"},
			expectedErr: false,
		},
	}

	// Get all notes to build the cache
	allNotes, _ := mockNote.GetNotesList(vaultDef)
	cache := BuildNotePathCache(allNotes)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			visited := make(map[string]bool)
			options := FollowWikilinksOptions{
				WikilinkOptions: DefaultWikilinkOptions,
				MaxDepth:        tt.maxDepth,
			}
			result, err := FollowWikilinks(vaultDef, mockNote, tt.startFile, visited, cache, options)

			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.ElementsMatch(t, tt.expected, result)
			}
		})
	}

	// Test error case
	t.Run("error when getting content", func(t *testing.T) {
		mockNote := &MockNoteReader{}
		mockNote.On("GetContents", mock.Anything, "error.md").Return("", errors.New("content error"))

		visited := make(map[string]bool)
		options := DefaultFollowWikilinksOptions
		result, err := FollowWikilinks(vaultDef, mockNote, "error.md", visited, cache, options)

		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

func TestFollowWikilinksWithOptions(t *testing.T) {
	// Create test mocks
	mockNote := &MockNoteReader{}

	// Setup vault definition
	vaultDef := VaultDefinition{Name: "test", Path: "/test/vault"}

	// Notes list mock - used for building the cache
	mockNote.On("GetNotesList", mock.Anything).Return([]string{
		"note1.md",
		"note2.md",
		"note3.md",
		"folder/note4.md",
		"folder/note5.md",
	}, nil)

	// Content mocks - with some anchored links
	mockNote.On("GetContents", mock.Anything, "note1.md").Return("Content with link to [[note2]] and anchored [[note3#section]]", nil)
	mockNote.On("GetContents", mock.Anything, "note2.md").Return("Content with link to [[folder/note4#details]] and [[folder/note5]]", nil)
	mockNote.On("GetContents", mock.Anything, "note3.md").Return("Content with link to [[note1]]", nil)
	mockNote.On("GetContents", mock.Anything, "folder/note4.md").Return("Content with no links", nil)
	mockNote.On("GetContents", mock.Anything, "folder/note5.md").Return("Content with link back to [[note2]]", nil)

	// Get all notes to build the cache
	allNotes, _ := mockNote.GetNotesList(vaultDef)
	cache := BuildNotePathCache(allNotes)

	t.Run("follow with skipAnchors=true", func(t *testing.T) {
		visited := make(map[string]bool)
		options := FollowWikilinksOptions{
			WikilinkOptions: WikilinkOptions{
				SkipAnchors: true,
				SkipEmbeds:  false,
			},
			MaxDepth: 3,
		}
		result, err := FollowWikilinks(vaultDef, mockNote, "note1.md", visited, cache, options)

		assert.NoError(t, err)
		expected := []string{"note1.md", "note2.md", "folder/note5.md"}
		assert.ElementsMatch(t, expected, result)
	})

	t.Run("follow with skipAnchors=false", func(t *testing.T) {
		visited := make(map[string]bool)
		options := FollowWikilinksOptions{
			WikilinkOptions: WikilinkOptions{
				SkipAnchors: false,
				SkipEmbeds:  false,
			},
			MaxDepth: 3,
		}
		result, err := FollowWikilinks(vaultDef, mockNote, "note1.md", visited, cache, options)

		assert.NoError(t, err)
		// When skipAnchors=false, we follow all links including anchored ones
		expected := []string{"note1.md", "note2.md", "note3.md", "folder/note4.md", "folder/note5.md"}
		assert.ElementsMatch(t, expected, result)
	})

	t.Run("follow with unlimited depth", func(t *testing.T) {
		visited := make(map[string]bool)
		options := FollowWikilinksOptions{
			WikilinkOptions: DefaultWikilinkOptions,
			MaxDepth:        -1,
		}
		result, err := FollowWikilinks(vaultDef, mockNote, "note1.md", visited, cache, options)

		assert.NoError(t, err)
		expected := []string{"note1.md", "note2.md", "note3.md", "folder/note4.md", "folder/note5.md"}
		assert.ElementsMatch(t, expected, result)
	})
}

func TestExtractWikilinksOptions(t *testing.T) {
	const content = "Link to [[Regular Link]], anchored [[Anchored Link#section]], and embed ![[Embedded Link]]"

	tests := []struct {
		name    string
		options WikilinkOptions
		want    []string
	}{
		{
			name:    "extract all links",
			options: WikilinkOptions{SkipAnchors: false, SkipEmbeds: false},
			want:    []string{"Regular Link", "Anchored Link#section", "Embedded Link"},
		},
		{
			name:    "skip anchors",
			options: WikilinkOptions{SkipAnchors: true, SkipEmbeds: false},
			want:    []string{"Regular Link", "Embedded Link"},
		},
		{
			name:    "skip embeds",
			options: WikilinkOptions{SkipAnchors: false, SkipEmbeds: true},
			want:    []string{"Regular Link", "Anchored Link#section"},
		},
		{
			name:    "skip both anchors and embeds",
			options: WikilinkOptions{SkipAnchors: true, SkipEmbeds: true},
			want:    []string{"Regular Link"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractWikilinks(content, tt.options)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestCollectBacklinksHonorsSkipOptions(t *testing.T) {
	vaultPath := filepath.Join("..", "..", "..", "mocks", "vaults", "backlinks")
	vaultDef := VaultDefinition{Name: "backlinks", Path: vaultPath}
	note := &Note{}
	targets := []string{"target.md"}

	backlinksSkipNone, err := CollectBacklinks(vaultDef, note, targets, WikilinkOptions{SkipAnchors: false, SkipEmbeds: false}, nil)
	assert.NoError(t, err)
	// Expect all variants present
	if assert.Contains(t, backlinksSkipNone, "target.md") {
		assert.Len(t, backlinksSkipNone["target.md"], 7)
	}

	backlinksSkipAnchors, err := CollectBacklinks(vaultDef, note, targets, WikilinkOptions{SkipAnchors: true, SkipEmbeds: false}, nil)
	assert.NoError(t, err)
	if assert.Contains(t, backlinksSkipAnchors, "target.md") {
		for _, bl := range backlinksSkipAnchors["target.md"] {
			assert.NotEqual(t, BacklinkTypeHeading, bl.LinkType)
			assert.NotEqual(t, BacklinkTypeBlock, bl.LinkType)
		}
	}

	backlinksSkipEmbeds, err := CollectBacklinks(vaultDef, note, targets, WikilinkOptions{SkipAnchors: false, SkipEmbeds: true}, nil)
	assert.NoError(t, err)
	if assert.Contains(t, backlinksSkipEmbeds, "target.md") {
		for _, bl := range backlinksSkipEmbeds["target.md"] {
			assert.NotEqual(t, BacklinkTypeEmbed, bl.LinkType)
		}
	}
}

func TestDeduplicateResults(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no duplicates",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with duplicates",
			input:    []string{"a", "b", "a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty list",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "all duplicates",
			input:    []string{"a", "a", "a"},
			expected: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DeduplicateResults(tt.input)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already normalized",
			input:    "folder/note.md",
			expected: "folder/note.md",
		},
		{
			name:     "Windows path",
			input:    "folder\\note.md",
			expected: "folder/note.md",
		},
		{
			name:     "with leading ./",
			input:    "./folder/note.md",
			expected: "folder/note.md",
		},
		{
			name:     "with leading ../",
			input:    "../folder/note.md",
			expected: "folder/note.md",
		},
		{
			name:     "mixed path separators",
			input:    "folder\\subfolder/note.md",
			expected: "folder/subfolder/note.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCollectBacklinks(t *testing.T) {
	vaultPath := filepath.Join("..", "..", "..", "mocks", "vaults", "backlinks")
	vaultDef := VaultDefinition{Name: "backlinks", Path: vaultPath}
	note := &Note{}

	targets := []string{"target.md", "nolinks.md"}

	backlinks, err := CollectBacklinks(vaultDef, note, targets, WikilinkOptions{SkipAnchors: false, SkipEmbeds: false}, nil)
	assert.NoError(t, err)

	expected := map[string]map[string]BacklinkType{
		"target.md": {
			"ref-alias.md":             BacklinkTypeAlias,
			"ref-basic.md":             BacklinkTypeBasic,
			"ref-block.md#^blockid":    BacklinkTypeBlock,
			"ref-duplicate.md":         BacklinkTypeBasic,
			"ref-duplicate.md#Heading": BacklinkTypeHeading,
			"ref-embed.md":             BacklinkTypeEmbed,
			"ref-heading.md#Heading":   BacklinkTypeHeading,
		},
		"nolinks.md": {},
	}

	for target, expectedRefs := range expected {
		assert.Contains(t, backlinks, target)
		resultRefs := make(map[string]BacklinkType)
		for _, backlink := range backlinks[target] {
			key := NormalizePath(backlink.Referrer)
			if fragment := strings.TrimSpace(backlink.Fragment); fragment != "" {
				key += "#" + fragment
			}
			resultRefs[key] = backlink.LinkType
		}

		assert.Equal(t, len(expectedRefs), len(resultRefs))
		for referrer, linkType := range expectedRefs {
			assert.Equal(t, linkType, resultRefs[referrer], "target %s referrer %s", target, referrer)
		}
	}
}

func TestFollowWikilinksWithMarkdownLinks(t *testing.T) {
	mockNote := &MockNoteReader{}
	vaultDef := VaultDefinition{Name: "test", Path: "/test/vault", Links: LinkTypeBoth}

	// Setup mock expectations
	mockNote.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md", "note3.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "note1.md").Return("Link to [[note2]] and [note3](note3.md)", nil)
	mockNote.On("GetContents", mock.Anything, "note2.md").Return("Just text", nil)
	mockNote.On("GetContents", mock.Anything, "note3.md").Return("Also text", nil)

	allNotes, _ := mockNote.GetNotesList(vaultDef)
	cache := BuildNotePathCache(allNotes)

	visited := make(map[string]bool)
	options := FollowWikilinksOptions{
		WikilinkOptions: DefaultWikilinkOptions,
		MaxDepth:        1,
	}

	result, err := FollowWikilinks(vaultDef, mockNote, "note1.md", visited, cache, options)
	assert.NoError(t, err)

	// Should follow both wikilink and markdown link
	assert.Contains(t, result, "note1.md")
	assert.Contains(t, result, "note2.md")
	assert.Contains(t, result, "note3.md")
}

func TestCollectBacklinksWithMarkdownLinks(t *testing.T) {
	mockNote := &MockNoteReader{}
	vaultDef := VaultDefinition{Name: "test", Path: "/test/vault", Links: LinkTypeBoth}

	mockNote.On("GetNotesList", mock.Anything).Return([]string{"target.md", "ref-wiki.md", "ref-md.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "target.md").Return("Target content", nil)
	mockNote.On("GetContents", mock.Anything, "ref-wiki.md").Return("Links via [[target]]", nil)
	mockNote.On("GetContents", mock.Anything, "ref-md.md").Return("Links via [target](target.md)", nil)

	targets := []string{"target.md"}
	backlinks, err := CollectBacklinks(vaultDef, mockNote, targets, WikilinkOptions{}, nil)

	assert.NoError(t, err)
	assert.Contains(t, backlinks, "target.md")
	assert.Len(t, backlinks["target.md"], 2)

	referrers := make(map[string]bool)
	for _, bl := range backlinks["target.md"] {
		referrers[bl.Referrer] = true
	}
	assert.True(t, referrers["ref-wiki.md"], "Should find wikilink referrer")
	assert.True(t, referrers["ref-md.md"], "Should find markdown link referrer")
}

func TestCollectBacklinksMarkdownOnly(t *testing.T) {
	mockNote := &MockNoteReader{}
	// markdown-only vault
	vaultDef := VaultDefinition{Name: "test", Path: "/test/vault", Links: LinkTypeMarkdown}

	mockNote.On("GetNotesList", mock.Anything).Return([]string{"target.md", "ref-wiki.md", "ref-md.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "target.md").Return("Target content", nil)
	mockNote.On("GetContents", mock.Anything, "ref-wiki.md").Return("Links via [[target]]", nil)
	mockNote.On("GetContents", mock.Anything, "ref-md.md").Return("Links via [target](target.md)", nil)

	targets := []string{"target.md"}
	backlinks, err := CollectBacklinks(vaultDef, mockNote, targets, WikilinkOptions{}, nil)

	assert.NoError(t, err)
	assert.Contains(t, backlinks, "target.md")
	// Should only find markdown link, not wikilink
	assert.Len(t, backlinks["target.md"], 1)
	assert.Equal(t, "ref-md.md", backlinks["target.md"][0].Referrer)
}

func TestCollectBacklinksUsesSnapshotEntriesAndSuppressedTags(t *testing.T) {
	vaultDef := VaultDefinition{Name: "test", Path: "/test/vault", Links: LinkTypeBoth}
	note := &snapshotNoteReader{
		entries: []NoteEntry{
			{Path: "target.md", Content: "Target"},
			{Path: "ref-wiki.md", Content: "Links via [[target|alias]]"},
			{Path: "ref-md.md", Content: "Links via [target](target.md#heading)"},
			{Path: "ref-hidden.md", Content: "Links via ![](target.md)", Tags: []string{"no-prompt"}},
		},
	}

	backlinks, err := CollectBacklinks(vaultDef, note, []string{"target.md"}, WikilinkOptions{}, []string{"no-prompt"})
	assert.NoError(t, err)
	assert.Contains(t, backlinks, "target.md")
	assert.Len(t, backlinks["target.md"], 2)

	typeMap := make(map[string]BacklinkType)
	for _, backlink := range backlinks["target.md"] {
		typeMap[backlink.Referrer] = backlink.LinkType
	}
	assert.Equal(t, BacklinkTypeAlias, typeMap["ref-wiki.md"])
	assert.Equal(t, BacklinkTypeHeading, typeMap["ref-md.md"])
	_, hidden := typeMap["ref-hidden.md"]
	assert.False(t, hidden)
}

func TestCollectBacklinksKeepsDistinctFragmentsFromSameReferrer(t *testing.T) {
	mockNote := &MockNoteReader{}
	vaultDef := VaultDefinition{Name: "test", Path: "/test/vault", Links: LinkTypeBoth}

	mockNote.On("GetNotesList", mock.Anything).Return([]string{"target.md", "ref.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "target.md").Return("Target content", nil)
	mockNote.On("GetContents", mock.Anything, "ref.md").Return("Links [one](target.md#alpha) and [two](target.md#beta)", nil)

	backlinks, err := CollectBacklinks(vaultDef, mockNote, []string{"target.md"}, WikilinkOptions{}, nil)
	assert.NoError(t, err)
	assert.Contains(t, backlinks, "target.md")
	assert.Len(t, backlinks["target.md"], 2)

	fragments := make([]string, 0, len(backlinks["target.md"]))
	for _, backlink := range backlinks["target.md"] {
		fragments = append(fragments, backlink.Fragment)
	}
	assert.ElementsMatch(t, []string{"alpha", "beta"}, fragments)
}

func TestVaultDefinitionLinkSupport(t *testing.T) {
	tests := []struct {
		name          string
		links         string
		wikiSupported bool
		mdSupported   bool
	}{
		{"default (empty)", "", true, true},
		{"wikilinks only", LinkTypeWikilinks, true, false},
		{"markdown only", LinkTypeMarkdown, false, true},
		{"both", LinkTypeBoth, true, true},
		{"unknown defaults to both", "unknown", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := VaultDefinition{Links: tt.links}
			assert.Equal(t, tt.wikiSupported, def.SupportsWikilinks())
			assert.Equal(t, tt.mdSupported, def.SupportsMarkdownLinks())
		})
	}
}
