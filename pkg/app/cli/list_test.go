package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestParseInputs tests the ParseInputs function
func TestParseInputs(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedInputs []ListInput
		expectedError  bool
		errorContains  string
	}{
		{
			name:           "Empty args",
			args:           []string{},
			expectedInputs: nil,
			expectedError:  false,
		},
		{
			name: "Single file path",
			args: []string{"path/to/file.md"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeFile,
					Value: "path/to/file.md",
				},
			},
			expectedError: false,
		},
		{
			name: "Multiple file paths",
			args: []string{"path/to/file1.md", "path/to/file2.md"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeFile,
					Value: "path/to/file1.md",
				},
				{
					Type:  InputTypeFile,
					Value: "path/to/file2.md",
				},
			},
			expectedError: false,
		},
		{
			name: "Single tag",
			args: []string{"tag:project"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeTag,
					Value: "project",
				},
			},
			expectedError: false,
		},
		{
			name: "Tag with quotes",
			args: []string{`tag:"multi word"`},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeTag,
					Value: "multi word",
				},
			},
			expectedError: false,
		},
		{
			name: "Tag with single quotes",
			args: []string{"tag:'multi word'"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeTag,
					Value: "'multi word'",
				},
			},
			expectedError: false,
		},
		{
			name: "Multiple tags",
			args: []string{"tag:project", "tag:work"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeTag,
					Value: "project",
				},
				{
					Type:  InputTypeTag,
					Value: "work",
				},
			},
			expectedError: false,
		},
		{
			name: "Single find",
			args: []string{"find:note"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeFind,
					Value: "note",
				},
			},
			expectedError: false,
		},
		{
			name: "Find with quotes",
			args: []string{`find:"complex query"`},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeFind,
					Value: "complex query",
				},
			},
			expectedError: false,
		},
		{
			name: "Find with single quotes",
			args: []string{"find:'complex query'"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeFind,
					Value: "'complex query'",
				},
			},
			expectedError: false,
		},
		{
			name: "Mixed inputs",
			args: []string{"path/to/file.md", "tag:project", "find:note"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeFile,
					Value: "path/to/file.md",
				},
				{
					Type:  InputTypeTag,
					Value: "project",
				},
				{
					Type:  InputTypeFind,
					Value: "note",
				},
			},
			expectedError: false,
		},
		{
			name: "Special characters in inputs",
			args: []string{"path/to/file-with-dashes.md", "tag:project-2023", "find:note_123"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeFile,
					Value: "path/to/file-with-dashes.md",
				},
				{
					Type:  InputTypeTag,
					Value: "project-2023",
				},
				{
					Type:  InputTypeFind,
					Value: "note_123",
				},
			},
			expectedError: false,
		},
		{
			name: "Boolean operators ignored in inputs",
			args: []string{"tag:project", "AND", "tag:work"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeTag,
					Value: "project",
				},
				{
					Type:  InputTypeTag,
					Value: "work",
				},
			},
			expectedError: false,
		},
		{
			name: "Parenthesized boolean expression",
			args: []string{"(tag:project AND tag:work)"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeTag,
					Value: "project",
				},
				{
					Type:  InputTypeTag,
					Value: "work",
				},
			},
			expectedError: false,
		},
		{
			name: "Property input",
			args: []string{"Office:AOGR"},
			expectedInputs: []ListInput{
				{
					Type:     InputTypeProperty,
					Value:    "AOGR",
					Property: "Office",
				},
			},
			expectedError: false,
		},
		{
			name:          "Empty tag value",
			args:          []string{"tag:"},
			expectedError: true,
			errorContains: "tag cannot be empty",
		},
		{
			name:          "Empty find value",
			args:          []string{"find:"},
			expectedError: true,
			errorContains: "find cannot be empty",
		},
		{
			name:          "Tag with wildcard",
			args:          []string{"tag:*"},
			expectedError: true,
			errorContains: "tag cannot be empty or a wildcard",
		},
		{
			name:          "Find with wildcard",
			args:          []string{"find:*"},
			expectedError: true,
			errorContains: "find cannot be empty or a wildcard",
		},
		{
			name: "Malformed tag and find without colon",
			args: []string{"tagproject", "findnote"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeFile,
					Value: "tagproject",
				},
				{
					Type:  InputTypeFile,
					Value: "findnote",
				},
			},
			expectedError: false,
		},
		{
			name: "Wildcard in file path",
			args: []string{"*"},
			expectedInputs: []ListInput{
				{
					Type:  InputTypeFile,
					Value: "*",
				},
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs, err := ParseInputs(tt.args)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedInputs, inputs)
			}
		})
	}
}

// MockVaultManager is a mock implementation of VaultManager
type MockVaultManager struct {
	mock.Mock
}

func (m *MockVaultManager) Path() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockVaultManager) DefaultName() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockVaultManager) SetDefaultName(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *MockVaultManager) Definition() (obsidian.VaultDefinition, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return obsidian.VaultDefinition{}, args.Error(1)
	}
	return args.Get(0).(obsidian.VaultDefinition), args.Error(1)
}

// MockNoteReader is a mock implementation of NoteReader
type MockNoteReader struct {
	mock.Mock
}

func (m *MockNoteReader) GetContents(def obsidian.VaultDefinition, notePath string) (string, error) {
	args := m.Called(def, notePath)
	return args.String(0), args.Error(1)
}

func (m *MockNoteReader) GetNotesList(def obsidian.VaultDefinition) ([]string, error) {
	args := m.Called(def)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockNoteReader) GetModTime(def obsidian.VaultDefinition, notePath string) (time.Time, error) {
	args := m.Called(def, notePath)
	return args.Get(0).(time.Time), args.Error(1)
}

func (m *MockNoteReader) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}

func TestListFiles(t *testing.T) {
	tests := []struct {
		name             string
		mockVault        *mocks.VaultManager
		mockNote         *mocks.NoteReader
		params           ListParams
		facts            map[string]string
		setupMocks       func(*mocks.VaultManager, *mocks.NoteReader)
		validateResponse func(*testing.T, []string, error)
	}{
		{
			name:      "list with multiple input types",
			facts:     map[string]string{"folder/note1.md": "Regular content", "note2.md": "---\ntags: [tag1]\n---\nContent", "note3.md": "Content", "note4.md": "Content with #tag1"},
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{
					{Type: InputTypeTag, Value: "tag1"},
					{Type: InputTypeFile, Value: "folder/note1.md"},
					{Type: InputTypeFind, Value: "note3"},
				},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"folder/note1.md", "note2.md", "note3.md", "note4.md"}, nil)
				n.On("GetContents", mock.Anything, "folder/note1.md").Return("Regular content", nil).Maybe()
				n.On("GetContents", mock.Anything, "note2.md").Return("---\ntags: [tag1]\n---\nContent", nil).Maybe()
				n.On("GetContents", mock.Anything, "note3.md").Return("Content", nil).Maybe()
				n.On("GetContents", mock.Anything, "note4.md").Return("Content with #tag1", nil).Maybe()
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.NoError(t, err)
				assert.ElementsMatch(t, []string{"folder/note1.md", "note2.md", "note3.md", "note4.md"}, files)
			},
		},
		{
			name:      "property filter matches frontmatter",
			facts:     map[string]string{"note1.md": "---\nOffice: AOGR\n---\n", "note2.md": "---\nOffice: AORD\n---\n"},
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{
					{Type: InputTypeProperty, Property: "Office", Value: "AOGR"},
				},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md"}, nil)
				n.On("GetContents", mock.Anything, "note1.md").Return("---\nOffice: AOGR\n---\n", nil).Maybe()
				n.On("GetContents", mock.Anything, "note2.md").Return("---\nOffice: AORD\n---\n", nil).Maybe()
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.NoError(t, err)
				assert.ElementsMatch(t, []string{"note1.md"}, files)
			},
		},
		{
			name:      "property filter matches list wikilinks with alias",
			facts:     map[string]string{"note1.md": "---\nOffice: [[AOGR|Grand Rapids]]\n---\n", "note2.md": "---\nOffice: [[AORD]]\n---\n"},
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{
					{Type: InputTypeProperty, Property: "Office", Value: "AOGR"},
				},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md"}, nil)
				n.On("GetContents", mock.Anything, "note1.md").Return("---\nOffice: [[AOGR|Grand Rapids]]\n---\n", nil).Maybe()
				n.On("GetContents", mock.Anything, "note2.md").Return("---\nOffice: [[AORD]]\n---\n", nil).Maybe()
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.NoError(t, err)
				assert.ElementsMatch(t, []string{"note1.md"}, files)
			},
		},
		{
			name:      "list with multiple tags",
			facts:     map[string]string{"note1.md": "---\ntags: [tag1]\n---\nContent", "note2.md": "Content with #tag2", "note3.md": "Regular content"},
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{
					{Type: InputTypeTag, Value: "tag1"},
					{Type: InputTypeTag, Value: "tag2"},
				},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md", "note3.md"}, nil)
				n.On("GetContents", mock.Anything, "note1.md").Return("---\ntags: [tag1]\n---\nContent", nil).Maybe()
				n.On("GetContents", mock.Anything, "note2.md").Return("Content with #tag2", nil).Maybe()
				n.On("GetContents", mock.Anything, "note3.md").Return("Regular content", nil).Maybe()
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.NoError(t, err)
				assert.ElementsMatch(t, []string{"note1.md", "note2.md"}, files)
			},
		},
		{
			name:      "list with quoted tag",
			facts:     map[string]string{"note1.md": "---\ntags: [some-tag]\n---\nContent", "note2.md": "Regular content"},
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{
					{Type: InputTypeTag, Value: "some-tag"},
				},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md"}, nil)
				n.On("GetContents", mock.Anything, "note1.md").Return("---\ntags: [some-tag]\n---\nContent", nil).Maybe()
				n.On("GetContents", mock.Anything, "note2.md").Return("Regular content", nil).Maybe()
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.NoError(t, err)
				assert.Equal(t, []string{"note1.md"}, files)
			},
		},
		{
			name:      "parent tag matches hierarchy but not similar prefix",
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params:    ListParams{Inputs: []ListInput{{Type: InputTypeTag, Value: "context"}}},
			facts:     map[string]string{"private.md": "#Context/Private", "contextual.md": "#Contextual/Private"},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"private.md", "contextual.md"}, nil)
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				require.NoError(t, err)
				require.Equal(t, []string{"private.md"}, files)
			},
		},
		{
			name:      "list with directory path",
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{
					{Type: InputTypeFile, Value: "folder"},
				},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"folder/note1.md", "folder/note2.md", "other/note3.md"}, nil)
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.NoError(t, err)
				assert.ElementsMatch(t, []string{"folder/note1.md", "folder/note2.md"}, files)
			},
		},
		{
			name:      "list with no inputs",
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md"}, nil)
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.NoError(t, err)
				assert.Empty(t, files)
			},
		},
		{
			name:      "vault.Definition returns error",
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{}, errors.New("Failed to get vault definition"))
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.Error(t, err)
				assert.Equal(t, "Failed to get vault definition", err.Error())
				assert.Empty(t, files)
			},
		},
		{
			name:      "note.GetNotesList returns error",
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return(nil, errors.New("Failed to get notes list"))
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.Error(t, err)
				assert.Equal(t, "Failed to get notes list", err.Error())
				assert.Empty(t, files)
			},
		},
		{
			name:      "list with wildcard pattern",
			mockVault: &mocks.VaultManager{},
			mockNote:  &mocks.NoteReader{},
			params: ListParams{
				Inputs: []ListInput{
					{Type: InputTypeFile, Value: "*"},
				},
			},
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md", "folder/note3.md"}, nil)
			},
			validateResponse: func(t *testing.T, files []string, err error) {
				assert.NoError(t, err)
				assert.ElementsMatch(t, []string{"note1.md", "note2.md", "folder/note3.md"}, files)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMocks(tt.mockVault, tt.mockNote)

			files, err := ListFiles(tt.mockVault, newFactReader(tt.mockNote, tt.facts), tt.params)
			tt.validateResponse(t, files, err)

			tt.mockVault.AssertExpectations(t)
			tt.mockNote.AssertExpectations(t)
		})
	}
}

func TestListFilesBooleanExpression(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contents map[string]string
		query    []string
		want     []string
	}{
		{"and", map[string]string{"foo.md": "#foo", "bar.md": "#foo #bar", "baz.md": "#bar"}, []string{"tag:foo", "AND", "tag:bar"}, []string{"bar.md"}},
		{"not", map[string]string{"foo.md": "#foo", "bar.md": "#foo #bar", "baz.md": "#bar"}, []string{"tag:foo", "AND", "NOT", "tag:bar"}, []string{"foo.md"}},
		{"nested", map[string]string{"a.md": "#alpha", "b.md": "#beta", "c.md": "#alpha #gamma", "d.md": "#beta #gamma"}, []string{"(tag:alpha", "OR", "tag:beta)", "AND", "tag:gamma"}, []string{"c.md", "d.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := &mocks.VaultManager{}
			vault.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}, nil)
			note := &mocks.NoteReader{}
			paths := make([]string, 0, len(tc.contents))
			for path := range tc.contents {
				paths = append(paths, path)
			}
			note.On("GetNotesList", mock.Anything).Return(paths, nil).Maybe()
			_, expr, err := ParseInputsWithExpression(tc.query)
			require.NoError(t, err)
			files, err := ListFiles(vault, newFactReader(note, tc.contents), ListParams{Expression: expr})
			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, files)
		})
	}
	t.Run("projected facts without content snapshot", func(t *testing.T) {
		vault := &mocks.VaultManager{}
		vault.On("Definition").Return(obsidian.VaultDefinition{Name: "test", Path: t.TempDir()}, nil)
		reader := &listFactsOnlyReader{metadataSnapshotNote: metadataSnapshotNote{entries: []cache.Entry{
			{Path: "active.md", Tags: []string{"work"}, Frontmatter: map[string]any{"status": "active"}},
			{Path: "inactive.md", Tags: []string{"work"}, Frontmatter: map[string]any{"status": "inactive"}},
		}}}
		_, expr, err := ParseInputsWithExpression([]string{"tag:work", "AND", "status:active"})
		require.NoError(t, err)
		files, err := ListFiles(vault, reader, ListParams{Expression: expr})
		require.NoError(t, err)
		require.Equal(t, []string{"active.md"}, files)
		require.Zero(t, reader.snapshotReads)
	})
}

func TestListFilesWithFuzzySearch(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []ListInput
		files    []string
		facts    map[string]string
		expected []string
	}{
		{
			name: "single character directory match",
			inputs: []ListInput{
				{Type: InputTypeFind, Value: "l/"},
			},
			files: []string{
				"Log/Sync with team.md",
				"Notes/Some file.md",
			},
			expected: []string{
				"Log/Sync with team.md",
			},
		},
		{
			name: "directory and content match",
			inputs: []ListInput{
				{Type: InputTypeFind, Value: "log/sync joe"},
			},
			files: []string{
				"Log/Sync with Joe.md",
				"Log/Meeting with Joe.md",
				"Notes/Sync with Joe.md",
			},
			expected: []string{
				"Log/Sync with Joe.md",
			},
		},
		{
			name: "multiple matches",
			inputs: []ListInput{
				{Type: InputTypeFind, Value: "log/sync"},
			},
			files: []string{
				"Log/Sync with team.md",
				"Log/Sync with Joe.md",
				"Notes/Sync.md",
			},
			expected: []string{
				"Log/Sync with team.md",
				"Log/Sync with Joe.md",
			},
		},
		{
			name:  "combined tag and fuzzy search",
			facts: map[string]string{"Log/Sync meeting.md": "#meeting", "Notes/Sync meeting.md": "#meeting"},
			inputs: []ListInput{
				{Type: InputTypeTag, Value: "meeting"},
				{Type: InputTypeFind, Value: "log/sync"},
			},
			files: []string{
				"Log/Sync meeting.md",   // Matches both tag and fuzzy search
				"Log/Sync with team.md", // Matches fuzzy search only
				"Notes/Sync meeting.md", // Matches tag only
			},
			expected: []string{
				"Log/Sync meeting.md",   // Include once even though it matches both
				"Log/Sync with team.md", // Matches fuzzy search
				"Notes/Sync meeting.md", // Matches tag
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock vault and note managers
			mockVault := &MockVaultManager{}
			mockNote := &MockNoteReader{}
			vaultDef := obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}

			// Setup mock expectations
			mockVault.On("Definition").Return(vaultDef, nil)
			mockNote.On("GetNotesList", mock.Anything).Return(tt.files, nil)

			result, err := ListFiles(mockVault, newFactReader(mockNote, tt.facts), ListParams{Inputs: tt.inputs})

			// Verify results
			assert.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)

			// Verify all mock expectations were met
			mockVault.AssertExpectations(t)
			mockNote.AssertExpectations(t)
		})
	}
}

func TestListFilesWithWikilinks(t *testing.T) {
	tests := []struct {
		name         string
		files        []string
		fileContents map[string]string
		inputs       []ListInput
		maxDepth     int
		skipAnchors  bool
		expected     []string
	}{
		{
			name:  "follow wikilinks disabled",
			files: []string{"note1.md", "note2.md", "note3.md", "note4.md"},
			fileContents: map[string]string{
				"note1.md": "Content with link to [[note2]]",
				"note2.md": "Content with #tag1",
				"note3.md": "Content with link to [[note4]]",
				"note4.md": "Regular content",
			},
			inputs: []ListInput{
				{Type: InputTypeTag, Value: "tag1"},
			},
			maxDepth:    0,
			skipAnchors: false,
			expected:    []string{"note2.md"}, // only direct tag match
		},
		{
			name:  "with filepath input and follow links",
			files: []string{"folder/note1.md", "folder/note2.md", "note3.md", "note4.md"},
			fileContents: map[string]string{
				"folder/note1.md": "Content with link to [[note3]]",
				"folder/note2.md": "Content with link to [[note4]]",
				"note3.md":        "Content with #important",
				"note4.md":        "Regular content",
			},
			inputs: []ListInput{
				{Type: InputTypeFile, Value: "folder"},
			},
			maxDepth:    1,
			skipAnchors: false,
			expected:    []string{"folder/note1.md", "folder/note2.md", "note3.md", "note4.md"},
		},
		{
			name:  "find input with depth traversal - checks callback behavior",
			files: []string{"weekly/2025-W12.md", "daily/2025-03-17.md", "daily/2025-03-18.md", "daily/2025-03-19.md"},
			fileContents: map[string]string{
				"weekly/2025-W12.md":  "Weekly note with links to [[daily/2025-03-17]], [[daily/2025-03-18]], and [[daily/2025-03-19]]",
				"daily/2025-03-17.md": "Daily note for Monday",
				"daily/2025-03-18.md": "Daily note for Tuesday",
				"daily/2025-03-19.md": "Daily note for Wednesday",
			},
			inputs: []ListInput{
				{Type: InputTypeFind, Value: "2025-W12"},
			},
			maxDepth:    2,
			skipAnchors: false,
			expected:    []string{"weekly/2025-W12.md", "daily/2025-03-17.md", "daily/2025-03-18.md", "daily/2025-03-19.md"},
		},
		{
			name:  "skip anchored links",
			files: []string{"project.md", "tasks.md", "section1.md", "section2.md"},
			fileContents: map[string]string{
				"project.md":  "Project with links to [[tasks]] and sections [[section1#details]], [[section2#summary]]",
				"tasks.md":    "Tasks related to the project",
				"section1.md": "Section 1 details",
				"section2.md": "Section 2 summary",
			},
			inputs: []ListInput{
				{Type: InputTypeFile, Value: "project.md"},
			},
			maxDepth:    1,
			skipAnchors: true,
			expected:    []string{"project.md", "tasks.md"}, // should not include section1.md or section2.md
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock vault and note managers
			mockVault := &MockVaultManager{}
			mockNote := &MockNoteReader{}
			vaultDef := obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}

			// Setup vault definition
			mockVault.On("Definition").Return(vaultDef, nil)

			mockNote.On("GetNotesList", mock.Anything).Return(tt.files, nil).Maybe()
			for file, content := range tt.fileContents {
				mockNote.On("GetContents", mock.Anything, file).Return(content, nil).Maybe()
			}
			matched := map[string]int{}
			result, err := ListFiles(mockVault, newFactReader(mockNote, tt.fileContents), ListParams{
				Inputs:      tt.inputs,
				MaxDepth:    tt.maxDepth,
				SkipAnchors: tt.skipAnchors,
				OnMatch:     func(path string) { matched[path]++ },
			})
			for _, path := range tt.expected {
				require.Equal(t, 1, matched[path], path)
			}
			require.Len(t, matched, len(tt.expected))

			// Verify results
			assert.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)

			// Verify all mock expectations were met
			mockVault.AssertExpectations(t)
			mockNote.AssertExpectations(t)
		})
	}
}

// Test cases for tag suppression functionality
func TestListFilesWithSuppressedTags(t *testing.T) {
	tests := []struct {
		name           string
		files          []string
		fileContents   map[string]string
		projected      []notemeta.NoteSourceSnapshot
		inputs         []ListInput
		maxDepth       int
		suppressedTags []string
		expected       []string
	}{
		{
			name:  "default no-prompt suppression",
			files: []string{"note1.md", "note2.md", "note3.md"},
			fileContents: map[string]string{
				"note1.md": "Content",
				"note2.md": "Content with #no-prompt",
				"note3.md": "Content with #work",
			},
			inputs: []ListInput{
				{Type: InputTypeFile, Value: "*"},
			},
			suppressedTags: []string{"no-prompt"},
			expected:       []string{"note1.md", "note3.md"},
		},
		{
			name:  "tag search with suppression",
			files: []string{"note1.md", "note2.md", "note3.md", "note4.md"},
			fileContents: map[string]string{
				"note1.md": "---\ntags: [work]\n---\nContent",
				"note2.md": "---\ntags: [work, no-prompt]\n---\nContent",
				"note3.md": "Content with #work",
				"note4.md": "Content with #other",
			},
			inputs: []ListInput{
				{Type: InputTypeTag, Value: "work"},
			},
			suppressedTags: []string{"no-prompt"},
			expected:       []string{"note1.md", "note3.md"}, // note2.md excluded despite having work tag
		},
		{
			name:  "multiple suppressed tags",
			files: []string{"note1.md", "note2.md", "note3.md", "note4.md"},
			fileContents: map[string]string{
				"note1.md": "Content",
				"note2.md": "Content with #no-prompt",
				"note3.md": "Content with #private",
				"note4.md": "Content with #draft",
			},
			inputs: []ListInput{
				{Type: InputTypeFile, Value: "*"},
			},
			suppressedTags: []string{"no-prompt", "private", "draft"},
			expected:       []string{"note1.md"},
		},
		{
			name:  "no suppression",
			files: []string{"note1.md", "note2.md", "note3.md"},
			fileContents: map[string]string{
				"note1.md": "Content",
				"note2.md": "Content with #no-prompt",
				"note3.md": "Content with #private",
			},
			inputs: []ListInput{
				{Type: InputTypeFile, Value: "*"},
			},
			suppressedTags: []string{}, // no suppression
			expected:       []string{"note1.md", "note2.md", "note3.md"},
		},
		{
			name:  "projected parent tag excludes hierarchy but not similar prefix",
			files: []string{"private.md", "contextual.md", "plain.md"},
			projected: []notemeta.NoteSourceSnapshot{
				{Path: paths.NormalizeNotePath("private.md"), Tags: []string{"#Context/Private"}},
				{Path: paths.NormalizeNotePath("contextual.md"), Tags: []string{"#Contextual/Private"}},
			},
			inputs:         []ListInput{{Type: InputTypeFile, Value: "*"}},
			suppressedTags: []string{"context"},
			expected:       []string{"contextual.md", "plain.md"},
		},
		{
			name:           "missing projected facts remain visible",
			files:          []string{"hidden.md", "missing.md"},
			projected:      []notemeta.NoteSourceSnapshot{{Path: paths.NormalizeNotePath("hidden.md"), Tags: []string{"NO-PROMPT"}}},
			inputs:         []ListInput{{Type: InputTypeFile, Value: "*"}},
			suppressedTags: []string{"no-prompt"},
			expected:       []string{"missing.md"},
		},
		{
			name:  "followed results are suppressed",
			files: []string{"source.md", "allowed.md", "hidden.md"},
			fileContents: map[string]string{
				"source.md":  "[[allowed]] [[hidden]]",
				"allowed.md": "#work",
				"hidden.md":  "#no-prompt",
			},
			inputs:         []ListInput{{Type: InputTypeFile, Value: "source.md"}},
			maxDepth:       1,
			suppressedTags: []string{"no-prompt"},
			expected:       []string{"source.md", "allowed.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock vault and note managers
			mockVault := &MockVaultManager{}
			mockNote := &MockNoteReader{}
			vaultDef := obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}

			// Setup mock expectations
			mockVault.On("Definition").Return(vaultDef, nil)
			mockNote.On("GetNotesList", mock.Anything).Return(tt.files, nil)

			// Setup content expectations for tag inputs
			for _, input := range tt.inputs {
				if input.Type == InputTypeTag {
					for _, file := range tt.files {
						content := tt.fileContents[file]
						mockNote.On("GetContents", mock.Anything, file).Return(content, nil).Maybe()
					}
				}
			}

			// Setup content expectations for suppression filtering
			if len(tt.suppressedTags) > 0 {
				for file, content := range tt.fileContents {
					mockNote.On("GetContents", mock.Anything, file).Return(content, nil).Maybe()
				}
			}

			// Run the test
			reader := newFactReader(mockNote, tt.fileContents)
			if tt.projected != nil {
				reader = factReader{NoteReader: mockNote, facts: NewNoteFacts(tt.projected)}
			}
			result, err := ListFiles(mockVault, reader, ListParams{
				Inputs:         tt.inputs,
				MaxDepth:       tt.maxDepth,
				SuppressedTags: tt.suppressedTags,
			})

			// Verify results
			assert.NoError(t, err)
			assert.ElementsMatch(t, tt.expected, result)

			// Verify all mock expectations were met
			mockVault.AssertExpectations(t)
			mockNote.AssertExpectations(t)
		})
	}
}

func TestListFilesBacklinksIncludeEmptyTargets(t *testing.T) {
	mockVault := &MockVaultManager{}
	mockNote := &MockNoteReader{}
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}
	mockVault.On("Definition").Return(vaultDef, nil)
	mockNote.On("GetNotesList", mock.Anything).Return([]string{"target.md", "nolinks.md", "ref.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "target.md").Return("content", nil)
	mockNote.On("GetContents", mock.Anything, "nolinks.md").Return("", nil)
	mockNote.On("GetContents", mock.Anything, "ref.md").Return("Basic [[target]] and heading [[target#H]]", nil)

	backlinkMap := make(map[string][]obsidian.Backlink)
	var primaries []string
	matches, err := ListFiles(mockVault, mockNote, ListParams{
		Inputs:           []ListInput{{Type: InputTypeFile, Value: "target.md"}, {Type: InputTypeFile, Value: "nolinks.md"}},
		IncludeBacklinks: true,
		Backlinks:        &backlinkMap,
		PrimaryMatches:   &primaries,
		WikilinkOptions: obsidian.WikilinkOptions{
			SkipAnchors: false,
			SkipEmbeds:  false,
		},
	})

	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"target.md", "nolinks.md"}, matches)
	assert.ElementsMatch(t, []string{"target.md", "nolinks.md"}, primaries)
	if assert.Contains(t, backlinkMap, "target.md") {
		assert.ElementsMatch(t, []obsidian.Backlink{
			{Referrer: "ref.md", LinkType: obsidian.BacklinkTypeBasic},
			{Referrer: "ref.md", LinkType: obsidian.BacklinkTypeHeading, Fragment: "H"},
		}, backlinkMap["target.md"])
	}
	if assert.Contains(t, backlinkMap, "nolinks.md") {
		assert.Len(t, backlinkMap["nolinks.md"], 0)
	}
}

func TestListFilesBacklinksWithStorePreservesLiveFragments(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "target.md"), []byte("# Target\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ref.md"), []byte("[heading](target.md#details) and [[target#^block-id]]\n"), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vaultDef := obsidian.VaultDefinition{Name: "vault", Path: root, Links: obsidian.LinkTypeBoth}
	note := &obsidian.Note{}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), vaultDef, note, store)
	require.NoError(t, err)

	backlinks := map[string][]obsidian.Backlink{}
	vault := &fixedVault{def: vaultDef, name: "vault"}
	matches, err := ListFiles(vault, note, ListParams{
		Inputs:           []ListInput{{Type: InputTypeFile, Value: "target.md"}},
		IncludeBacklinks: true,
		Backlinks:        &backlinks,
		SessionStore:     store,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"target.md"}, matches)
	require.Equal(t, []obsidian.Backlink{
		{Referrer: "ref.md", LinkType: obsidian.BacklinkTypeBlock, Fragment: "^block-id"},
		{Referrer: "ref.md", LinkType: obsidian.BacklinkTypeHeading, Fragment: "details"},
	}, backlinks["target.md"])
}

func TestListFilesBacklinksRespectSuppressedTags(t *testing.T) {
	mockVault := &MockVaultManager{}
	mockNote := &MockNoteReader{}
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: "/test/vault"}
	mockVault.On("Definition").Return(vaultDef, nil)
	mockNote.On("GetNotesList", mock.Anything).Return([]string{"target.md", "ref-keep.md", "ref-hide.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "target.md").Return("content", nil)
	mockNote.On("GetContents", mock.Anything, "ref-keep.md").Return("Link [[target]]", nil)
	mockNote.On("GetContents", mock.Anything, "ref-hide.md").Return("Frontmatter:\n#no-prompt\nLink [[target]]", nil)

	backlinkMap := make(map[string][]obsidian.Backlink)
	_, err := ListFiles(mockVault, mockNote, ListParams{
		Inputs:           []ListInput{{Type: InputTypeFile, Value: "target.md"}},
		IncludeBacklinks: true,
		Backlinks:        &backlinkMap,
		SuppressedTags:   []string{"no-prompt"},
		WikilinkOptions: obsidian.WikilinkOptions{
			SkipAnchors: false,
			SkipEmbeds:  false,
		},
	})

	assert.NoError(t, err)
	if assert.Contains(t, backlinkMap, "target.md") {
		assert.Len(t, backlinkMap["target.md"], 1)
		assert.Equal(t, "ref-keep.md", backlinkMap["target.md"][0].Referrer)
	}
}

type metadataSnapshotNote struct {
	entries []cache.Entry
}

type listFactsOnlyReader struct {
	metadataSnapshotNote
	snapshotReads int
}

func (r *listFactsOnlyReader) EntriesSnapshot(context.Context) ([]cache.Entry, error) {
	r.snapshotReads++
	return nil, errors.New("unused content snapshot requested")
}

func (n *metadataSnapshotNote) NoteFacts() NoteFacts {
	sources := make([]notemeta.NoteSourceSnapshot, 0, len(n.entries))
	for _, entry := range n.entries {
		sources = append(sources, notemeta.NoteSourceSnapshot{
			Path:        paths.NormalizeNotePath(entry.Path),
			Content:     entry.Content,
			Frontmatter: entry.Frontmatter,
			InlineProps: entry.InlineProps,
			Tags:        entry.Tags,
		})
	}
	return NewNoteFacts(sources)
}

func (n *metadataSnapshotNote) EntriesSnapshot(context.Context) ([]cache.Entry, error) {
	return append([]cache.Entry(nil), n.entries...), nil
}

func (n *metadataSnapshotNote) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	for _, entry := range n.entries {
		if entry.Path == path {
			return entry.Content, nil
		}
	}
	return "", errors.New("unexpected direct content read")
}

func (n *metadataSnapshotNote) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	out := make([]string, 0, len(n.entries))
	for _, entry := range n.entries {
		out = append(out, entry.Path)
	}
	return out, nil
}

func (n *metadataSnapshotNote) GetModTime(_ obsidian.VaultDefinition, path string) (time.Time, error) {
	for _, entry := range n.entries {
		if entry.Path == path {
			return entry.ModTime, nil
		}
	}
	return time.Time{}, nil
}

func (n *metadataSnapshotNote) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}

type noListMetadataNote struct {
	metadataSnapshotNote
}

func (n *noListMetadataNote) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, errors.New("unexpected note list read")
}

// TestListFilesFindResolvesFrontmatterAlias verifies that a `find:` token
// that does not fuzzy-match any filename still resolves to the owning note
// when that note declares the token as a frontmatter alias. This is the
// alias shortcut added in cmd/app/cli/list.go's evalLeafSet: it lets
// `find:SPEC-001` return `specs/indexed-search.md` when the note has
// `aliases: [SPEC-001]`.
//
// The fixture filename intentionally has no "spec" or "001" substring so
// obsidian.FuzzyMatch cannot hit it — any match must come from the DB
// alias shortcut. The subcases also pin case-insensitivity of the
// shortcut (mirrors plan review feedback: note_property_values.value_norm
// is lowercased at index time, so the token must be normalized before
// the query or mixed-case inputs silently miss).
func TestListFilesFindResolvesFrontmatterAlias(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "specs", "indexed-search.md"),
		[]byte("---\nspecId: SPEC-001\naliases:\n  - SPEC-001\n---\n# Indexed Search\n"),
		0o644,
	))

	// Sanity-check the fuzzy matcher can't hit the filename so the
	// alias shortcut is the only way the expected match is produced.
	require.False(t, obsidian.FuzzyMatch("SPEC-001", "specs/indexed-search.md"), "fixture must be fuzzy-unreachable")
	require.False(t, obsidian.FuzzyMatch("spec-001", "specs/indexed-search.md"), "fixture must be fuzzy-unreachable")

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	note := &metadataSnapshotNote{
		entries: []cache.Entry{
			{
				Path:    "specs/indexed-search.md",
				Content: "---\nspecId: SPEC-001\naliases:\n  - SPEC-001\n---\n# Indexed Search\n",
				Frontmatter: map[string]any{
					"specId":  "SPEC-001",
					"aliases": []interface{}{"SPEC-001"},
				},
				ModTime: time.Unix(100, 0),
				Size:    100,
			},
		},
	}
	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "TestVault", Path: root},
		name: "TestVault",
	}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), vault.def, note, store)
	require.NoError(t, err)

	for _, tok := range []string{"SPEC-001", "spec-001", "  SPEC-001  "} {
		tok := tok
		t.Run(tok, func(t *testing.T) {
			matches, err := ListFiles(vault, note, ListParams{
				Inputs:       []ListInput{{Type: InputTypeFind, Value: tok}},
				SessionStore: store,
			})
			require.NoError(t, err)
			require.Equal(t, []string{"specs/indexed-search.md"}, matches)
		})
	}
}

func TestListFilesBooleanExpressionUsesStoreWithoutListingNotes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Roadmap.md"), []byte("placeholder\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Launch.md"), []byte("placeholder\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	note := &metadataSnapshotNote{
		entries: []cache.Entry{
			{
				Path:        "notes/Roadmap.md",
				Content:     "---\nstatus: active\n---\n",
				Frontmatter: map[string]any{"status": "active"},
				ModTime:     time.Unix(10, 0),
				Size:        40,
			},
			{
				Path:        "notes/Launch.md",
				Content:     "---\nstatus: planned\n---\n",
				Frontmatter: map[string]any{"status": "planned"},
				ModTime:     time.Unix(20, 0),
				Size:        41,
			},
		},
	}
	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "TestVault", Path: root},
		name: "TestVault",
	}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), vault.def, note, store)
	require.NoError(t, err)

	storeOnlyNote := &noListMetadataNote{metadataSnapshotNote{entries: note.entries}}
	expr := &InputExpression{
		Type: exprAnd,
		Left: &InputExpression{Type: exprLeaf, Input: &ListInput{Type: InputTypeFile, Value: "notes/Roadmap.md"}},
		Right: &InputExpression{Type: exprLeaf, Input: &ListInput{
			Type:     InputTypeProperty,
			Property: "status",
			Value:    "active",
		}},
	}

	matches, err := ListFiles(vault, storeOnlyNote, ListParams{
		Expression:   expr,
		SessionStore: store,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"notes/Roadmap.md"}, matches)
}

func TestListFilesBooleanExpressionNarrowsFallbackFindBySelectiveStoreLeaf(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Roadmap.md"), []byte("placeholder\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Launch.md"), []byte("placeholder\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	note := &metadataSnapshotNote{
		entries: []cache.Entry{
			{
				Path:        "notes/Roadmap.md",
				Content:     "---\nstatus: active\n---\n",
				Frontmatter: map[string]any{"status": "active"},
				ModTime:     time.Unix(10, 0),
				Size:        40,
			},
			{
				Path:        "notes/Launch.md",
				Content:     "---\nstatus: planned\n---\n",
				Frontmatter: map[string]any{"status": "planned"},
				ModTime:     time.Unix(20, 0),
				Size:        41,
			},
		},
	}
	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "TestVault", Path: root},
		name: "TestVault",
	}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), vault.def, note, store)
	require.NoError(t, err)

	storeOnlyNote := &noListMetadataNote{metadataSnapshotNote{entries: note.entries}}
	expr := &InputExpression{
		Type: exprAnd,
		Left: &InputExpression{Type: exprLeaf, Input: &ListInput{
			Type:     InputTypeProperty,
			Property: "status",
			Value:    "active",
		}},
		Right: &InputExpression{Type: exprLeaf, Input: &ListInput{
			Type:  InputTypeFind,
			Value: "*map*",
		}},
	}

	matches, err := ListFiles(vault, storeOnlyNote, ListParams{
		Expression:   expr,
		SessionStore: store,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"notes/Roadmap.md"}, matches)
}

func TestListFilesTrustsReadyMetadataWithoutRehashingVault(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "TestVault", Path: root},
		name: "TestVault",
	}
	initial := &metadataSnapshotNote{
		entries: []cache.Entry{
			{
				Path:        "notes/roadmap.md",
				Content:     "---\nstatus: old\n---\n",
				Frontmatter: map[string]any{"status": "old"},
				ModTime:     time.Unix(10, 0),
				Size:        21,
			},
		},
	}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), vault.def, initial, store)
	require.NoError(t, err)

	staleOnDisk := &noListMetadataNote{metadataSnapshotNote{
		entries: []cache.Entry{
			{
				Path:        "notes/roadmap.md",
				Content:     "---\nstatus: new\n---\n",
				Frontmatter: map[string]any{"status": "new"},
				ModTime:     time.Unix(20, 0),
				Size:        21,
			},
			{
				Path:        "notes/planned.md",
				Content:     "---\nstatus: planned\n---\n",
				Frontmatter: map[string]any{"status": "planned"},
				ModTime:     time.Unix(21, 0),
			},
		},
	}}

	matches, err := ListFiles(vault, staleOnDisk, ListParams{
		Inputs:         []ListInput{{Type: InputTypeProperty, Property: "status", Value: "old"}},
		SessionStore:   store,
		PrimaryMatches: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"notes/roadmap.md"}, matches)
	for _, status := range []string{"new", "planned"} {
		matches, err := ListFiles(vault, staleOnDisk, ListParams{
			Inputs:       []ListInput{{Type: InputTypeProperty, Property: "status", Value: status}},
			SessionStore: store,
		})
		require.NoError(t, err)
		require.Empty(t, matches, status)
	}
}

func TestListFilesRespectsExplicitEmptyCandidateScope(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Roadmap.md"), []byte("placeholder\n"), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	note := &metadataSnapshotNote{
		entries: []cache.Entry{
			{
				Path:        "notes/Roadmap.md",
				Content:     "---\nstatus: active\n---\n",
				Frontmatter: map[string]any{"status": "active"},
				ModTime:     time.Unix(10, 0),
				Size:        40,
			},
		},
	}
	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "TestVault", Path: root},
		name: "TestVault",
	}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(context.Background(), vault.def, note, store)
	require.NoError(t, err)

	storeOnlyNote := &noListMetadataNote{metadataSnapshotNote{entries: note.entries}}
	matches, err := ListFiles(vault, storeOnlyNote, ListParams{
		Inputs:         []ListInput{{Type: InputTypeProperty, Property: "status", Value: "active"}},
		SessionStore:   store,
		CandidatePaths: []string{},
		PrimaryMatches: nil,
	})
	require.NoError(t, err)
	require.Empty(t, matches)
}

func TestListFilesUsesLivePathsForExplicitFileInputs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vault := &fixedVault{def: obsidian.VaultDefinition{Name: "test", Path: root}, name: "test"}
	indexed := &metadataSnapshotNote{entries: []cache.Entry{{Path: "notes/old.md", Content: "# Old", ModTime: time.Unix(10, 0)}}}
	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, vault.def, indexed, store)
	require.NoError(t, err)
	live := &metadataSnapshotNote{entries: []cache.Entry{{Path: "notes/renamed.md", Content: "# Renamed", ModTime: time.Unix(20, 0)}}}
	for _, tc := range []struct {
		name  string
		query []string
		want  []string
	}{
		{"single live file", []string{"notes/renamed.md"}, []string{"notes/renamed.md"}},
		{"file OR file", []string{"notes/renamed.md", "OR", "notes/old.md"}, []string{"notes/renamed.md"}},
		{"mixed find uses indexed title", []string{"notes/renamed.md", "OR", "find:old"}, []string{"notes/old.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, expr, err := ParseInputsWithExpression(tc.query)
			require.NoError(t, err)
			got, err := ListFiles(vault, live, ListParams{Expression: expr, SessionStore: store})
			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, got)
		})
	}
}
