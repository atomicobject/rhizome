package obsidian

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type healthContextReader struct {
	notes       []string
	contents    map[string]string
	readErrors  map[string]error
	blockReads  bool
	readStarted chan string
}

func (r *healthContextReader) GetContents(_ VaultDefinition, path string) (string, error) {
	return r.contents[path], r.readErrors[path]
}

func (r *healthContextReader) GetContentsContext(ctx context.Context, _ VaultDefinition, path string) (string, error) {
	if r.readStarted != nil {
		r.readStarted <- path
	}
	if r.blockReads {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return r.GetContents(VaultDefinition{}, path)
}

func (r *healthContextReader) GetNotesList(_ VaultDefinition) ([]string, error) {
	return r.notes, nil
}

func (r *healthContextReader) GetNotesListContext(ctx context.Context, _ VaultDefinition) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.notes, nil
}

func (r *healthContextReader) GetModTime(VaultDefinition, string) (time.Time, error) {
	return time.Time{}, nil
}

func (r *healthContextReader) Title(path string) (string, bool) { return path, true }

func TestFindBrokenLinksContextPreservesUnreadableSourceEvidence(t *testing.T) {
	reader := &healthContextReader{
		notes:      []string{"source.md", "target.md"},
		contents:   map[string]string{"target.md": "# Target\n"},
		readErrors: map[string]error{"source.md": errors.New("permission denied")},
	}

	broken, err := FindBrokenLinksContext(context.Background(), VaultDefinition{Name: "test"}, reader, DefaultBrokenLinksOptions)
	assert.Empty(t, broken)
	var readErrors NoteReadErrors
	require.ErrorAs(t, err, &readErrors)
	require.Len(t, readErrors, 1)
	var readErr *NoteReadError
	require.ErrorAs(t, err, &readErr)
	assert.Equal(t, "source.md", readErr.Source)
	assert.Empty(t, readErr.Target)
	assert.ErrorContains(t, err, "permission denied")
}

func TestFindBrokenLinksContextDoesNotInventFragmentFindingsForUnreadableTarget(t *testing.T) {
	reader := &healthContextReader{
		notes:      []string{"source.md", "target.md"},
		contents:   map[string]string{"source.md": "[[target#Missing]]"},
		readErrors: map[string]error{"target.md": errors.New("unreadable target")},
	}

	broken, err := FindBrokenLinksContext(context.Background(), VaultDefinition{Name: "test"}, reader, DefaultBrokenLinksOptions)
	assert.Empty(t, broken)
	var readErrors NoteReadErrors
	require.ErrorAs(t, err, &readErrors)
	require.Len(t, readErrors, 1)
	var readErr *NoteReadError
	require.ErrorAs(t, err, &readErr)
	assert.Equal(t, "source.md", readErr.Source)
	assert.Equal(t, "target.md", readErr.Target)
	assert.ErrorContains(t, err, "unreadable target")
}

func TestFindBrokenLinksContextPropagatesCancellationThroughContentReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &healthContextReader{
		notes:       []string{"source.md", "target.md"},
		contents:    map[string]string{"source.md": "[[target]]"},
		blockReads:  true,
		readStarted: make(chan string, 1),
	}
	result := make(chan error, 1)
	go func() {
		_, err := FindBrokenLinksContext(ctx, VaultDefinition{Name: "test"}, reader, DefaultBrokenLinksOptions)
		result <- err
	}()
	<-reader.readStarted
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase conversion",
			input:    "Meeting Notes",
			expected: "meeting notes",
		},
		{
			name:     "already lowercase",
			input:    "meeting notes",
			expected: "meeting notes",
		},
		{
			name:     "remove punctuation",
			input:    "Meeting-Notes!",
			expected: "meetingnotes",
		},
		{
			name:     "collapse whitespace",
			input:    "Meeting   Notes",
			expected: "meeting notes",
		},
		{
			name:     "remove .md extension",
			input:    "Meeting Notes.md",
			expected: "meeting notes",
		},
		{
			name:     "complex case",
			input:    "Meeting   Notes (Draft)!.md",
			expected: "meeting notes draft",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only punctuation",
			input:    "...",
			expected: "",
		},
		{
			name:     "path with directories",
			input:    "folder/Meeting Notes.md",
			expected: "foldermeeting notes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindBrokenLinks(t *testing.T) {
	// Use the health test fixtures
	fixturesPath := paths.ResolveSymlinks("../../../mocks/vaults/health").String()
	fixturesDef := VaultDefinition{Name: "health", Path: fixturesPath}

	tests := []struct {
		name            string
		vaultDef        VaultDefinition
		options         BrokenLinksOptions
		expectBroken    []string // Expected broken link targets
		expectNotBroken []string // Expected valid link targets
	}{
		{
			name:     "detects basic broken link",
			vaultDef: fixturesDef,
			options:  DefaultBrokenLinksOptions,
			expectBroken: []string{
				"Does Not Exist",
				"Missing Embed",
				"Another Missing",
			},
			expectNotBroken: []string{
				"target", // This resolves to target.md
			},
		},
		{
			name:     "skip embeds option",
			vaultDef: fixturesDef,
			options: BrokenLinksOptions{
				WikilinkOptions: WikilinkOptions{
					SkipEmbeds: true,
				},
			},
			expectBroken: []string{
				"Does Not Exist",
				"Another Missing",
			},
			expectNotBroken: []string{
				"Missing Embed", // Should not appear when skipping embeds
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note := &Note{}
			broken, err := FindBrokenLinks(tt.vaultDef, note, tt.options)
			assert.NoError(t, err)

			// Extract broken targets for comparison
			brokenTargets := make(map[string]bool)
			for _, bl := range broken {
				brokenTargets[bl.Target] = true
			}

			// Verify expected broken links are found
			for _, expected := range tt.expectBroken {
				assert.True(t, brokenTargets[expected], "Expected broken link to %q not found", expected)
			}

			if tt.name == "detects basic broken link" {
				require.Len(t, broken, 3)
				types := map[string]BacklinkType{}
				for _, link := range broken {
					types[link.Target] = link.LinkType
				}
				require.Equal(t, BacklinkTypeEmbed, types["Missing Embed"])
				require.Equal(t, BacklinkTypeAlias, types["Another Missing"])
			}

			// Verify expected valid links are NOT in broken list
			for _, notExpected := range tt.expectNotBroken {
				assert.False(t, brokenTargets[notExpected], "Link to %q should not be broken", notExpected)
			}
		})
	}
	t.Run("image filtering", func(t *testing.T) {
		root := t.TempDir()
		targets := []string{"picture.png", "picture.jpg", "picture.jpeg", "picture.gif", "picture.webp", "picture.svg", "Missing Note", "Missing.md", "document.pdf"}
		content := strings.Builder{}
		for _, target := range targets {
			content.WriteString("[[" + target + "]]\n")
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte(content.String()), 0o644))
		def := VaultDefinition{Name: "images", Path: root}
		for _, tc := range []struct {
			name    string
			include bool
			want    []string
		}{
			{"default", false, targets[6:]},
			{"including images", true, targets},
		} {
			t.Run(tc.name, func(t *testing.T) {
				options := DefaultBrokenLinksOptions
				options.IncludeImages = tc.include
				broken, err := FindBrokenLinks(def, &Note{}, options)
				require.NoError(t, err)
				got := make([]string, len(broken))
				for i, link := range broken {
					got[i] = link.Target
					require.Equal(t, BrokenLinkReasonNoteMissing, link.Reason)
					require.Equal(t, BacklinkTypeBasic, link.LinkType)
				}
				require.ElementsMatch(t, tc.want, got)
			})
		}
	})

}

func TestFindBrokenLinksResolvesFrontmatterAliasTargets(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "people"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "people", "Drew Colthorp.md"), []byte(`---
aliases:
  - Drew
---

# Drew Colthorp
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte(`# Source

- [ ] Follow up #action-item
  assignee:: [[Drew]]
  reviewer:: [[Drew Colthorp|Drew]]
`), 0o644))

	broken, err := FindBrokenLinks(VaultDefinition{Name: "aliases", Path: root}, &Note{}, DefaultBrokenLinksOptions)
	assert.NoError(t, err)
	require.Empty(t, broken)
}

func TestFindBrokenLinksConcurrentScanKeepsNoteOrder(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"01.md": "# One\n[[Missing One]]\n",
		"02.md": "# Two\n[[Missing Two]]\n",
		"03.md": "# Three\n[[Missing Three]]\n",
		"04.md": "# Four\n[[01]]\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}

	broken, err := FindBrokenLinks(VaultDefinition{Name: "ordered", Path: root}, &Note{}, DefaultBrokenLinksOptions)
	require.NoError(t, err)
	require.Len(t, broken, 3)
	assert.Equal(t, "01.md", broken[0].Source)
	assert.Equal(t, "Missing One", broken[0].Target)
	assert.Equal(t, "02.md", broken[1].Source)
	assert.Equal(t, "Missing Two", broken[1].Target)
	assert.Equal(t, "03.md", broken[2].Source)
	assert.Equal(t, "Missing Three", broken[2].Target)
}

func TestFindDeadEnds(t *testing.T) {
	tests := []struct {
		name          string
		analysis      *GraphAnalysis
		expectedCount int
		expectedPaths []string
	}{
		{
			name: "single dead end",
			analysis: &GraphAnalysis{
				Nodes: map[string]GraphNode{
					"stub.md":   {Path: "stub.md", Inbound: 2, Outbound: 0},
					"linker.md": {Path: "linker.md", Inbound: 0, Outbound: 1},
				},
			},
			expectedCount: 1,
			expectedPaths: []string{"stub.md"},
		},
		{
			name: "no dead ends",
			analysis: &GraphAnalysis{
				Nodes: map[string]GraphNode{
					"note1.md": {Path: "note1.md", Inbound: 1, Outbound: 1},
					"note2.md": {Path: "note2.md", Inbound: 1, Outbound: 1},
				},
			},
			expectedCount: 0,
			expectedPaths: []string{},
		},
		{
			name: "orphan is not dead end",
			analysis: &GraphAnalysis{
				Nodes: map[string]GraphNode{
					"orphan.md": {Path: "orphan.md", Inbound: 0, Outbound: 0},
				},
			},
			expectedCount: 0,
			expectedPaths: []string{},
		},
		{
			name: "multiple dead ends",
			analysis: &GraphAnalysis{
				Nodes: map[string]GraphNode{
					"stub1.md":  {Path: "stub1.md", Inbound: 3, Outbound: 0},
					"stub2.md":  {Path: "stub2.md", Inbound: 1, Outbound: 0},
					"active.md": {Path: "active.md", Inbound: 2, Outbound: 5},
				},
			},
			expectedCount: 2,
			expectedPaths: []string{"stub1.md", "stub2.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deadEnds := FindDeadEnds(tt.analysis)
			assert.Len(t, deadEnds, tt.expectedCount)

			foundPaths := make(map[string]bool)
			for _, de := range deadEnds {
				foundPaths[de.Path] = true
			}

			for _, expected := range tt.expectedPaths {
				assert.True(t, foundPaths[expected], "Expected dead-end %q not found", expected)
			}
		})
	}
}

func TestFindStaleNotes(t *testing.T) {
	root := t.TempDir()
	now := time.Now().Truncate(time.Second)
	oldTime := now.Add(-100 * 24 * time.Hour)
	for name, modified := range map[string]time.Time{
		"old.md":    oldTime,
		"recent.md": now.Add(-10 * 24 * time.Hour),
	} {
		path := filepath.Join(root, name)
		require.NoError(t, os.WriteFile(path, []byte("# Note\n"), 0o644))
		require.NoError(t, os.Chtimes(path, modified, modified))
	}

	stale, err := FindStaleNotes(VaultDefinition{Name: "health", Path: root}, &Note{}, 90)
	require.NoError(t, err)
	require.Len(t, stale, 1)
	assert.Equal(t, "old.md", stale[0].Path)
	assert.WithinDuration(t, oldTime, stale[0].LastModified, time.Second)
	assert.Equal(t, 100, stale[0].DaysSinceModification)
}

func TestFindMergeSuggestions(t *testing.T) {
	tests := []struct {
		name          string
		notes         []string
		expectedPairs [][2]string // Expected merge pairs
		expectedCount int
	}{
		{
			name:  "case difference suggests merge",
			notes: []string{"Meeting Notes.md", "meeting notes.md"},
			expectedPairs: [][2]string{
				{"Meeting Notes.md", "meeting notes.md"},
			},
			expectedCount: 1,
		},
		{
			name:          "no duplicates",
			notes:         []string{"alpha.md", "beta.md", "gamma.md"},
			expectedPairs: [][2]string{},
			expectedCount: 0,
		},
		{
			name:          "whitespace difference",
			notes:         []string{"Meeting Notes.md", "MeetingNotes.md"},
			expectedPairs: [][2]string{},
			expectedCount: 0, // These normalize differently: "meeting notes" vs "meetingnotes"
		},
		{
			name:          "punctuation difference",
			notes:         []string{"Meeting-Notes.md", "Meeting Notes.md"},
			expectedPairs: [][2]string{},
			expectedCount: 0, // "meetingnotes" vs "meeting notes"
		},
		{
			name:  "three-way duplicate",
			notes: []string{"Notes.md", "NOTES.md", "notes.md"},
			expectedPairs: [][2]string{
				{"Notes.md", "NOTES.md"},
				{"Notes.md", "notes.md"},
				{"NOTES.md", "notes.md"},
			},
			expectedCount: 3,
		},
		{
			name:  "different directories same name",
			notes: []string{"folder1/notes.md", "folder2/Notes.md"},
			expectedPairs: [][2]string{
				{"folder1/notes.md", "folder2/Notes.md"},
			},
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions := FindMergeSuggestions(tt.notes)
			assert.Len(t, suggestions, tt.expectedCount)

			// Verify expected pairs are found
			for _, expectedPair := range tt.expectedPairs {
				found := false
				for _, s := range suggestions {
					if (s.Note1 == expectedPair[0] && s.Note2 == expectedPair[1]) ||
						(s.Note1 == expectedPair[1] && s.Note2 == expectedPair[0]) {
						found = true
						assert.Equal(t, 1.0, s.Similarity)
						assert.Equal(t, "normalized name match", s.Reason)
						break
					}
				}
				assert.True(t, found, "Expected merge pair %v not found", expectedPair)
			}
		})
	}
}

// TestFindBrokenLinks_FragmentReasons guards SPEC-0023.US4.AC3/AC4 substrate:
// FindBrokenLinks must classify breakage so the orphan check can treat
// typo'd-fragment inbound references as soft holds rather than as
// "no inbound reference at all". Three cases:
//   - target note exists, fragment `^id` resolves            → not broken
//   - target note exists, fragment `^id` does NOT resolve    → block_missing
//   - target note exists, heading-text fragment NOT present  → heading_missing
//   - target note missing                                    → note_missing (legacy code path)
//
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
func TestFindBrokenLinks_FragmentReasons(t *testing.T) {
	root := t.TempDir()
	mustWriteFile := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mustWriteFile("target.md", `# Target

## Heading One

Some prose.
^block-a

### Story
status:: TODO
^story-a
`)
	mustWriteFile("source.md", `# Source

- [[target#^block-a]]                resolved block ref
- [[target#^BLOCK-A]]                case-mismatched block ref
- [[target#^typo-block]]              typo'd block ref
- [[target#Heading One]]              resolved heading ref
- [[target#Heading Missing]]          missing heading
- [[no-such-note]]                    missing note
- [[no-such-note#^anything]]          missing note + fragment
`)

	vaultDef := VaultDefinition{Name: "fragment", Path: root}
	note := &Note{}
	broken, err := FindBrokenLinks(vaultDef, note, DefaultBrokenLinksOptions)
	if err != nil {
		t.Fatalf("FindBrokenLinks: %v", err)
	}

	type seen struct {
		target   string
		fragment string
		reason   BrokenLinkReason
	}
	got := make([]seen, 0, len(broken))
	for _, link := range broken {
		got = append(got, seen{link.Target, link.Fragment, link.Reason})
	}
	require.ElementsMatch(t, []seen{
		{"target", "^BLOCK-A", BrokenLinkReasonBlockMissing},
		{"target", "^typo-block", BrokenLinkReasonBlockMissing},
		{"target", "Heading Missing", BrokenLinkReasonHeadingMissing},
		{"no-such-note", "", BrokenLinkReasonNoteMissing},
		{"no-such-note", "^anything", BrokenLinkReasonNoteMissing},
	}, got)

}

func TestFindBrokenLinks_ChecksSameNoteFragments(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte(`# Here

Paragraph ^Block

- [[#Here]]
- [[#Missing]]
- [[#^Block]]
- [[#^block]]
`), 0o644))

	broken, err := FindBrokenLinks(VaultDefinition{Name: "same-note", Path: root}, &Note{}, DefaultBrokenLinksOptions)
	require.NoError(t, err)
	require.ElementsMatch(t, []BrokenLink{
		{Source: "source.md", LinkType: BacklinkTypeHeading, Fragment: "Missing", Reason: BrokenLinkReasonHeadingMissing, Line: 6, Raw: "[[#Missing]]"},
		{Source: "source.md", LinkType: BacklinkTypeBlock, Fragment: "^block", Reason: BrokenLinkReasonBlockMissing, Line: 8, Raw: "[[#^block]]"},
	}, broken)
}
