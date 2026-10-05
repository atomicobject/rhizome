package actions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDeleteTagsIntegration(t *testing.T) {
	tests := []struct {
		name             string
		tagsToDelete     []string
		fileContent      string
		dryRun           bool
		expectedChanged  bool
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name:         "delete frontmatter tag",
			tagsToDelete: []string{"work"},
			fileContent: `---
title: Test Note
tags: [work, personal]
---
# Test Note
Some content here.`,
			expectedChanged:  true,
			shouldContain:    []string{"personal"},
			shouldNotContain: []string{"work"},
		},
		{
			name:         "delete hashtag",
			tagsToDelete: []string{"work"},
			fileContent: `# Test Note
This is about #work and other things.
More content here.`,
			expectedChanged:  true,
			shouldNotContain: []string{"#work"},
		},
		{
			name:             "ignore hashtags in code blocks",
			tagsToDelete:     []string{"work"},
			fileContent:      "# Test Note\nThis is about #work.\n\n```\n#work should not be deleted here\n```\n\nMore #work content.",
			expectedChanged:  true,
			shouldContain:    []string{"#work should not be deleted here"},
			shouldNotContain: []string{"This is about #work", "More #work content"},
		},
		{
			name:         "no changes when tag not found",
			tagsToDelete: []string{"nonexistent"},
			fileContent: `---
tags: [personal]
---
# Test Note
This is about #other things.`,
			expectedChanged: false,
		},
		{
			name:         "dry run reports planned change without writing",
			tagsToDelete: []string{"work"},
			fileContent: `---
tags: [work, personal]
---
# Test Note
This is about #work.`,
			dryRun:          true,
			expectedChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			testFile := filepath.Join(tempDir, "test.md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.fileContent), 0644))

			// Vault.Name doubles as the vault path in these fixtures.
			vault := &obsidian.Vault{Name: tempDir}
			note := &obsidian.Note{}

			summary, err := DeleteTags(t.Context(), vault, note, tt.tagsToDelete, tt.dryRun)
			assert.NoError(t, err)

			modifiedContent, err := os.ReadFile(testFile)
			assert.NoError(t, err)
			contentStr := string(modifiedContent)

			if tt.expectedChanged {
				assert.Equal(t, 1, summary.NotesTouched)
			} else {
				assert.Equal(t, 0, summary.NotesTouched)
			}
			if tt.dryRun || !tt.expectedChanged {
				assert.Equal(t, tt.fileContent, contentStr)
				return
			}
			for _, shouldContain := range tt.shouldContain {
				assert.Contains(t, contentStr, shouldContain, "Should contain: %s", shouldContain)
			}
			for _, shouldNotContain := range tt.shouldNotContain {
				assert.NotContains(t, contentStr, shouldNotContain, "Should not contain: %s", shouldNotContain)
			}
		})
	}
}

func TestRenameTagsIntegration(t *testing.T) {
	tests := []struct {
		name             string
		fromTags         []string
		toTag            string
		fileContent      string
		expectedChanged  bool
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name:     "rename frontmatter tag",
			fromTags: []string{"work"},
			toTag:    "office",
			fileContent: `---
title: Test Note
tags: [work, personal]
---
# Test Note
Some content here.`,
			expectedChanged:  true,
			shouldContain:    []string{"office", "personal"},
			shouldNotContain: []string{"work"},
		},
		{
			name:     "rename hashtag",
			fromTags: []string{"work"},
			toTag:    "office",
			fileContent: `# Test Note
This is about #work and other things.
More #work content here.`,
			expectedChanged:  true,
			shouldContain:    []string{"#office"},
			shouldNotContain: []string{"#work"},
		},
		{
			name:     "rename multiple tags to same destination",
			fromTags: []string{"work", "job"},
			toTag:    "office",
			fileContent: `---
tags: [work, personal, job]
---
# Test Note
Some content here.`,
			expectedChanged:  true,
			shouldContain:    []string{"office", "personal"},
			shouldNotContain: []string{"work", "job"},
		},
		{
			name:     "hierarchical rename in mixed content",
			fromTags: []string{"work"},
			toTag:    "project",
			fileContent: `---
tags: [work/frontend, work/backend, other]
---
# Test Note
Discussing #work/testing and #work/deployment but not #working.`,
			expectedChanged:  true,
			shouldContain:    []string{"project/frontend", "project/backend", "project/testing", "project/deployment", "#working"},
			shouldNotContain: []string{"work/frontend", "work/backend", "#work/testing", "#work/deployment"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tempDir, err := os.MkdirTemp("", "obsidian-test-*")
			assert.NoError(t, err)
			defer os.RemoveAll(tempDir)

			// Create test file
			testFile := filepath.Join(tempDir, "test.md")
			err = os.WriteFile(testFile, []byte(tt.fileContent), 0644)
			assert.NoError(t, err)

			// Create vault and note managers
			vault := &obsidian.Vault{Name: tempDir} // Hack for testing
			note := &obsidian.Note{}

			// Execute rename (not dry run)
			summary, err := RenameTags(t.Context(), vault, note, tt.fromTags, tt.toTag, false)
			assert.NoError(t, err)

			if tt.expectedChanged {
				assert.Greater(t, summary.NotesTouched, 0)

				// Read the modified file
				modifiedContent, err := os.ReadFile(testFile)
				assert.NoError(t, err)
				contentStr := string(modifiedContent)

				// Check expected content
				for _, shouldContain := range tt.shouldContain {
					assert.Contains(t, contentStr, shouldContain, "Should contain: %s", shouldContain)
				}
				for _, shouldNotContain := range tt.shouldNotContain {
					assert.NotContains(t, contentStr, shouldNotContain, "Should not contain: %s", shouldNotContain)
				}
			} else {
				assert.Equal(t, 0, summary.NotesTouched)
			}
		})
	}
}

func TestDeleteTagsValidation(t *testing.T) {
	vault := &obsidian.Vault{Name: "/fake/path"}
	note := &obsidian.Note{}

	tests := []struct {
		name         string
		tagsToDelete []string
		errorMessage string
	}{
		{
			name:         "empty tags list",
			tagsToDelete: []string{},
			errorMessage: "no tags specified for deletion",
		},
		{
			name:         "invalid tag with spaces",
			tagsToDelete: []string{"invalid tag"},
			errorMessage: "invalid tag",
		},
		{
			name:         "purely numeric tag",
			tagsToDelete: []string{"123"},
			errorMessage: "invalid tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DeleteTags(t.Context(), vault, note, tt.tagsToDelete, true)

			require.ErrorContains(t, err, tt.errorMessage)
		})
	}
}

func TestRenameTagsValidation(t *testing.T) {
	vault := &obsidian.Vault{Name: "/fake/path"}
	note := &obsidian.Note{}

	tests := []struct {
		name         string
		fromTags     []string
		toTag        string
		errorMessage string
	}{
		{
			name:         "empty from tags",
			fromTags:     []string{},
			toTag:        "office",
			errorMessage: "no source tags specified for rename",
		},
		{
			name:         "empty to tag",
			fromTags:     []string{"work"},
			toTag:        "",
			errorMessage: "destination tag cannot be empty",
		},
		{
			name:         "invalid source tag",
			fromTags:     []string{"invalid tag"},
			toTag:        "office",
			errorMessage: "invalid source tag",
		},
		{
			name:         "invalid destination tag",
			fromTags:     []string{"work"},
			toTag:        "invalid tag",
			errorMessage: "invalid destination tag",
		},
		{
			name:         "circular rename",
			fromTags:     []string{"work", "office"},
			toTag:        "work",
			errorMessage: "cannot rename tag work to itself",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RenameTags(t.Context(), vault, note, tt.fromTags, tt.toTag, true)

			require.ErrorContains(t, err, tt.errorMessage)
		})
	}
}

func TestAddTagsIntegration(t *testing.T) {
	const withWork = `---
title: Test Note
tags: [work]
---
# Test Note
Some content here.`
	const withWorkExisting = `---
title: Test Note
tags: [work, existing]
---
# Test Note
Some content here.`
	tests := []struct {
		name        string
		tagsToAdd   []string
		fileContent string
		dryRun      bool
		wantChanges map[string]int
		// wantTags, wantTitle and wantBody describe the written note; nil wantTags means original bytes remain.
		wantTags  []string
		wantTitle string
		wantBody  string
	}{
		{
			name:        "add tags to existing frontmatter",
			tagsToAdd:   []string{"urgent", "project"},
			fileContent: withWork,
			wantChanges: map[string]int{"urgent": 1, "project": 1},
			wantTags:    []string{"work", "urgent", "project"},
			wantTitle:   "Test Note",
			wantBody:    "# Test Note\nSome content here.",
		},
		{
			name:      "add tags to note without frontmatter",
			tagsToAdd: []string{"new-tag"},
			fileContent: `# Test Note
This is a note without frontmatter.`,
			wantChanges: map[string]int{"new-tag": 1},
			wantTags:    []string{"new-tag"},
			wantBody:    "# Test Note\nThis is a note without frontmatter.",
		},
		{
			name:        "avoid duplicate tags",
			tagsToAdd:   []string{"work", "duplicate"},
			fileContent: withWorkExisting,
			wantChanges: map[string]int{"duplicate": 1},
			wantTags:    []string{"work", "existing", "duplicate"},
			wantTitle:   "Test Note",
			wantBody:    "# Test Note\nSome content here.",
		},
		{
			name:        "no change when all tags already exist",
			tagsToAdd:   []string{"work", "existing"},
			fileContent: withWorkExisting,
		},
		{
			name:        "dry run reports planned change without writing",
			tagsToAdd:   []string{"new-tag"},
			fileContent: withWork,
			dryRun:      true,
			wantChanges: map[string]int{"new-tag": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			testFile := filepath.Join(tempDir, "test.md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.fileContent), 0644))

			vault := &obsidian.Vault{Name: tempDir}
			note := &obsidian.Note{}

			summary, err := AddTagsToFiles(t.Context(), vault, note, tt.tagsToAdd, []string{"test.md"}, tt.dryRun)
			require.NoError(t, err)

			if tt.wantChanges != nil {
				assert.Equal(t, 1, summary.NotesTouched)
				assert.Equal(t, tt.wantChanges, summary.TagChanges)
			} else {
				assert.Equal(t, 0, summary.NotesTouched)
			}

			actualContent, err := os.ReadFile(testFile)
			require.NoError(t, err)
			if tt.wantTags == nil {
				assert.Equal(t, tt.fileContent, string(actualContent))
				return
			}
			parts := strings.SplitN(string(actualContent), "---\n", 3)
			require.Len(t, parts, 3, "expected frontmatter in %q", actualContent)
			var frontmatter struct {
				Title string   `yaml:"title"`
				Tags  []string `yaml:"tags"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
			assert.Equal(t, tt.wantTags, frontmatter.Tags)
			assert.Equal(t, tt.wantTitle, frontmatter.Title)
			assert.Equal(t, tt.wantBody, parts[2])
		})
	}
}

func TestAddTagsWithEmptyTags(t *testing.T) {
	vault := &obsidian.Vault{Name: "/tmp"}
	note := &obsidian.Note{}

	for _, tt := range []struct {
		name  string
		tags  []string
		files []string
		want  string
	}{
		{name: "empty tags", tags: []string{}, files: []string{"test.md"}, want: "no tags specified"},
		{name: "empty files", tags: []string{"test"}, files: []string{}, want: "no files specified"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AddTagsToFiles(t.Context(), vault, note, tt.tags, tt.files, false)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestTagMutations_VaultInputFailuresAndEmptyResults(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(obsidian.VaultManager, obsidian.NoteReader, []string) (TagMutationSummary, error)
	}{
		{"add", func(v obsidian.VaultManager, n obsidian.NoteReader, tags []string) (TagMutationSummary, error) {
			return AddTagsWithWorkers(t.Context(), v, n, tags, true, 1)
		}},
		{"delete", func(v obsidian.VaultManager, n obsidian.NoteReader, tags []string) (TagMutationSummary, error) {
			return DeleteTagsWithWorkers(t.Context(), v, n, tags, true, 1)
		}},
		{"rename", func(v obsidian.VaultManager, n obsidian.NoteReader, tags []string) (TagMutationSummary, error) {
			return RenameTagsWithWorkers(t.Context(), v, n, tags, "destination", true, 1)
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			t.Run("invalid tags precede vault access", func(t *testing.T) {
				summary, err := operation.run(nil, nil, []string{"invalid tag"})
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid")
				require.Equal(t, TagMutationSummary{}, summary)
			})
			t.Run("vault failure precedes note listing", func(t *testing.T) {
				failure := errors.New("vault unavailable")
				vault := &MockVaultManager{}
				vault.On("Definition").Return(obsidian.VaultDefinition{}, failure).Once()
				summary, err := operation.run(vault, nil, []string{"old"})
				require.ErrorIs(t, err, failure)
				require.EqualError(t, err, "failed to get vault path: vault unavailable")
				require.Equal(t, TagMutationSummary{}, summary)
				vault.AssertExpectations(t)
			})
			t.Run("invalid root precedes note listing", func(t *testing.T) {
				vault := &MockVaultManager{}
				vault.On("Definition").Return(obsidian.VaultDefinition{}, nil).Once()
				summary, err := operation.run(vault, nil, []string{"old"})
				require.EqualError(t, err, `invalid vault path ""`)
				require.Equal(t, TagMutationSummary{}, summary)
				vault.AssertExpectations(t)
			})
			for _, listErr := range []error{nil, errors.New("listing unavailable")} {
				name := "empty note list"
				if listErr != nil {
					name = "note listing failure"
				}
				t.Run(name, func(t *testing.T) {
					definition := obsidian.VaultDefinition{Path: t.TempDir()}
					vault := &MockVaultManager{}
					vault.On("Definition").Return(definition, nil).Once()
					note := &MockNoteReader{}
					note.On("GetNotesList", definition).Return([]string(nil), listErr).Once()
					summary, err := operation.run(vault, note, []string{"old"})
					if listErr != nil {
						require.ErrorIs(t, err, listErr)
						require.EqualError(t, err, "failed to get notes list: listing unavailable")
						require.Equal(t, TagMutationSummary{}, summary)
					} else {
						require.NoError(t, err)
						require.Equal(t, TagMutationSummary{TagChanges: map[string]int{}, FilesChanged: []string{}}, summary)
					}
					vault.AssertExpectations(t)
					note.AssertExpectations(t)
				})
			}
		})
	}
}
