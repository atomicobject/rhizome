package actions

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/mocks"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBrokenLinks(t *testing.T) {
	vaultDef := obsidian.VaultDefinition{Name: "vault", Path: "/vault"}

	tests := []struct {
		name          string
		setupMocks    func(*mocks.VaultManager, *mocks.NoteReader)
		options       obsidian.BrokenLinksOptions
		expectedCount int
		expectError   bool
	}{
		{
			name: "finds broken links",
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(vaultDef, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"source.md", "target.md"}, nil)
				n.On("GetContents", mock.Anything, "source.md").Return("Link to [[target]] and [[missing]]", nil)
				n.On("GetContents", mock.Anything, "target.md").Return("No links here", nil)
			},
			options:       obsidian.DefaultBrokenLinksOptions,
			expectedCount: 1, // [[missing]] is broken
			expectError:   false,
		},
		{
			name: "no broken links",
			setupMocks: func(v *mocks.VaultManager, n *mocks.NoteReader) {
				v.On("Definition").Return(vaultDef, nil)
				n.On("GetNotesList", mock.Anything).Return([]string{"source.md", "target.md"}, nil)
				n.On("GetContents", mock.Anything, "source.md").Return("Link to [[target]]", nil)
				n.On("GetContents", mock.Anything, "target.md").Return("Link back to [[source]]", nil)
			},
			options:       obsidian.DefaultBrokenLinksOptions,
			expectedCount: 0,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockVault := &mocks.VaultManager{}
			mockNote := &mocks.NoteReader{}
			tt.setupMocks(mockVault, mockNote)

			broken, err := BrokenLinks(mockVault, mockNote, tt.options)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, broken, tt.expectedCount)
			}

			mockVault.AssertExpectations(t)
			mockNote.AssertExpectations(t)
		})
	}
}

func TestDeadEnds(t *testing.T) {
	// DeadEnds relies on GraphAnalysis which has complex mocking requirements.
	// This is an integration test that verifies the function signature works.
	// Full coverage is provided by the primitive tests in pkg/obsidian/health_test.go
	vaultDef := obsidian.VaultDefinition{Name: "vault", Path: "/vault"}

	mockVault := &mocks.VaultManager{}
	mockNote := &mocks.NoteReader{}

	mockVault.On("Definition").Return(vaultDef, nil)
	mockNote.On("GetNotesList", mock.Anything).Return([]string{"stub.md", "linker.md"}, nil)
	mockNote.On("GetContents", mock.Anything, "stub.md").Return("No links here", nil)
	mockNote.On("GetContents", mock.Anything, "linker.md").Return("Links to [[stub]]", nil)

	params := GraphAnalysisParams{}

	deadEnds, err := DeadEnds(mockVault, mockNote, params)
	assert.NoError(t, err)
	require.Len(t, deadEnds, 1)

	// stub.md should be a dead-end (has inbound from linker, no outbound)
	found := false
	for _, de := range deadEnds {
		if de.Path == "stub.md" {
			found = true
			assert.Equal(t, 1, de.InboundLinks)
			break
		}
	}
	assert.True(t, found, "Expected stub.md to be a dead-end")

	mockVault.AssertExpectations(t)
	mockNote.AssertExpectations(t)
}

func TestStaleNotes(t *testing.T) {
	root := t.TempDir()
	vault := &fixedVault{def: obsidian.VaultDefinition{Name: "vault", Path: root}, name: "vault"}
	note := &metadataSnapshotNote{entries: []cache.Entry{
		{Path: "old.md", ModTime: time.Now().AddDate(0, 0, -120)},
		{Path: "recent.md", ModTime: time.Now().AddDate(0, 0, -5)},
	}}
	stale, err := StaleNotes(vault, note, 90)
	require.NoError(t, err)
	require.Len(t, stale, 1)
	require.Equal(t, "old.md", stale[0].Path)
	stale, err = StaleNotes(vault, note, 180)
	require.NoError(t, err)
	require.Empty(t, stale)
}

func healthFixture(t *testing.T) (*fixedVault, *metadataSnapshotNote) {
	t.Helper()
	root := t.TempDir()
	vault := &fixedVault{def: obsidian.VaultDefinition{Name: "TestVault", Path: root, Links: obsidian.LinkTypeBoth}, name: "TestVault"}
	note := &metadataSnapshotNote{entries: []cache.Entry{
		{Path: "source.md", Content: "[[folder/target]] [[missing]]", ModTime: time.Now()},
		{Path: "folder/target.md", Content: "No links", ModTime: time.Now().AddDate(0, 0, -120)},
		{Path: "archive/target.md", Content: "No links", ModTime: time.Now()},
	}}
	return vault, note
}

func TestVaultHealth(t *testing.T) {
	vault, note := healthFixture(t)
	report, err := VaultHealth(vault, note, HealthParams{
		StaleDays:       90,
		Include:         []string{"brokenLinks", "staleNotes", "deadEnds", "suggestedMerges"},
		WikilinkOptions: obsidian.DefaultWikilinkOptions,
	})
	require.NoError(t, err)
	require.Equal(t, "TestVault", report.Vault)
	require.Equal(t, filepath.ToSlash(vault.def.Path), report.VaultPath)
	require.Equal(t, 3, report.Stats.TotalNotes)
	require.Equal(t, 1, report.Stats.BrokenLinkCount)
	require.Equal(t, "missing", report.BrokenLinks[0].Target)
	require.Equal(t, 1, report.Stats.StaleNoteCount)
	require.Equal(t, "folder/target.md", report.StaleNotes[0].Path)
	require.Equal(t, 1, report.Stats.DeadEndCount)
	require.Equal(t, "folder/target.md", report.DeadEnds[0].Path)
	require.Equal(t, 1, report.DeadEnds[0].InboundLinks)
	require.Equal(t, 1, report.Stats.MergeSuggestionCount)
	require.ElementsMatch(t, []string{"folder/target.md", "archive/target.md"}, []string{report.SuggestedMerges[0].Note1, report.SuggestedMerges[0].Note2})
	require.False(t, report.AnalyzedAt.IsZero())
}

func TestVaultHealthSelectiveInclude(t *testing.T) {
	vault, note := healthFixture(t)
	full, err := VaultHealth(vault, note, HealthParams{StaleDays: 90, Include: []string{"brokenLinks", "staleNotes", "deadEnds", "suggestedMerges"}, WikilinkOptions: obsidian.DefaultWikilinkOptions})
	require.NoError(t, err)
	require.Positive(t, full.Stats.StaleNoteCount)
	require.Positive(t, full.Stats.DeadEndCount)
	require.Positive(t, full.Stats.MergeSuggestionCount)
	selected, err := VaultHealth(vault, note, HealthParams{StaleDays: 90, Include: []string{"brokenLinks"}, WikilinkOptions: obsidian.DefaultWikilinkOptions})
	require.NoError(t, err)
	require.Equal(t, 1, selected.Stats.BrokenLinkCount)
	require.Zero(t, selected.Stats.StaleNoteCount)
	require.Zero(t, selected.Stats.DeadEndCount)
	require.Zero(t, selected.Stats.MergeSuggestionCount)
	require.Empty(t, selected.StaleNotes)
	require.Empty(t, selected.DeadEnds)
	require.Empty(t, selected.SuggestedMerges)
}

type fixedVault struct {
	def  obsidian.VaultDefinition
	name string
}

func (v *fixedVault) DefaultName() (string, error) { return v.name, nil }
func (v *fixedVault) SetDefaultName(name string) error {
	v.name = name
	return nil
}
func (v *fixedVault) Path() (string, error) { return v.def.Path, nil }
func (v *fixedVault) Definition() (obsidian.VaultDefinition, error) {
	return v.def, nil
}

func TestVaultHealth_IncludesOntologyIssues(t *testing.T) {
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(filepath.Join(rhizomeDir, "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "ontology", "schema.graphql"), []byte(`
type Person @node(paths: ["people/*.md"]) {
  name: String!
}
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "misc"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "misc", "Loose.md"), []byte(`---
type: Person
---
`), 0o644))

	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "TestVault", Path: root},
		name: "TestVault",
	}
	note := &obsidian.Note{}
	store, err := sqlitefixture.Open(filepath.Join(rhizomeDir, "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	report, err := VaultHealth(vault, note, HealthParams{
		Include:         []string{"brokenLinks"},
		SessionStore:    store,
		NoteMetadata:    testNoteMetadataIndexer(t),
		WikilinkOptions: obsidian.DefaultWikilinkOptions,
	})
	require.NoError(t, err)
	require.NotEmpty(t, report.OntologyIssues)
	require.Equal(t, len(report.OntologyIssues), report.Stats.OntologyIssueCount)
	codes := make([]string, 0, len(report.OntologyIssues))
	for _, issue := range report.OntologyIssues {
		codes = append(codes, issue.Code)
	}
	assert.Contains(t, codes, "declared_type_mismatch")
}

func TestVaultHealthManagedReadOnlyUsesPersistedOntologyIssuesForDriftedNotes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes: {}\n"), 0o644))
	notePath := filepath.Join(root, "Drifted.md")
	require.NoError(t, os.WriteFile(notePath, []byte("# Before\n"), 0o644))

	issuesJSON, err := json.Marshal([]ontology.ValidationIssue{{
		Code:     "persisted_ontology_issue",
		NotePath: "Drifted.md",
		Message:  "The indexed ontology result is authoritative for this read-only request.",
	}})
	require.NoError(t, err)
	indexPath := filepath.Join(rhizomeDir, "db.sqlite")
	writable, err := sqlitefixture.Open(indexPath)
	require.NoError(t, err)
	require.NoError(t, writable.ReplaceOntologySnapshot(ctx, codeanchorsqlite.OntologySnapshot{
		SchemaState: codeanchorsqlite.OntologySchemaState{ErrorJSON: string(issuesJSON)},
	}))
	require.NoError(t, writable.Close())
	require.NoError(t, os.WriteFile(notePath, []byte("# After\n\nThis note drifted after the ontology projection.\n"), 0o644))
	before, err := os.ReadFile(indexPath)
	require.NoError(t, err)

	readOnly, err := codeanchorsqlite.OpenReadOnlyExisting(indexPath, ctx, sqliteutil.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })
	vault := &fixedVault{
		def:  obsidian.VaultDefinition{Name: "TestVault", Path: root},
		name: "TestVault",
	}
	report, err := VaultHealth(vault, &obsidian.Note{}, HealthParams{
		Include:               []string{"brokenLinks"},
		SessionStore:          readOnly,
		MetadataStoreFallback: MetadataStoreFallbackLive,
		WikilinkOptions:       obsidian.DefaultWikilinkOptions,
	})
	require.NoError(t, err)
	require.Len(t, report.OntologyIssues, 1)
	assert.Equal(t, "persisted_ontology_issue", report.OntologyIssues[0].Code)
	after, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "managed read-only health must not materialize an ontology projection")
}
